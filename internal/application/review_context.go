package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
)

const (
	defaultSourceContextBytes = 16 << 10
	maxContextChangedFiles    = 4
	maxContextSourceReads     = 12
	maxContextDirectories     = 2
)

var contextHunk = regexp.MustCompile(`(?m)^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
var contextIdentifier = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{3,}`)

// collectReviewContext adds only bounded, numbered source excerpts. Patches
// remain untouched: source text must never become a substitute for a diff.
func collectReviewContext(ctx context.Context, gh ports.PullRequestGateway, o ReviewOptions, pr ports.PullRequest, files []ports.ChangedFile, patchBytes int) ([]ports.ContextFile, []string) {
	budget := o.MaxSourceContextBytes
	if budget == 0 {
		budget = defaultSourceContextBytes
	}
	if o.MaxContextBytes > 0 {
		budget = min(budget, o.MaxContextBytes-patchBytes)
	}
	if budget <= 0 {
		return nil, nil
	}
	var selected []ports.ChangedFile
	changed := map[string]bool{}
	for _, file := range files {
		changed[file.Path] = true
		if contextSourcePath(file.Path) {
			selected = append(selected, file)
		}
	}
	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].Additions+selected[i].Deletions > selected[j].Additions+selected[j].Deletions
	})
	if len(selected) > maxContextChangedFiles {
		selected = selected[:maxContextChangedFiles]
	}
	if len(selected) == 0 {
		return nil, nil
	}
	var out []ports.ContextFile
	var limitations []string
	remaining, reads := budget-2, 0 // JSON array delimiters.
	maxFileBytes := o.MaxFileBytes
	if maxFileBytes <= 0 {
		maxFileBytes = 256 << 10
	}
	read := func(file, sha string) string {
		if sha == "" || reads >= maxContextSourceReads || ctx.Err() != nil {
			return ""
		}
		reads++
		data, err := gh.ReadFileAtSHA(ctx, o.Repository, file, sha)
		if err != nil || len(data) > maxFileBytes || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
			limitations = append(limitations, "source context unavailable for "+file+" at "+sha)
			return ""
		}
		return string(data)
	}
	add := func(file, revision, source string, anchors []int, related []string, capBytes, radius int) {
		entry := ports.ContextFile{Path: file, Revision: revision, RelatedTo: related}
		metadata, _ := json.Marshal(entry)
		// Newlines can double under JSON escaping. Measure the actual encoding
		// below, shrinking the excerpt until it fits the shared wire budget.
		capBytes = min(capBytes, remaining-len(metadata)-1)
		for capBytes > 128 {
			entry.Content = sourceExcerpt(source, anchors, radius, capBytes)
			if entry.Content == "" {
				return
			}
			encoded, _ := json.Marshal(entry)
			if len(encoded)+1 <= remaining {
				out = append(out, entry)
				remaining -= len(encoded) + 1
				return
			}
			capBytes /= 2
		}
	}
	type origin struct {
		file, source string
		symbols      map[string]bool
	}
	var origins []origin
	// Leave room for BASE excerpts and adjacent callers/helpers.
	perFile := min(3072, budget/(2*max(1, len(selected))))
	for _, file := range selected {
		if remaining <= 256 {
			break
		}
		headAnchors, baseAnchors := patchAnchors(file.Patch)
		var head string
		if file.Status != "removed" {
			head = read(file.Path, pr.HeadSHA)
			add(file.Path, "HEAD", head, headAnchors, nil, perFile, 24)
		}
		origins = append(origins, origin{file.Path, head, reviewSymbols(file.Patch)})
		if file.Status != "added" && (file.Truncated || file.Deletions > 0 || strings.Contains(file.Patch, "\n-")) && remaining > 512 {
			previous := file.Path
			if file.PreviousPath != "" {
				previous = file.PreviousPath
			}
			add(previous, "BASE", read(previous, pr.BaseSHA), baseAnchors, []string{file.Path}, min(1536, perFile/2), 12)
		}
	}
	// A directory listing contains paths, never code in the model context.
	// Inspect at most four adjacent files after the changed-file reads.
	if lister, ok := gh.(ports.ReviewContextGateway); ok && remaining > 512 {
		directories, seen := map[string]bool{}, map[string]bool{}
		inspected := 0
		for _, origin := range origins {
			directory := path.Dir(origin.file)
			if directories[directory] || len(directories) >= maxContextDirectories || reads >= maxContextSourceReads || inspected >= 4 {
				continue
			}
			directories[directory] = true
			queryDirectory := directory
			if directory == "." {
				queryDirectory = ""
			}
			paths, err := lister.ListDirectoryAtSHA(ctx, o.Repository, queryDirectory, pr.HeadSHA)
			if err != nil {
				limitations = append(limitations, "adjacent source discovery unavailable for "+directory)
				continue
			}
			sort.SliceStable(paths, func(i, j int) bool {
				a, b := adjacentPriority(paths[i], origin.source), adjacentPriority(paths[j], origin.source)
				if a != b {
					return a > b
				}
				return paths[i] < paths[j]
			})
			for _, candidate := range paths {
				if remaining <= 512 || reads >= maxContextSourceReads || inspected >= 4 {
					break
				}
				if changed[candidate] || seen[candidate] || path.Dir(candidate) != directory || !contextSourcePath(candidate) {
					continue
				}
				seen[candidate] = true
				inspected++
				source := read(candidate, pr.HeadSHA)
				var anchors []int
				var related []string
				for _, other := range origins {
					matches := symbolAnchors(source, other.symbols)
					if len(matches) > 0 {
						anchors = append(anchors, matches...)
						related = append(related, other.file)
					}
				}
				if len(anchors) > 0 {
					add(candidate, "HEAD", source, anchors, related, 1536, 8)
				}
			}
		}
	}
	// Preserve cross-file evidence even when the related file is also changed
	// by this PR. Account for relation metadata inside the same byte budget.
	for i, entry := range out {
		for _, origin := range origins {
			if origin.file == entry.Path || len(symbolAnchors(entry.Content, origin.symbols)) == 0 {
				continue
			}
			already := false
			for _, related := range entry.RelatedTo {
				already = already || related == origin.file
			}
			if already {
				continue
			}
			before, _ := json.Marshal(entry)
			related := append(append([]string(nil), entry.RelatedTo...), origin.file)
			updated := entry
			updated.RelatedTo = related
			after, _ := json.Marshal(updated)
			if extra := len(after) - len(before); extra <= remaining {
				entry = updated
				remaining -= extra
			}
		}
		out[i] = entry
	}
	limitations = append(limitations, "source context is sampled: up to 4 changed files, 12 source reads, and a bounded byte budget; omitted code is unavailable")
	return out, limitations
}

func contextSourcePath(file string) bool {
	name := strings.ToLower(path.Base(file))
	if strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".generated.go") || strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".test.") || strings.Contains(name, ".spec.") {
		return false
	}
	switch path.Ext(name) {
	case ".go", ".js", ".jsx", ".ts", ".tsx", ".py", ".rb", ".rs", ".java", ".kt", ".cs", ".c", ".h", ".cpp", ".php", ".swift", ".sql":
		return true
	}
	return false
}

func patchAnchors(patch string) (head, base []int) {
	for _, match := range contextHunk.FindAllStringSubmatch(patch, 8) {
		old, _ := strconv.Atoi(match[1])
		current, _ := strconv.Atoi(match[2])
		head = append(head, max(1, current))
		base = append(base, max(1, old))
	}
	return head, base
}

func sourceExcerpt(source string, anchors []int, radius, budget int) string {
	if source == "" {
		return ""
	}
	if len(anchors) == 0 {
		anchors = []int{1}
	}
	lines := strings.Split(source, "\n")
	// Alternate windows so a large first hunk cannot consume the entire excerpt.
	selected := map[int]bool{}
	used := 0
	for distance := 0; distance <= radius; distance++ {
		for _, anchor := range anchors {
			for _, line := range []int{anchor - distance, anchor + distance} {
				if line < 1 || line > len(lines) || selected[line] {
					continue
				}
				cost := len(strconv.Itoa(line)) + 2 + len(lines[line-1]) + 1
				if used+cost <= budget {
					selected[line] = true
					used += cost
				}
			}
		}
	}
	numbers := make([]int, 0, len(selected))
	for line := range selected {
		numbers = append(numbers, line)
	}
	sort.Ints(numbers)
	var out strings.Builder
	for _, line := range numbers {
		fmt.Fprintf(&out, "%d: %s\n", line, lines[line-1])
	}
	return out.String()
}

func reviewSymbols(patch string) map[string]bool {
	ignored := " func function return class const true false null undefined error string bool int64 context import package public private static this self await async else throw throws value type name void interface struct range case break continue default select switch make append print println Error Sprintf Errorf "
	counts := map[string]int{}
	for _, symbol := range contextIdentifier.FindAllString(patch, -1) {
		if !strings.Contains(ignored, " "+symbol+" ") {
			counts[symbol]++
		}
	}
	var symbols []string
	for symbol := range counts {
		symbols = append(symbols, symbol)
	}
	sort.Slice(symbols, func(i, j int) bool {
		if counts[symbols[i]] != counts[symbols[j]] {
			return counts[symbols[i]] > counts[symbols[j]]
		}
		return symbols[i] < symbols[j]
	})
	out := map[string]bool{}
	for _, symbol := range symbols[:min(24, len(symbols))] {
		out[symbol] = true
	}
	return out
}

func symbolAnchors(source string, symbols map[string]bool) []int {
	var anchors []int
	for i, line := range strings.Split(source, "\n") {
		for _, symbol := range contextIdentifier.FindAllString(line, -1) {
			if symbols[symbol] {
				anchors = append(anchors, i+1)
				break
			}
		}
		if len(anchors) == 6 {
			break
		}
	}
	return anchors
}

func adjacentPriority(file, source string) int {
	stem := strings.TrimSuffix(path.Base(file), path.Ext(file))
	if strings.Contains(source, "./"+stem) || strings.Contains(source, "/"+stem+"\"") || strings.Contains(source, "/"+stem+"'") {
		return 1
	}
	return 0
}
