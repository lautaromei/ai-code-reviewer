package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/anthropic/anthropictest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func newTestServer(t *testing.T, toolName string, toolInput map[string]any) (*httptest.Server, *Client) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req messageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Error(err)
			return
		}
		if req.ToolChoice.Type != "auto" || req.ToolChoice.Name != "" {
			t.Errorf("tool choice: got %#v, want auto", req.ToolChoice)
			return
		}
		if req.Thinking == nil || req.Thinking.Type != "between_tools" {
			t.Errorf("thinking: got %#v, want between_tools", req.Thinking)
			return
		}

		rawInput, err := json.Marshal(toolInput)
		if err != nil {
			t.Error(err)
			return
		}

		resp := messageResponse{
			Model: "test-model",
			Content: []contentBlock{
				{Type: "tool_use", Name: toolName, Input: rawInput},
			},
			StopReason: "tool_use",
		}
		w.Header().Set("content-type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	client := New("test-key", "claude-sonnet-5-5", "v1", WithBaseURL(server.URL))
	return server, client
}

func classificationClient(t *testing.T) *Client {
	t.Helper()
	_, client := newTestServer(t, "emit_classification", map[string]any{
		"verdicts": []map[string]any{
			{"comment_id": 1, "verdict": "covered", "rule_id": "AIR-GO-001"},
			{"comment_id": 2, "verdict": "new", "candidate_key": "group-imports"},
			{"comment_id": 3, "verdict": "one_off"},
		},
	})
	return client
}

func classifyInput() ports.ClassificationInput {
	return anthropictest.ClassificationRequest([]ports.CommentInput{
		anthropictest.GoComment(1, "usa gofmt"),
		anthropictest.GoComment(2, "group imports"),
		anthropictest.GoComment(3, "lgtm"),
	}, []ports.CatalogEntry{anthropictest.ActiveCatalogEntry("AIR-GO-001", "Format with gofmt")})
}

func TestClassificationConversation(t *testing.T) {
	testigo.Run(t, "returns the responding model", func(t *testing.T) {
		result, err := classificationClient(t).Classify(context.Background(), classifyInput())
		assert.NoError(t, err)
		assert.Equal(t, result.Model, "claude-sonnet-5-5")
	})

	testigo.Run(t, "maps a covered verdict", func(t *testing.T) {
		result, err := classificationClient(t).Classify(context.Background(), classifyInput())
		assert.NoError(t, err)
		assert.Equal(t, result.Verdicts[0].Verdict, domain.VerdictCovered)
		assert.Equal(t, result.Verdicts[0].RuleID, "AIR-GO-001")
	})

	testigo.Run(t, "maps a new-rule verdict", func(t *testing.T) {
		result, err := classificationClient(t).Classify(context.Background(), classifyInput())
		assert.NoError(t, err)
		assert.Equal(t, result.Verdicts[1].Verdict, domain.VerdictNew)
		assert.Equal(t, result.Verdicts[1].CandidateKey, "group-imports")
	})

	testigo.Run(t, "maps a one-off verdict", func(t *testing.T) {
		result, err := classificationClient(t).Classify(context.Background(), classifyInput())
		assert.NoError(t, err)
		assert.Equal(t, result.Verdicts[2].Verdict, domain.VerdictOneOff)
	})
}

func TestClassify_RejectsUnknownRuleID(t *testing.T) {
	testigo.Run(t, "rejects an unknown rule identity", func(t *testing.T) {
		_, client := newTestServer(t, "emit_classification", map[string]any{
			"verdicts": []map[string]any{
				{"comment_id": 1, "verdict": "covered", "rule_id": "AIR-GO-999"},
			},
		})

		_, err := client.Classify(context.Background(), anthropictest.ClassificationRequest(
			[]ports.CommentInput{anthropictest.GoComment(1, "")},
			[]ports.CatalogEntry{anthropictest.ActiveCatalogEntry("AIR-GO-001", "Format with gofmt")},
		))
		assert.ErrorContains(t, err, `rule_id "AIR-GO-999" does not exist in the catalog`)
	})
}

func TestClassify_RejectsMissingVerdictForComment(t *testing.T) {
	testigo.Run(t, "rejects a missing comment verdict", func(t *testing.T) {
		_, client := newTestServer(t, "emit_classification", map[string]any{
			"verdicts": []map[string]any{
				{"comment_id": 1, "verdict": "noise"},
			},
		})

		_, err := client.Classify(context.Background(), anthropictest.ClassificationRequest(
			[]ports.CommentInput{anthropictest.GoComment(1, ""), anthropictest.GoComment(2, "")}, nil,
		))
		assert.ErrorContains(t, err, "comment_id 2 received no verdict")
	})
}

func TestCompact_ValidResponse(t *testing.T) {
	testigo.Run(t, "returns the requested merge", func(t *testing.T) {
		_, client := newTestServer(t, "emit_compaction", map[string]any{
			"merges": []map[string]any{
				{"surviving_candidate_id": "a", "merged_candidate_ids": []string{"b"}},
			},
		})

		result, err := client.Compact(context.Background(), anthropictest.CompactionRequest([]ports.CandidateEntry{
			anthropictest.Candidate("a", "group imports"),
			anthropictest.Candidate("b", "sort imports"),
		}))
		assert.NoError(t, err)
		assert.Equal(t, result.Merges, []ports.CompactionMerge{anthropictest.CompactionMerge.WithSurvivingCandidateID("a").WithMergedCandidateIDs([]string{"b"}).Bare()})
	})
}

func TestCompact_RejectsUnknownCandidateID(t *testing.T) {
	testigo.Run(t, "rejects an unknown candidate identity", func(t *testing.T) {
		_, client := newTestServer(t, "emit_compaction", map[string]any{
			"merges": []map[string]any{
				{"surviving_candidate_id": "a", "merged_candidate_ids": []string{"does-not-exist"}},
			},
		})

		_, err := client.Compact(context.Background(), anthropictest.CompactionRequest([]ports.CandidateEntry{anthropictest.Candidate("a", "x")}))
		assert.Error(t, err)
	})
}

func TestDraftRule_ValidResponse(t *testing.T) {
	testigo.Run(t, "returns the drafted rule", func(t *testing.T) {
		_, client := newTestServer(t, "emit_rule_draft", map[string]any{
			"title": "Group imports",
			"scope": "**/*.go",
			"rule":  "Group imports by origin.",
		})

		draft, err := client.DraftRule(context.Background(), anthropictest.RuleDraftRequest([]ports.CommentInput{anthropictest.GoComment(1, "group imports")}))
		assert.NoError(t, err)
		want := anthropictest.RuleDraft.WithTitle("Group imports").WithScope("**/*.go").Bare()
		want.Rule = "Group imports by origin."
		want.Body = ""
		assert.Equal(t, draft, want)
	})
}

func TestCallTool_NonOKStatusReturnsError(t *testing.T) {
	testigo.Run(t, "reports an unauthorized response", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
		}))
		defer server.Close()

		client := New("bad-key", "test-model", "v1", WithBaseURL(server.URL))
		_, err := client.Classify(context.Background(), anthropictest.ClassificationRequest([]ports.CommentInput{anthropictest.GoComment(1, "")}, nil))
		assert.ErrorContains(t, err, "authentication_error (HTTP 401): invalid x-api-key")
	})
}

func TestCallTool_OmitsThinkingForOpus(t *testing.T) {
	testigo.Run(t, "uses the model default thinking mode", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req messageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			if req.Thinking != nil {
				t.Errorf("thinking: got %#v, want omitted", req.Thinking)
			}
			_ = json.NewEncoder(w).Encode(messageResponse{
				Model:      "claude-opus-5-5",
				Content:    []contentBlock{{Type: "tool_use", Name: "emit_classification", Input: json.RawMessage(`{"verdicts":[{"comment_id":1,"verdict":"one_off"}]}`)}},
				StopReason: "tool_use",
			})
		}))
		defer server.Close()

		client := New("test-key", "claude-opus-5-5", "v1", WithBaseURL(server.URL))
		_, err := client.Classify(context.Background(), anthropictest.ClassificationRequest([]ports.CommentInput{anthropictest.GoComment(1, "")}, nil))
		assert.NoError(t, err)
	})
}

func TestCallTool_UsesAdaptiveThinkingAndMaxEffortForHaiku(t *testing.T) {
	testigo.Run(t, "sends the Haiku 5.5 effort payload accepted by the Messages API", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req messageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			if req.Model != "claude-haiku-5-5" {
				t.Errorf("model: got %q", req.Model)
			}
			if req.Thinking == nil || req.Thinking.Type != "adaptive" {
				t.Errorf("thinking: got %#v, want adaptive", req.Thinking)
			}
			if req.OutputConfig == nil || req.OutputConfig.Effort != "max" {
				t.Errorf("output_config: got %#v, want max effort", req.OutputConfig)
			}
			if req.ToolChoice.Type != "auto" {
				t.Errorf("tool_choice: got %#v, want auto", req.ToolChoice)
			}
			_ = json.NewEncoder(w).Encode(messageResponse{
				Model:      "claude-haiku-5-5",
				Content:    []contentBlock{{Type: "tool_use", Name: "emit_classification", Input: json.RawMessage(`{"verdicts":[{"comment_id":1,"verdict":"one_off"}]}`)}},
				StopReason: "tool_use",
			})
		}))
		defer server.Close()

		client := New("test-key", "claude-haiku-5-5", "v1", WithBaseURL(server.URL), WithEffort("max"), WithMaxTokens(128000))
		_, err := client.Classify(context.Background(), anthropictest.ClassificationRequest([]ports.CommentInput{anthropictest.GoComment(1, "")}, nil))
		assert.NoError(t, err)
	})
}

func TestCallTool_UsesConfiguredMaxTokens(t *testing.T) {
	testigo.Run(t, "sends the review output limit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req messageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			if req.MaxTokens != 128000 {
				t.Errorf("max_tokens: got %d, want 128000", req.MaxTokens)
			}
			_ = json.NewEncoder(w).Encode(messageResponse{
				Model:      "claude-opus-5-5",
				Content:    []contentBlock{{Type: "tool_use", Name: "emit_classification", Input: json.RawMessage(`{"verdicts":[{"comment_id":1,"verdict":"one_off"}]}`)}},
				StopReason: "tool_use",
			})
		}))
		defer server.Close()

		client := New("test-key", "claude-opus-5-5", "v1", WithBaseURL(server.URL), WithMaxTokens(128000))
		_, err := client.Classify(context.Background(), anthropictest.ClassificationRequest([]ports.CommentInput{anthropictest.GoComment(1, "")}, nil))
		assert.NoError(t, err)
	})
}

func TestNew_MissingAPIKey(t *testing.T) {
	testigo.Run(t, "requires an API key", func(t *testing.T) {
		client := New("", "test-model", "v1")
		_, err := client.Classify(context.Background(), ports.ClassificationInput{})
		assert.Error(t, err)
	})
}
