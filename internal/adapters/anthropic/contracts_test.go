package anthropic

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/anthropic/anthropictest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func commentInput(id int64) ports.CommentInput {
	return anthropictest.CommentInput.WithCommentID(id).WithLanguage("go").WithBody("group imports").Bare()
}

func clientReturningToolInput(t *testing.T, toolName, input string) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("content-type", "application/json")
		_, _ = io.WriteString(w, `{"model":"test-model","content":[{"type":"tool_use","name":"`+toolName+`","input":`+input+`}],"stop_reason":"tool_use"}`)
	}))
	t.Cleanup(server.Close)
	return New("test-key", "test-model", "v1", WithBaseURL(server.URL))
}

func TestClassificationResponseContract(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		payload  string
		comments []ports.CommentInput
		catalog  []ports.CatalogEntry
		want     string
	}{
		{"rejects malformed tool input", `"invalid"`, []ports.CommentInput{commentInput(1)}, nil, "invalid tool response"},
		{"rejects an unknown comment", `{"verdicts":[{"comment_id":2,"verdict":"noise"}]}`, []ports.CommentInput{commentInput(1)}, nil, "was not in the input"},
		{"rejects duplicate verdicts", `{"verdicts":[{"comment_id":1,"verdict":"noise"},{"comment_id":1,"verdict":"noise"}]}`, []ports.CommentInput{commentInput(1)}, nil, "more than one verdict"},
		{"rejects an unknown verdict", `{"verdicts":[{"comment_id":1,"verdict":"maybe"}]}`, []ports.CommentInput{commentInput(1)}, nil, "unknown verdict"},
		{"requires a rule for covered comments", `{"verdicts":[{"comment_id":1,"verdict":"covered"}]}`, []ports.CommentInput{commentInput(1)}, nil, "has no rule_id"},
		{"requires a candidate key for new comments", `{"verdicts":[{"comment_id":1,"verdict":"new"}]}`, []ports.CommentInput{commentInput(1)}, nil, "has no candidate_key"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			client := clientReturningToolInput(t, "emit_classification", scenario.payload)
			_, err := client.Classify(context.Background(), ports.ClassificationInput{Comments: scenario.comments, Catalog: scenario.catalog})
			assert.ErrorContains(t, err, scenario.want)
		})
	}

	testigo.Run(t, "reports an unknown prompt version", func(t *testing.T) {
		_, err := New("key", "model", "missing").Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "no template")
	})
}

func TestCompactionResponseContract(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		payload    string
		candidates []ports.CandidateEntry
		want       string
	}{
		{"rejects malformed tool input", `"invalid"`, nil, "invalid tool response"},
		{"rejects an unknown survivor", `{"merges":[{"surviving_candidate_id":"missing","merged_candidate_ids":[]}]}`, []ports.CandidateEntry{anthropictest.CandidateEntry.WithCandidateID("one").WithLabel("candidate").Bare()}, "surviving_candidate_id"},
		{"rejects a candidate merged into itself", `{"merges":[{"surviving_candidate_id":"one","merged_candidate_ids":["one"]}]}`, []ports.CandidateEntry{anthropictest.CandidateEntry.WithCandidateID("one").WithLabel("candidate").Bare()}, "cannot be merged into itself"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			client := clientReturningToolInput(t, "emit_compaction", scenario.payload)
			_, err := client.Compact(context.Background(), ports.CompactionInput{Candidates: scenario.candidates})
			assert.ErrorContains(t, err, scenario.want)
		})
	}

	testigo.Run(t, "reports an unknown prompt version", func(t *testing.T) {
		_, err := New("key", "model", "missing").Compact(context.Background(), ports.CompactionInput{})
		assert.ErrorContains(t, err, "no template")
	})

	testigo.Run(t, "reports an unavailable Messages API", func(t *testing.T) {
		_, err := New("", "model", "v1").Compact(context.Background(), ports.CompactionInput{})
		assert.ErrorContains(t, err, "API key is missing")
	})
}

func TestRuleDraftResponseContract(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		payload string
		want    string
	}{
		{"rejects malformed tool input", `"invalid"`, "invalid tool response"},
		{"requires a title", `{"rule":"rule"}`, "does not include title/rule"},
		{"requires a rule", `{"title":"Rule"}`, "does not include title/rule"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			client := clientReturningToolInput(t, "emit_rule_draft", scenario.payload)
			_, err := client.DraftRule(context.Background(), ports.DraftRuleInput{})
			assert.ErrorContains(t, err, scenario.want)
		})
	}

	testigo.Run(t, "reports an unknown prompt version", func(t *testing.T) {
		_, err := New("key", "model", "missing").DraftRule(context.Background(), ports.DraftRuleInput{})
		assert.ErrorContains(t, err, "no template")
	})

	testigo.Run(t, "reports an unavailable Messages API", func(t *testing.T) {
		_, err := New("", "model", "v1").DraftRule(context.Background(), ports.DraftRuleInput{})
		assert.ErrorContains(t, err, "API key is missing")
	})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("read unavailable") }
func (failingBody) Close() error             { return nil }

func TestMessagesAPIResponseContract(t *testing.T) {
	testigo.Run(t, "requires a configured model", func(t *testing.T) {
		_, err := New("key", "", "v1").Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "model is missing")
	})

	testigo.Run(t, "reports an invalid endpoint", func(t *testing.T) {
		client := New("key", "model", "v1", WithBaseURL(":"))
		_, err := client.Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "build request")
	})

	testigo.Run(t, "reports a transport failure", func(t *testing.T) {
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("network unavailable") })
		client := New("key", "model", "v1", WithHTTPClient(&http.Client{Transport: transport}))
		_, err := client.Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "Messages API failed")
	})

	testigo.Run(t, "reports an unreadable response", func(t *testing.T) {
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: failingBody{}, Header: make(http.Header)}, nil
		})
		client := New("key", "model", "v1", WithHTTPClient(&http.Client{Transport: transport}))
		_, err := client.Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "read response")
	})

	testigo.Run(t, "reports an unstructured HTTP failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "upstream unavailable")
		}))
		defer server.Close()
		_, err := New("key", "model", "v1", WithBaseURL(server.URL)).Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "HTTP 502")
	})

	testigo.Run(t, "reports malformed JSON", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, "{") }))
		defer server.Close()
		_, err := New("key", "model", "v1", WithBaseURL(server.URL)).Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "invalid response")
	})

	testigo.Run(t, "requires the selected tool invocation", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"content":[{"type":"text","name":"other"}],"stop_reason":"end_turn"}`)
		}))
		defer server.Close()
		_, err := New("key", "model", "v1", WithBaseURL(server.URL)).Classify(context.Background(), ports.ClassificationInput{})
		assert.ErrorContains(t, err, "did not invoke tool")
	})

	testigo.Run(t, "sends the configured Anthropic version", func(t *testing.T) {
		var received string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
			received = request.Header.Get("anthropic-version")
			_, _ = io.WriteString(w, `{"content":[{"type":"tool_use","name":"emit_classification","input":{"verdicts":[]}}]}`)
		}))
		defer server.Close()
		client := New("key", "model", "v1", WithBaseURL(server.URL), WithAnthropicVersion("current"), WithHTTPClient(server.Client()))
		_, err := client.Classify(context.Background(), ports.ClassificationInput{})
		assert.NoError(t, err)
		assert.Equal(t, received, "current")
	})
}

func TestResponseHelpers(t *testing.T) {
	testigo.Run(t, "preserves text readers", func(t *testing.T) {
		content, err := io.ReadAll(strings.NewReader("response"))
		assert.NoError(t, err)
		assert.Equal(t, string(content), "response")
	})
}
