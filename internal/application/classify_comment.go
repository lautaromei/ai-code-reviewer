package application

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

func BuildRuleIDPattern(prefix string) (*regexp.Regexp, error) {
	normalized := strings.ToUpper(strings.TrimSpace(prefix))
	if normalized == "" {
		return nil, fmt.Errorf("application: rule prefix is empty")
	}
	escaped := regexp.QuoteMeta(normalized)
	pattern := `\b` + escaped + `(?:-[A-Z][A-Z0-9]*)+-[0-9]{3,}\b`
	return regexp.Compile(pattern)
}

func ExtractRuleIDs(body string, pattern *regexp.Regexp) []string {
	if pattern == nil {
		return nil
	}
	matches := pattern.FindAllString(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(matches))
	ids := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		ids = append(ids, m)
	}
	return ids
}

func DetectLanguage(path string, extensionToLanguage map[string]string) string {
	if len(extensionToLanguage) == 0 {
		return "unknown"
	}

	base := filepath.Base(path)
	if lang, ok := extensionToLanguage[base]; ok {
		return lang
	}

	ext := filepath.Ext(path)
	if ext != "" {
		if lang, ok := extensionToLanguage[ext]; ok {
			return lang
		}
	}

	return "unknown"
}

type OriginInput struct {
	AuthorLogin string
	AuthorIsBot bool
	Body        string
}

func ClassifyOrigin(input OriginInput, aiAuthors []string, rulePattern *regexp.Regexp) (domain.AuthorKind, domain.CommentOrigin, []string) {
	if strings.Contains(input.Body, "<!-- ai-reviewer:finding:") {
		return domain.AuthorAI, domain.OriginAIRuleUsage, ExtractRuleIDs(input.Body, rulePattern)
	}
	isAIAuthor := false
	for _, author := range aiAuthors {
		if strings.EqualFold(author, input.AuthorLogin) {
			isAIAuthor = true
			break
		}
	}

	if isAIAuthor {
		ids := ExtractRuleIDs(input.Body, rulePattern)
		if len(ids) > 0 {
			return domain.AuthorAI, domain.OriginAIRuleUsage, ids
		}
		return domain.AuthorAI, domain.OriginAICandidate, nil
	}

	if !input.AuthorIsBot {
		return domain.AuthorHuman, domain.OriginHuman, nil
	}

	return domain.AuthorBot, domain.OriginBotNoise, nil
}
