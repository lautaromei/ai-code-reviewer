package markdown

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

func Render(title string, rules []domain.Rule) []byte {
	byLanguage := make(map[string][]domain.Rule)
	for _, rule := range rules {
		byLanguage[rule.Language] = append(byLanguage[rule.Language], rule)
	}

	languages := make([]string, 0, len(byLanguage))
	for lang := range byLanguage {
		languages = append(languages, lang)
	}
	sort.Strings(languages)

	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", title)

	for _, lang := range languages {
		group := byLanguage[lang]
		sort.Slice(group, func(i, j int) bool {
			return group[i].RuleID < group[j].RuleID
		})

		fmt.Fprintf(&b, "\n# %s\n", lang)
		for _, rule := range group {
			fmt.Fprintf(&b, "\n## %s — %s\n\n%s\n", rule.RuleID, rule.Title, rule.Body)
		}
	}

	return []byte(b.String())
}

// Merge appends the rendered rule snapshot to the repository-owned base content.
func Merge(base []byte, title string, rules []domain.Rule) []byte {
	rendered := Render(title, rules)
	if len(strings.TrimSpace(string(base))) == 0 {
		return rendered
	}

	merged := make([]byte, 0, len(base)+1+len(rendered))
	merged = append(merged, base...)
	if len(merged) > 0 && merged[len(merged)-1] != '\n' {
		merged = append(merged, '\n')
	}
	merged = append(merged, '\n')
	merged = append(merged, rendered...)
	return merged
}
