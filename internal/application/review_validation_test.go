package application

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type countingReviewModel struct {
	mu    sync.Mutex
	calls int
}

func (m *countingReviewModel) Review(context.Context, string, ports.ReviewPayload) (ports.ReviewModelResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return ports.ReviewModelResult{Findings: []ports.ModelFinding{{Body: "finding"}}, InputTokens: 10, OutputTokens: 2}, nil
}

func TestGenerateReviewCandidatesRunsConfiguredAgents(t *testing.T) {
	model := &countingReviewModel{}
	models := []GenerationModel{{Name: "haiku_max", Model: model}, {Name: "sonnet", Model: model}, {Name: "opus", Model: model}}
	got, cost, err := generateReviewCandidates(context.Background(), models, "prompt", ports.ReviewPayload{})
	if err != nil || model.calls != 3 || len(got.Findings) != 3 || got.InputTokens != 30 || got.OutputTokens != 6 || cost == 0 {
		t.Fatalf("unexpected aggregate: calls=%d result=%#v err=%v", model.calls, got, err)
	}
}

func TestValidateFindingsKeepsIndependentComments(t *testing.T) {
	rules := []ports.ReviewRule{{RuleID: "AIR-GO-001", Scopes: []string{"**/*.go"}}}
	files := []ports.ChangedFile{{Path: "main.go", Patch: "@@ -1,1 +1,3 @@\n old\n+first\n+second"}}
	input := []ports.ModelFinding{
		{Kind: "catalog_rule", RuleID: "AIR-GO-001", Path: "main.go", Line: 2, Confidence: 80, Body: "first"},
		{Kind: "catalog_rule", RuleID: "AIR-GO-001", Path: "main.go", Line: 3, Confidence: 80, Body: "second"},
	}

	got, rejected := validateFindings(input, rules, files, 20)
	if len(got) != 2 || len(rejected) != 0 {
		t.Fatalf("expected two valid findings, got %d valid and %v rejected", len(got), rejected)
	}
}

func TestPublicationPreservesMasterPromptMarkdown(t *testing.T) {
	markdown := "## 💡 Suggestions\n\n`main.go:7` · `AIR-GO-001` · 82% — Keep this exact structure."
	body := publicationBody(domain.ReviewFinding{Body: markdown, Confidence: 82}, "<!-- marker -->", "")
	if body != "💡 · 82% — Keep this exact structure.\n\n<!-- marker -->" {
		t.Fatalf("publication did not normalize finding markdown: %q", body)
	}
	summary := "## Summary\n\nConcise review.\n\n**Verdict: COMMENT**"
	got := publicationSummary(summary, "abc")
	if !strings.HasPrefix(got, summary) {
		t.Fatalf("publication changed summary markdown: %q", got)
	}
}

func TestPublicationLinksCatalogRule(t *testing.T) {
	ruleID := "AIR-GO-001"
	body := publicationBody(domain.ReviewFinding{RuleID: &ruleID, Body: "[AIR-GO-001] — Avoid this."}, "<!-- marker -->", "https://review.example")
	want := "[AIR-GO-001](https://review.example/rules/AIR-GO-001)"
	if !strings.Contains(body, want) {
		t.Fatalf("expected linked rule reference %q in %q", want, body)
	}
}

func TestPublicationUsesCompactSingleLineHeader(t *testing.T) {
	ruleID := "AIR-COMMON-007"
	body := publicationBody(domain.ReviewFinding{RuleID: &ruleID, Confidence: 60, Severity: "medium", Body: "This binary should not be committed."}, "<!-- marker -->", "https://review.example")
	want := "💡 [AIR-COMMON-007](https://review.example/rules/AIR-COMMON-007) · 60% — This binary should not be committed."
	if !strings.HasPrefix(body, want) {
		t.Fatalf("expected compact linked header %q in %q", want, body)
	}
}

func TestPublicationPreservesSuggestionIndentation(t *testing.T) {
	body := publicationBody(domain.ReviewFinding{Body: "Explain the change.", Suggestion: "\n\t\ttimeout = minRateTimeout\n"}, "<!-- marker -->", "")
	if !strings.Contains(body, "```suggestion\n\t\ttimeout = minRateTimeout\n```") {
		t.Fatalf("suggestion indentation changed: %q", body)
	}
}

func TestPublicationPreservesUsefulBodyParagraphs(t *testing.T) {
	body := publicationBody(domain.ReviewFinding{
		Body:       "The resource is never closed.\n\nThis leaks a connection on every request. Close it with defer.",
		Confidence: 75,
		Severity:   "medium",
	}, "<!-- marker -->", "")
	for _, want := range []string{
		"The resource is never closed.",
		"This leaks a connection on every request. Close it with defer.",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("publication dropped useful finding content %q from %q", want, body)
		}
	}
}

func TestValidateFindingsDowngradesUncommentableLine(t *testing.T) {
	rules := []ports.ReviewRule{{RuleID: "AIR-GO-001", Scopes: []string{"**/*.go"}}}
	files := []ports.ChangedFile{{Path: "main.go", Patch: "@@ -1,1 +1,1 @@\n+first"}}

	got, rejected := validateFindings([]ports.ModelFinding{{Kind: "catalog_rule", RuleID: "AIR-GO-001", Path: "main.go", Line: 99, Confidence: 80, Body: "finding"}}, rules, files, 20)
	if len(got) != 1 || got[0].Path != "" || got[0].Line != 0 || len(rejected) != 1 {
		t.Fatalf("expected a general fallback comment, got %#v and %v", got, rejected)
	}
}

func TestValidateFindingsKeepsOneThreadPerLocation(t *testing.T) {
	rules := []ports.ReviewRule{{RuleID: "AIR-GO-001", Scopes: []string{"**/*.go"}}}
	files := []ports.ChangedFile{{Path: "main.go", Patch: "@@ -1,1 +1,1 @@\n+first"}}
	input := []ports.ModelFinding{
		{Kind: "catalog_rule", RuleID: "AIR-GO-001", Path: "main.go", Line: 1, Confidence: 90, Body: "primary issue"},
		{Kind: "general", Path: "main.go", Line: 1, Confidence: 80, Body: "same location"},
	}

	got, rejected := validateFindings(input, rules, files, 20)
	if len(got) != 1 || len(rejected) != 1 {
		t.Fatalf("expected one review thread, got %#v and %v", got, rejected)
	}
}

func TestValidateFindingsRejectsEmptyOrHeadingOnlyBody(t *testing.T) {
	input := []ports.ModelFinding{
		{CandidateID: "empty", Kind: "general", Confidence: 80, Body: ""},
		{CandidateID: "heading", Kind: "general", Confidence: 80, Body: "## Finding"},
	}
	observed := map[string]string{}

	got, rejected := validateFindingsObserved(input, nil, nil, 20, func(f ports.ModelFinding, reason string) {
		observed[f.CandidateID] = reason
	})

	if len(got) != 0 || len(rejected) != 2 {
		t.Fatalf("expected both empty findings to be rejected, got %#v and %v", got, rejected)
	}
	if observed["empty"] != "empty_body" || observed["heading"] != "empty_body" {
		t.Fatalf("expected auditable empty_body reasons, got %#v", observed)
	}
}

func TestValidateFindingsObservesEveryDiscardedCandidate(t *testing.T) {
	files := []ports.ChangedFile{{Path: "main.go", Patch: "@@ -1,1 +1,1 @@\n+first"}}
	input := []ports.ModelFinding{
		{CandidateID: "low", Kind: "general", Path: "main.go", Line: 1, Confidence: 30, Body: "low confidence"},
		{CandidateID: "kept", Kind: "general", Path: "main.go", Line: 1, Confidence: 90, Body: "valid finding"},
		{CandidateID: "over-limit", Kind: "general", Path: "other.go", Line: 1, Confidence: 90, Body: "over limit"},
	}
	observed := map[string]string{}

	got, _ := validateFindingsObserved(input, nil, files, 1, func(f ports.ModelFinding, reason string) {
		observed[f.CandidateID] = reason
	})

	if len(got) != 1 || got[0].CandidateID != "kept" {
		t.Fatalf("expected the valid finding to be retained, got %#v", got)
	}
	if observed["low"] != "confidence_below_threshold" || observed["over-limit"] != "max_findings_reached" {
		t.Fatalf("expected every discarded candidate and reason, got %#v", observed)
	}
}

func TestSelectVerifiedFindingsCannotInventFindings(t *testing.T) {
	candidates := []ports.ModelFinding{{CandidateID: "candidate-1", Body: "keep"}, {CandidateID: "candidate-2", Body: "drop"}}
	verdicts := []ports.ModelFinding{{CandidateID: "candidate-1", Verdict: "accept"}, {CandidateID: "invented", Verdict: "reject", Reason: "not real"}}

	if _, _, err := selectVerifiedFindings(candidates, verdicts); err == nil {
		t.Fatal("expected an invented candidate ID to fail verification")
	}
}

func TestSelectVerifiedFindingsExcludesCandidatesOmittedByVerifier(t *testing.T) {
	candidates := []ports.ModelFinding{{CandidateID: "candidate-1", Body: "keep"}}
	got, decisions, err := selectVerifiedFindings(candidates, nil)
	if err != nil || len(got) != 0 || len(decisions) != 1 || decisions[0].Verdict != "missing" {
		t.Fatalf("expected an incomplete verifier result to exclude and record the candidate, got %#v, %#v, %v", got, decisions, err)
	}
}

func TestSelectVerifiedFindingsUnifiesDuplicateCandidates(t *testing.T) {
	candidates := []ports.ModelFinding{{CandidateID: "canonical", Body: "clear explanation"}, {CandidateID: "copy", Body: "same issue"}}
	verdicts := []ports.ModelFinding{{CandidateID: "canonical", Verdict: "accept"}, {CandidateID: "copy", Verdict: "duplicate", DuplicateOf: "canonical", Reason: "same underlying issue"}}

	got, decisions, err := selectVerifiedFindings(candidates, verdicts)
	if err != nil || len(got) != 1 || got[0].CandidateID != "canonical" {
		t.Fatalf("expected one canonical finding, got %#v, %v", got, err)
	}
	if len(decisions) != 2 || decisions[1].Verdict != "duplicate" || decisions[1].DuplicateOf != "canonical" {
		t.Fatalf("expected both verification decisions to be recorded, got %#v", decisions)
	}
}

func TestSelectVerifiedFindingsRejectsInvalidDuplicateTarget(t *testing.T) {
	candidates := []ports.ModelFinding{{CandidateID: "candidate-1", Body: "issue"}}
	verdicts := []ports.ModelFinding{{CandidateID: "candidate-1", Verdict: "duplicate", DuplicateOf: "missing"}}
	if _, _, err := selectVerifiedFindings(candidates, verdicts); err == nil {
		t.Fatal("expected an unknown duplicate target to fail verification")
	}
}

func TestSelectVerifiedFindingsRejectsUnknownCandidateIDs(t *testing.T) {
	candidates := []ports.ModelFinding{{CandidateID: "candidate-1", Body: "keep"}}
	verdicts := []ports.ModelFinding{{CandidateID: "unknown", Verdict: "accept"}}
	if _, _, err := selectVerifiedFindings(candidates, verdicts); err == nil {
		t.Fatal("expected non-empty verifier output with unknown IDs to fail")
	}
}

func TestSelectVerifiedFindingsPublishesOnlyExplicitAccepts(t *testing.T) {
	candidates := []ports.ModelFinding{
		{CandidateID: "accepted", Body: "accept"},
		{CandidateID: "uncertain", Body: "uncertain"},
		{CandidateID: "rejected", Body: "reject"},
		{CandidateID: "missing-reason", Body: "preserve"},
		{CandidateID: "low-value", Body: "cosmetic"},
	}
	verdicts := []ports.ModelFinding{
		{CandidateID: "accepted", Verdict: "accept"},
		{CandidateID: "uncertain", Verdict: "uncertain", Reason: "needs runtime context"},
		{CandidateID: "rejected", Verdict: "reject", Reason: "the cited line was removed"},
		{CandidateID: "missing-reason", Verdict: "reject"},
		{CandidateID: "low-value", Verdict: "low_value", Reason: "no meaningful impact"},
	}

	got, decisions, err := selectVerifiedFindings(candidates, verdicts)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CandidateID != "accepted" {
		t.Fatalf("expected only the explicit accept to be published, got %#v", got)
	}
	if len(decisions) != 5 || decisions[1].Verdict != "uncertain" || decisions[2].Reason == "" || decisions[3].Verdict != "reject" || decisions[4].Verdict != "low_value" {
		t.Fatalf("expected every decision and reason to be retained, got %#v", decisions)
	}
}

func TestSelectVerifiedFindingsRequiresAcceptedDuplicateTarget(t *testing.T) {
	candidates := []ports.ModelFinding{{CandidateID: "canonical"}, {CandidateID: "copy"}}
	verdicts := []ports.ModelFinding{{CandidateID: "canonical", Verdict: "uncertain"}, {CandidateID: "copy", Verdict: "duplicate", DuplicateOf: "canonical"}}
	if _, _, err := selectVerifiedFindings(candidates, verdicts); err == nil {
		t.Fatal("expected a duplicate pointing to a non-accepted candidate to fail")
	}
}

func TestFinalReviewSummaryPreservesOriginalWhenNothingWasDropped(t *testing.T) {
	original := "## Summary\n\nKeep the master prompt format."
	findings := []ports.ModelFinding{{Body: "finding"}}
	if got := finalReviewSummary(original, findings, 1); got != original {
		t.Fatalf("expected original summary, got %q", got)
	}
}

func TestFinalReviewSummaryReflectsVerifiedFindings(t *testing.T) {
	findings := []ports.ModelFinding{
		{Path: "main.go", Line: 7, Confidence: 90, Severity: "high", Body: "### Heading\n\n- Fix the verified problem.\n- More context."},
		{Path: "one.go", Line: 1, Confidence: 80, Body: "First additional issue."},
		{Path: "two.go", Line: 2, Confidence: 80, Body: "Second additional issue."},
		{Path: "three.go", Line: 3, Confidence: 80, Body: "Third additional issue."},
	}
	got := finalReviewSummary("stale summary", findings, 5)
	for _, want := range []string{
		"Found 4 actionable issues:",
		"🚨 `main.go:7` · 90% — Fix the verified problem.",
		"💡 `one.go:1` · 80% — First additional issue.",
		"💡 `two.go:2` · 80% — Second additional issue.",
		"💡 `three.go:3` · 80% — Third additional issue.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in regenerated summary: %q", want, got)
		}
	}
	if strings.Contains(got, "\n- ") {
		t.Fatalf("expected prose formatting without bullet points: %q", got)
	}
}

func TestFinalReviewSummaryApprovesWhenVerifierDropsEverything(t *testing.T) {
	got := finalReviewSummary("stale request changes", nil, 1)
	if got != "✅ No actionable issues found in the proposed changes." {
		t.Fatalf("expected a fresh approval summary, got %q", got)
	}
}

func TestReviewCostIncludesBothCallsAndCache(t *testing.T) {
	got := reviewCostMicroUSD("sonnet", 4, 7432, 106620, 5462)
	if got != 341424 {
		t.Fatalf("expected 341424 micro-USD, got %d", got)
	}
	if opus := reviewCostMicroUSD("opus", 1000, 100, 0, 0); opus != 6000 {
		t.Fatalf("expected 6000 micro-USD for Opus, got %d", opus)
	}
	if haiku := reviewCostMicroUSD("haiku_max", 1000, 100, 0, 0); haiku != 150 {
		t.Fatalf("expected 150 micro-USD for Haiku Max, got %d", haiku)
	}
	if longHaiku := reviewCostMicroUSD("haiku_max", 100001, 100, 0, 0); longHaiku != 50251 {
		t.Fatalf("expected 50251 micro-USD for long-context Haiku Max, got %d", longHaiku)
	}
	if jev := reviewCostMicroUSD("jev", 1_000_000, 500_000, 0, 0); jev != 42000 {
		t.Fatalf("expected 42000 micro-USD for Jev, got %d", jev)
	}
}
