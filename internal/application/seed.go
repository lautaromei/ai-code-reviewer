package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

var headerPattern = regexp.MustCompile(`^## (\S+) — (.+)$`)

var yearMonthPattern = regexp.MustCompile(`\b(\d{4})-(\d{2})\b`)

type SeedImportSummary struct {
	FilesScanned    int
	RulesFound      int
	Inserted        int
	SkippedExisting int
	ParseErrors     []string
}

type seedRules interface {
	Get(context.Context, string) (domain.Rule, bool, error)
	Insert(context.Context, domain.Rule) error
}

type seedBlock struct {
	file   string
	line   int
	ruleID string
	title  string
	body   string
}

func ImportSeeds(ctx context.Context, rules seedRules, clock ports.Clock, seedDir string) (SeedImportSummary, error) {
	summary := SeedImportSummary{}

	entries, err := os.ReadDir(seedDir)
	if err != nil {
		return summary, fmt.Errorf("import seeds: read %s: %w", seedDir, err)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	var blocks []seedBlock
	for _, name := range names {
		summary.FilesScanned++
		path := filepath.Join(seedDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			summary.ParseErrors = append(summary.ParseErrors, fmt.Sprintf("%s: read file: %v", name, err))
			continue
		}
		fileBlocks, errs := parseSeedFile(name, string(content))
		blocks = append(blocks, fileBlocks...)
		summary.ParseErrors = append(summary.ParseErrors, errs...)
	}
	summary.RulesFound = len(blocks)

	if rules == nil {
		return summary, fmt.Errorf("import seeds: rules dependency is nil")
	}
	for _, block := range blocks {
		language, ok := inferLanguage(block.ruleID)
		if !ok {
			summary.ParseErrors = append(summary.ParseErrors, fmt.Sprintf("%s:%d: could not infer language for %q", block.file, block.line, block.ruleID))
			continue
		}

		_, exists, err := rules.Get(ctx, block.ruleID)
		if err != nil {
			return summary, fmt.Errorf("import seeds: look up %s: %w", block.ruleID, err)
		}
		if exists {
			summary.SkippedExisting++
			continue
		}

		rule := domain.Rule{
			RuleID:      block.ruleID,
			Language:    language,
			Title:       block.title,
			Body:        block.body,
			Status:      parseStatus(block.body),
			CreatedAt:   clock.Now(),
			ActivatedAt: parseActivatedAt(block.body, clock),
			Source:      "seed",
			ContentHash: contentHash(block.body),
		}
		rule.Scope, rule.Rule, rule.Exception, rule.BadExample, rule.GoodExample = parseNormalizedRule(block.body)
		if rule.Rule != "" {
			rule.Body = rule.Rule
		}

		if err := rules.Insert(ctx, rule); err != nil {
			return summary, fmt.Errorf("import seeds: insert %s: %w", block.ruleID, err)
		}
		summary.Inserted++
	}

	return summary, nil
}

// parseNormalizedRule extracts the stable rule sections used by model prompts.
// Values may continue on indented lines until the next labelled field.
func parseNormalizedRule(body string) (scope, rule, exception, bad, good string) {
	values := map[string][]string{}
	current := ""
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		matched := false
		for _, field := range []string{"Scope", "Rule", "Exception", "Bad", "Good"} {
			prefix := "- **" + field + ":**"
			if strings.HasPrefix(trimmed, prefix) {
				current = field
				value := strings.TrimSpace(strings.TrimPrefix(trimmed, prefix))
				if value != "" {
					values[current] = append(values[current], value)
				}
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		if current != "" && trimmed != "" && !strings.HasPrefix(trimmed, "- **") {
			values[current] = append(values[current], trimmed)
		}
	}
	join := func(key string) string { return strings.TrimSpace(strings.Join(values[key], " ")) }
	return strings.Trim(join("Scope"), "`"), join("Rule"), join("Exception"), join("Bad"), join("Good")
}

func parseSeedFile(fileName string, content string) ([]seedBlock, []string) {
	lines := strings.Split(content, "\n")

	type headerHit struct {
		lineIdx int
		ruleID  string
		title   string
	}

	var headers []headerHit
	var errs []string
	for i, line := range lines {
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		m := headerPattern.FindStringSubmatch(line)
		if m == nil {
			errs = append(errs, fmt.Sprintf("%s:%d: unrecognized header: %q", fileName, i+1, line))
			continue
		}
		headers = append(headers, headerHit{lineIdx: i, ruleID: m[1], title: m[2]})
	}

	blocks := make([]seedBlock, 0, len(headers))
	for idx, h := range headers {
		end := len(lines)
		if idx+1 < len(headers) {
			end = headers[idx+1].lineIdx
		}
		bodyLines := lines[h.lineIdx+1 : end]

		start := 0
		for start < len(bodyLines) && strings.TrimSpace(bodyLines[start]) == "" {
			start++
		}
		stop := len(bodyLines)
		for stop > start && strings.TrimSpace(bodyLines[stop-1]) == "" {
			stop--
		}
		body := strings.Join(bodyLines[start:stop], "\n")

		blocks = append(blocks, seedBlock{
			file:   fileName,
			line:   h.lineIdx + 1,
			ruleID: h.ruleID,
			title:  h.title,
			body:   body,
		})
	}

	return blocks, errs
}

func inferLanguage(ruleID string) (string, bool) {
	segments := strings.Split(ruleID, "-")
	if len(segments) < 3 {
		return "", false
	}
	last := segments[len(segments)-1]
	if _, err := strconv.Atoi(last); err != nil {
		return "", false
	}
	langSegments := segments[1 : len(segments)-1]
	return strings.ToLower(strings.Join(langSegments, "-")), true
}

func parseStatus(body string) domain.RuleStatus {
	line := statusLine(body)
	if line == "" {
		return domain.RuleActive
	}
	fields := strings.Fields(line)
	switch strings.ToLower(fields[0]) {
	case string(domain.RuleDisabled):
		return domain.RuleDisabled
	case string(domain.RuleExpired):
		return domain.RuleExpired
	default:
		return domain.RuleActive
	}
}

func parseActivatedAt(body string, clock ports.Clock) time.Time {
	line := statusLine(body)
	if line == "" {
		return clock.Now()
	}
	m := yearMonthPattern.FindStringSubmatch(line)
	if m == nil {
		return clock.Now()
	}
	activatedAt, err := time.Parse("2006-01", m[1]+"-"+m[2])
	if err != nil {
		return clock.Now()
	}
	return activatedAt.UTC()
}

func statusLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "- **Status:**") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "- **Status:**"))
		}
	}
	return ""
}

func contentHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}
