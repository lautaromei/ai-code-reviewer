package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
)

func TestReviewCachesMasterPromptAndRulesOutsideDynamicPayload(t *testing.T) {
	var request map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(messageResponse{Model: "claude-opus-5-5", Content: []contentBlock{{Type: "tool_use", Name: "emit_review", Input: json.RawMessage(`{"summary":"No issues found.","findings":[],"limitations":[]}`)}}, StopReason: "tool_use"})
	}))
	defer server.Close()

	client := New("test-key", "claude-opus-5-5", "v2", WithBaseURL(server.URL))
	_, err := client.Review(context.Background(), "MASTER PROMPT", ports.ReviewPayload{Stage: "review", Rules: []ports.ReviewRule{{RuleID: "AIR-GO-001", Body: "RULE BODY"}}})
	if err != nil {
		t.Fatal(err)
	}
	blocks, ok := request["system"].([]any)
	if !ok || len(blocks) != 4 {
		t.Fatalf("expected four system blocks, got %#v", request["system"])
	}
	instructions, ok := blocks[0].(map[string]any)
	if !ok || !strings.Contains(instructions["text"].(string), "mandatory review checklist") ||
		!strings.Contains(instructions["text"].(string), "every changed file against every supplied rule") {
		t.Fatalf("expected catalog-first review instructions, got %#v", blocks[0])
	}
	cached, ok := blocks[2].(map[string]any)
	if !ok || cached["cache_control"] == nil || !strings.Contains(cached["text"].(string), "AIR-GO-001") {
		t.Fatalf("expected cached rule catalog block, got %#v", blocks[2])
	}
	messages := request["messages"].([]any)
	content := messages[0].(map[string]any)["content"].(string)
	if strings.Contains(content, "RULE BODY") || strings.Contains(content, "MASTER PROMPT") {
		t.Fatalf("dynamic payload duplicated cached context: %s", content)
	}
}

func TestReviewVerificationAllowsAllCandidatesToBeRejected(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(messageResponse{
			Model: "claude-opus-5-5",
			Content: []contentBlock{{
				Type:  "tool_use",
				Name:  "emit_review",
				Input: json.RawMessage(`{"summary":"All candidate findings were rejected.","findings":[],"limitations":[]}`),
			}},
			StopReason: "tool_use",
		})
	}))
	defer server.Close()

	client := New("test-key", "claude-opus-5-5", "v2", WithBaseURL(server.URL))
	result, err := client.Review(context.Background(), "prompt", ports.ReviewPayload{Stage: "verification"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(result.Findings) != 0 {
		t.Fatalf("expected one verification call with no accepted findings, got %d calls and %#v", calls, result.Findings)
	}
}

func TestReviewVerificationUsesVerdictOnlySchema(t *testing.T) {
	var request messageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(messageResponse{
			Model: "claude-sonnet-4-5",
			Content: []contentBlock{{Type: "tool_use", Name: "emit_review", Input: json.RawMessage(
				`{"summary":"verified","findings":[{"candidate_id":"candidate-1","verdict":"uncertain","reason":"runtime context is unavailable"}],"limitations":[]}`,
			)}},
			StopReason: "tool_use",
		})
	}))
	defer server.Close()

	client := New("test-key", "claude-sonnet-4-5", "v2", WithBaseURL(server.URL))
	result, err := client.Review(context.Background(), "prompt", ports.ReviewPayload{Stage: "verification", CandidateFindings: []ports.ModelFinding{{CandidateID: "candidate-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	required := request.Tools[0].InputSchema["properties"].(map[string]any)["findings"].(map[string]any)["items"].(map[string]any)["required"].([]any)
	if strings.Join(anyStrings(required), ",") != "candidate_id,verdict,reason" {
		t.Fatalf("unexpected verification fields: %#v", required)
	}
	if len(result.Findings) != 1 || result.Findings[0].Verdict != "uncertain" || result.Findings[0].Reason == "" {
		t.Fatalf("unexpected verification result: %#v", result.Findings)
	}
}

func anyStrings(values []any) []string {
	out := make([]string, len(values))
	for i, value := range values {
		out[i], _ = value.(string)
	}
	return out
}
