package application

import (
	"regexp"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func mustRulePattern(t *testing.T, prefix string) *regexp.Regexp {
	t.Helper()
	pattern, err := BuildRuleIDPattern(prefix)
	if err != nil {
		t.Fatal(err)
	}
	return pattern
}

func TestRuleIdentityPattern(t *testing.T) {
	for _, ruleID := range []string{"AIR-GO-001", "AIR-PHP-012", "AIR-SQL-005", "AIR-COMMON-003", "AIR-SEARCH-ENGINE-001"} {
		testigo.Run(t, "accepts "+ruleID, func(t *testing.T) {
			assert.True(t, mustRulePattern(t, "AIR").MatchString(ruleID), "identity should match")
		})
	}

	testigo.Run(t, "rejects another prefix", func(t *testing.T) {
		assert.False(t, mustRulePattern(t, "AIR").MatchString("ACME-GO-001"), "identity should not match")
	})

	testigo.Run(t, "normalizes a lowercase prefix", func(t *testing.T) {
		assert.True(t, mustRulePattern(t, "air").MatchString("AIR-GO-001"), "identity should match")
	})

	testigo.Run(t, "rejects an empty prefix", func(t *testing.T) {
		_, err := BuildRuleIDPattern("  ")
		assert.ErrorContains(t, err, "prefix is empty")
	})
}

func TestRuleIdentityExtraction(t *testing.T) {
	testigo.Run(t, "returns unique identities in encounter order", func(t *testing.T) {
		identities := ExtractRuleIDs("See AIR-PHP-012 and AIR-GO-001. Again AIR-PHP-012.", mustRulePattern(t, "AIR"))
		assert.Equal(t, identities, []string{"AIR-PHP-012", "AIR-GO-001"})
	})

	testigo.Run(t, "returns no identities when text has none", func(t *testing.T) {
		assert.Nil(t, ExtractRuleIDs("nothing relevant here", mustRulePattern(t, "AIR")))
	})

	testigo.Run(t, "returns no identities without a pattern", func(t *testing.T) {
		assert.Nil(t, ExtractRuleIDs("AIR-GO-001", nil))
	})
}

func TestLanguageDetection(t *testing.T) {
	extensions := map[string]string{".go": "go", ".php": "php", ".sql": "sql", "Dockerfile": "docker", "Makefile": "make"}
	for _, scenario := range []struct{ path, want string }{
		{"internal/foo/bar.go", "go"}, {"app/Model.php", "php"}, {"query.sql", "sql"},
		{"Dockerfile", "docker"}, {"build/Makefile", "make"}, {"assets/logo.svg", "unknown"}, {"README", "unknown"},
	} {
		testigo.Run(t, scenario.path, func(t *testing.T) {
			assert.Equal(t, DetectLanguage(scenario.path, extensions), scenario.want)
		})
	}

	testigo.Run(t, "unknown is not common", func(t *testing.T) {
		assert.NotEqual(t, DetectLanguage("assets/logo.svg", map[string]string{".go": "go"}), "common")
	})
}

func TestCommentOriginClassification(t *testing.T) {
	pattern := mustRulePattern(t, "AIR")
	for _, scenario := range []struct {
		name       string
		input      OriginInput
		authors    []string
		wantKind   domain.AuthorKind
		wantOrigin domain.CommentOrigin
		wantIDs    []string
	}{
		{"human", OriginInput{AuthorLogin: "jdoe", Body: "LGTM"}, []string{"claude[bot]"}, domain.AuthorHuman, domain.OriginHuman, nil},
		{"human citing rule", OriginInput{AuthorLogin: "jdoe", Body: "Apply AIR-GO-001"}, []string{"claude[bot]"}, domain.AuthorHuman, domain.OriginHuman, nil},
		{"AI rule usage", OriginInput{AuthorLogin: "claude[bot]", AuthorIsBot: true, Body: "Violates AIR-PHP-012"}, []string{"claude[bot]"}, domain.AuthorAI, domain.OriginAIRuleUsage, []string{"AIR-PHP-012"}},
		{"AI candidate", OriginInput{AuthorLogin: "claude[bot]", AuthorIsBot: true, Body: "Extract this helper"}, []string{"claude[bot]"}, domain.AuthorAI, domain.OriginAICandidate, nil},
		{"bot noise", OriginInput{AuthorLogin: "dependabot[bot]", AuthorIsBot: true, Body: "Bumps dependency"}, []string{"claude[bot]"}, domain.AuthorBot, domain.OriginBotNoise, nil},
		{"AI author case insensitive", OriginInput{AuthorLogin: "Claude[Bot]", AuthorIsBot: true, Body: "No rule"}, []string{"claude[bot]"}, domain.AuthorAI, domain.OriginAICandidate, nil},
		{"explicit AI author beats bot flag", OriginInput{AuthorLogin: "claude[bot]", Body: "Uses AIR-SQL-005"}, []string{"claude[bot]"}, domain.AuthorAI, domain.OriginAIRuleUsage, []string{"AIR-SQL-005"}},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			kind, origin, identities := ClassifyOrigin(scenario.input, scenario.authors, pattern)
			assert.Equal(t, kind, scenario.wantKind)
			assert.Equal(t, origin, scenario.wantOrigin)
			assert.Equal(t, identities, scenario.wantIDs)
		})
	}
}
