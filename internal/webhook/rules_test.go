package webhook

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type ruleReaderStub struct{ rule domain.Rule }

func (s ruleReaderStub) Get(context.Context, string) (domain.Rule, bool, error) {
	return s.rule, true, nil
}
func (s ruleReaderStub) ListReferences(context.Context, string) ([]domain.RuleReference, error) {
	return nil, nil
}

func TestRulePageRendersNormalizedFields(t *testing.T) {
	rule := domain.Rule{RuleID: "AIR-COMMON-014", Scope: "**/*", Rule: "Use narrow visibility.", Exception: "Shared helpers are protected.", BadExample: "public helper", GoodExample: "private helper"}
	w := httptest.NewRecorder()
	RulePages(ruleReaderStub{rule: rule}).ServeHTTP(w, httptest.NewRequest("GET", "/rules/AIR-COMMON-014", nil))
	body := w.Body.String()
	for _, want := range []string{"<h2>Scope</h2>", "<h2>Rule</h2>", "<h2>Exception</h2>", "<h2>Bad</h2>", "<h2>Good</h2>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("page does not contain %q", want)
		}
	}
	for _, want := range []string{`class="field rule-quote"`, `<blockquote>Use narrow visibility.</blockquote>`, `font-weight:400`, `class="field scope"`, `class="field bad"`, `class="field good"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("page does not contain %q", want)
		}
	}
	if strings.Index(body, "<h2>Rule</h2>") > strings.Index(body, "<h2>Scope</h2>") {
		t.Fatal("rule must be rendered before scope")
	}
}
