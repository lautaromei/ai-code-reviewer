package typesafejev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
)

func TestReviewMapsTypedJevChoicesToVerificationDecisions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" || r.URL.Path != "/v1/systemone" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("TypeSafe-Danger-Zone-Long-Context") != "true" {
			t.Fatal("long-context mode was not enabled")
		}
		var request systemOneRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.Model != "jev-latest" || len(request.Questions) != 3 {
			t.Fatalf("unexpected request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"model":"jev-1.0","answers":{"candidate_0":{"type":"choice","choice":"accept","confidence":0.96},"candidate_1":{"type":"choice","choice":"duplicate_of__candidate-1","confidence":0.91},"candidate_2":{"type":"choice","choice":"low_value","confidence":0.88}},"usage":{"input_tokens":42,"output_tokens":3}}`))
	}))
	defer server.Close()

	client := New("secret", WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	result, err := client.Review(context.Background(), "policy", ports.ReviewPayload{Stage: "verification", CandidateFindings: []ports.ModelFinding{{CandidateID: "candidate-1"}, {CandidateID: "candidate-2"}, {CandidateID: "candidate-3"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 3 || result.Findings[0].Verdict != "accept" || result.Findings[1].Verdict != "duplicate" || result.Findings[1].DuplicateOf != "candidate-1" || result.Findings[2].Verdict != "low_value" {
		t.Fatalf("unexpected decisions: %#v", result.Findings)
	}
	if result.InputTokens != 42 || result.OutputTokens != 3 || result.Model != "jev-1.0" {
		t.Fatalf("unexpected metadata: %#v", result)
	}
}

func TestReviewRejectsGenerationStage(t *testing.T) {
	if _, err := New("secret").Review(context.Background(), "", ports.ReviewPayload{Stage: "review"}); err == nil {
		t.Fatal("expected Jev generation to be rejected")
	}
}

func TestReviewSendsOnlyEvidenceReferencedByCandidates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request systemOneRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		state, ok := request.State.(map[string]any)
		if !ok {
			t.Fatalf("unexpected state type: %T", request.State)
		}
		files := state["changed_files"].([]any)
		rules := state["rules"].([]any)
		if len(files) != 1 || files[0].(map[string]any)["path"] != "kept.go" {
			t.Fatalf("unexpected files sent to Jev: %#v", files)
		}
		if len(rules) != 1 || rules[0].(map[string]any)["rule_id"] != "GO-1" {
			t.Fatalf("unexpected rules sent to Jev: %#v", rules)
		}
		pullRequest := state["pull_request"].(map[string]any)
		if _, exists := pullRequest["body"]; exists {
			t.Fatalf("pull request body should not be duplicated in Jev state: %#v", pullRequest)
		}
		_, _ = w.Write([]byte(`{"model":"jev-1.0","answers":{"candidate_0":{"type":"choice","choice":"accept","confidence":0.9}},"usage":{"input_tokens":10,"output_tokens":1}}`))
	}))
	defer server.Close()

	payload := ports.ReviewPayload{
		Stage:       "verification",
		PullRequest: ports.PullRequest{Number: 4, Title: "Change", Body: strings.Repeat("large body", 100)},
		ChangedFiles: []ports.ChangedFile{
			{Path: "kept.go", Patch: "+bug"},
			{Path: "unrelated.go", Patch: strings.Repeat("+large\n", 1000)},
		},
		Rules:             []ports.ReviewRule{{RuleID: "GO-1", Rule: "kept"}, {RuleID: "COMMON-1", Rule: strings.Repeat("large", 1000)}},
		CandidateFindings: []ports.ModelFinding{{CandidateID: "candidate-1", RuleID: "GO-1", Path: "kept.go", Body: "bug"}},
	}
	if _, err := New("secret", WithBaseURL(server.URL), WithHTTPClient(server.Client())).Review(context.Background(), "policy", payload); err != nil {
		t.Fatal(err)
	}
}
