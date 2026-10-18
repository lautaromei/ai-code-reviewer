package ghcli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/ghcli/ghclitest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

var validPublicationRequest = ghclitest.PublicationRequest.
	WithRepository("owner/repo").
	WithBranch("automation/rules").
	WithDefaultBranch("main").
	WithFilePath("docs/rules.md").
	WithContent([]byte("# Rules")).
	WithCommitMessage("update rules").
	WithPRTitle("Update rules").
	Bare()

func TestRepositoryMetadataFailures(t *testing.T) {
	for _, scenario := range []struct {
		name string
		call func(*GitHub) error
	}{
		{"languages require a repository", func(g *GitHub) error { _, err := g.RepositoryLanguages(context.Background(), ""); return err }},
		{"languages report GitHub failure", func(g *GitHub) error { _, err := g.RepositoryLanguages(context.Background(), "owner/repo"); return err }},
		{"languages reject malformed JSON", func(_ *GitHub) error {
			g := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte("{")}))
			_, err := g.RepositoryLanguages(context.Background(), "owner/repo")
			return err
		}},
		{"default branch requires a repository", func(g *GitHub) error { _, err := g.DefaultBranch(context.Background(), ""); return err }},
		{"default branch reports GitHub failure", func(g *GitHub) error { _, err := g.DefaultBranch(context.Background(), "owner/repo"); return err }},
		{"default branch rejects malformed JSON", func(_ *GitHub) error {
			g := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte("{")}))
			_, err := g.DefaultBranch(context.Background(), "owner/repo")
			return err
		}},
		{"default branch requires a branch name", func(_ *GitHub) error {
			g := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`{}`)}))
			_, err := g.DefaultBranch(context.Background(), "owner/repo")
			return err
		}},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			github := newTestGitHub(newScriptedRunner(t, scriptedCall{exitCode: 1, stderr: []byte("failure")}))
			assert.Error(t, scenario.call(github))
		})
	}
	assertRemoteFileConversation(t)
}

func assertRemoteFileConversation(t *testing.T) {
	t.Helper()
	github := newTestGitHub(newScriptedRunner(t,
		scriptedCall{stdout: []byte(`{"content":"cmVtb3RlIGNvbnRlbnQ=","encoding":"base64"}`)},
		scriptedCall{exitCode: 1, stderr: []byte("Not Found (HTTP 404)"), stdout: []byte(`{"message":"Not Found"}`)},
		scriptedCall{stdout: []byte(`{`)},
		scriptedCall{stdout: []byte(`{"content":"remote content","encoding":"utf-8"}`)},
		scriptedCall{stdout: []byte(`{"content":"%%%","encoding":"base64"}`)},
	))

	_, err := github.ReadFile(context.Background(), "", "docs/rules.md", "main")
	assert.ErrorContains(t, err, "repository cannot be empty")
	_, err = github.ReadFile(context.Background(), "owner/repo", "", "main")
	assert.ErrorContains(t, err, "path cannot be empty")
	_, err = github.ReadFile(context.Background(), "owner/repo", "docs/rules.md", "")
	assert.ErrorContains(t, err, "branch cannot be empty")

	content, err := github.ReadFile(context.Background(), "owner/repo", "docs/rules.md", "main")
	assert.NoError(t, err)
	assert.Equal(t, string(content), "remote content")

	_, err = github.ReadFile(context.Background(), "owner/repo", "docs/rules.md", "main")
	assert.Error(t, err)

	_, err = github.ReadFile(context.Background(), "owner/repo", "docs/rules.md", "main")
	assert.ErrorContains(t, err, "invalid response")

	_, err = github.ReadFile(context.Background(), "owner/repo", "docs/rules.md", "main")
	assert.ErrorContains(t, err, "unsupported encoding")

	_, err = github.ReadFile(context.Background(), "owner/repo", "docs/rules.md", "main")
	assert.ErrorContains(t, err, "invalid")
}

func TestReactionResponseFailures(t *testing.T) {
	testigo.Run(t, "requires a repository", func(t *testing.T) {
		_, err := newTestGitHub(nil).ListReactions(context.Background(), "", 1, "")
		assert.Error(t, err)
	})

	testigo.Run(t, "reports GitHub failure", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{exitCode: 1, stderr: []byte("failure")}))
		_, err := github.ListReactions(context.Background(), "owner/repo", 1, "")
		assert.Error(t, err)
	})

	testigo.Run(t, "rejects malformed JSON", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte("{")}))
		_, err := github.ListReactions(context.Background(), "owner/repo", 1, "")
		assert.ErrorContains(t, err, "invalid response")
	})
}

func TestReviewCommentResponseFailures(t *testing.T) {
	testigo.Run(t, "requires a repository", func(t *testing.T) {
		_, _, err := newTestGitHub(nil).ListReviewComments(context.Background(), "", ports.Cursor{})
		assert.Error(t, err)
	})

	testigo.Run(t, "reports a paginated request failure", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{exitCode: 1, stderr: []byte("failure")}))
		_, _, err := github.ListReviewComments(context.Background(), "owner/repo", ports.Cursor{})
		assert.ErrorContains(t, err, "fetch page")
	})

	testigo.Run(t, "rejects a non-array page", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`{}`)}))
		_, _, err := github.ListReviewComments(context.Background(), "owner/repo", ports.Cursor{})
		assert.ErrorContains(t, err, "expected a JSON array")
	})

	testigo.Run(t, "rejects malformed comment JSON", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`[{"id":"wrong"}]`)}))
		_, _, err := github.ListReviewComments(context.Background(), "owner/repo", ports.Cursor{})
		assert.ErrorContains(t, err, "invalid comment")
	})

	testigo.Run(t, "rejects a malformed pull request URL", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`[{"id":1,"pull_request_url":"missing"}]`)}))
		_, _, err := github.ListReviewComments(context.Background(), "owner/repo", ports.Cursor{})
		assert.ErrorContains(t, err, "unexpected format")
	})

	testigo.Run(t, "rejects a non-numeric pull request", func(t *testing.T) {
		_, err := parsePullRequestNumber("https://api.github.com/pulls/not-a-number")
		assert.ErrorContains(t, err, "invalid PR number")
	})

	testigo.Run(t, "accepts an empty response stream", func(t *testing.T) {
		comments, err := decodeJSONArrayStream(nil)
		assert.NoError(t, err)
		assert.Empty(t, comments)
	})

	testigo.Run(t, "rejects malformed streamed JSON", func(t *testing.T) {
		_, err := decodeJSONArrayStream([]byte("["))
		assert.ErrorContains(t, err, "invalid top-level JSON")
	})
}

func TestGitHubCommandContract(t *testing.T) {
	testigo.Run(t, "normalizes negative retries", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{exitCode: 1, stderr: []byte("rate limit (HTTP 429)")}))
		github.maxRetries = -1
		_, err := github.DefaultBranch(context.Background(), "owner/repo")
		assert.ErrorIs(t, err, ErrRateLimited)
	})

	testigo.Run(t, "stops retrying when context is cancelled", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{exitCode: 1, stderr: []byte("rate limit (HTTP 429)")}))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := github.DefaultBranch(ctx, "owner/repo")
		assert.ErrorIs(t, err, context.Canceled)
	})

	testigo.Run(t, "falls back to stdout error detail", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{exitCode: 1, stdout: []byte("failure detail")}))
		_, err := github.DefaultBranch(context.Background(), "owner/repo")
		assert.ErrorContains(t, err, "failure detail")
	})

	testigo.Run(t, "caps exponential backoff", func(t *testing.T) {
		assert.Equal(t, backoffDuration(20), 30*time.Second)
	})

	testigo.Run(t, "truncates long command output", func(t *testing.T) {
		detail := truncateCommandDetail([]byte(strings.Repeat("x", maxCommandErrorDetail+1)))
		assert.Contains(t, detail, "output truncated")
	})

	testigo.Run(t, "adds pagination to an existing query", func(t *testing.T) {
		endpoint, err := endpointWithPage("repos/o/r?state=open", 2)
		assert.NoError(t, err)
		assert.Contains(t, endpoint, "page=2")
	})

	testigo.Run(t, "rejects an invalid pagination endpoint", func(t *testing.T) {
		_, err := endpointWithPage("%", 1)
		assert.Error(t, err)
	})

	testigo.Run(t, "rejects a non-array pagination response", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`{}`)}))
		_, err := github.apiGetPaginated(context.Background(), "repos/o/r")
		assert.ErrorContains(t, err, "expected a JSON array")
	})

	testigo.Run(t, "rejects an invalid paginated endpoint", func(t *testing.T) {
		_, err := newTestGitHub(nil).apiGetPaginated(context.Background(), "%")
		assert.ErrorContains(t, err, "add page")
	})

	testigo.Run(t, "uses the default page size when disabled", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`[]`)}))
		github.pageSize = 0
		page, err := github.apiGetPaginated(context.Background(), "repos/o/r")
		assert.NoError(t, err)
		assert.Equal(t, string(page), "[]")
	})
}

func TestDefaultCommandRunner(t *testing.T) {
	testigo.Run(t, "returns command output", func(t *testing.T) {
		stdout, _, code, err := defaultCommandRunner(context.Background(), "sh", "-c", "printf success")
		assert.NoError(t, err)
		assert.Equal(t, code, 0)
		assert.Equal(t, string(stdout), "success")
	})

	testigo.Run(t, "returns process exit code", func(t *testing.T) {
		_, _, code, err := defaultCommandRunner(context.Background(), "sh", "-c", "exit 7")
		assert.NoError(t, err)
		assert.Equal(t, code, 7)
	})

	testigo.Run(t, "reports an unavailable executable", func(t *testing.T) {
		_, _, code, err := defaultCommandRunner(context.Background(), "definitely-not-a-command")
		assert.Error(t, err)
		assert.Equal(t, code, -1)
	})
}

func TestMutatingRequestSerialization(t *testing.T) {
	testigo.Run(t, "rejects values that JSON cannot represent", func(t *testing.T) {
		github := newTestGitHub(nil)
		_, err := github.apiMutate(context.Background(), "POST", "repos/o/r", map[string]any{"bad": make(chan int)})
		assert.ErrorContains(t, err, "serialize body")
	})

	testigo.Run(t, "reports unavailable temporary request storage", func(t *testing.T) {
		github := newTestGitHub(nil)
		github.requestBody = func() (temporaryRequestBody, error) { return nil, errors.New("storage unavailable") }
		_, err := github.apiMutate(context.Background(), "POST", "repos/o/r", map[string]string{})
		assert.ErrorContains(t, err, "create temporary file")
	})

	testigo.Run(t, "reports a request body close failure", func(t *testing.T) {
		github := newTestGitHub(nil)
		github.requestBody = func() (temporaryRequestBody, error) {
			return &requestBodyFault{closeErr: errors.New("close unavailable")}, nil
		}
		_, err := github.apiMutate(context.Background(), "POST", "repos/o/r", map[string]string{})
		assert.ErrorContains(t, err, "close temporary file")
	})
}

type requestBodyFault struct {
	strings.Builder
	closeErr error
}

func (*requestBodyFault) Name() string   { return "temporary-request" }
func (f *requestBodyFault) Close() error { return f.closeErr }

func TestErrorClassificationHelpers(t *testing.T) {
	testigo.Run(t, "ignores text without rate limit markers", func(t *testing.T) {
		assert.False(t, looksLikeRateLimit("permission denied"), "message should not be a rate limit")
	})

	testigo.Run(t, "rejects a non-array stream document", func(t *testing.T) {
		_, err := decodeJSONArrayStream([]byte(`{}`))
		assert.ErrorContains(t, err, "expected a JSON array per page")
	})

	testigo.Run(t, "reports no explicit retry delay", func(t *testing.T) {
		assert.Equal(t, parseRetryAfter("rate limit"), noExplicitRetryAfter)
	})

	testigo.Run(t, "classifies not modified", func(t *testing.T) {
		assert.Equal(t, classifyError(1, nil, []byte("(HTTP 304)")).kind, errKindNotModified)
	})
}

func TestPublicationRequestValidation(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*ports.PublicationRequest)
		want   string
	}{
		{"requires a repository", func(request *ports.PublicationRequest) { request.Repository = "" }, "repository"},
		{"requires a branch", func(request *ports.PublicationRequest) { request.Branch = "" }, "branch"},
		{"requires a default branch", func(request *ports.PublicationRequest) { request.DefaultBranch = "" }, "defaultBranch"},
		{"requires a file path", func(request *ports.PublicationRequest) { request.FilePath = "" }, "filePath"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			request := validPublicationRequest
			scenario.change(&request)
			_, err := newTestGitHub(nil).UpsertRulesPullRequest(context.Background(), request)
			assert.ErrorContains(t, err, scenario.want)
		})
	}
}

func TestPublicationPipelineFailures(t *testing.T) {
	notFound := scriptedCall{exitCode: 1, stderr: []byte("Not Found (HTTP 404)")}
	failure := scriptedCall{exitCode: 1, stderr: []byte("failure")}
	branch := scriptedCall{stdout: []byte(`{"object":{"sha":"branch-sha"}}`)}
	base := scriptedCall{stdout: []byte(`{"object":{"sha":"base-sha"}}`)}
	createdRef := scriptedCall{stdout: []byte(`{"ref":"refs/heads/automation/rules"}`)}
	commit := scriptedCall{stdout: []byte(`{"commit":{"sha":"commit-sha"}}`)}
	noPullRequest := scriptedCall{stdout: []byte(`[]`)}

	for _, scenario := range []struct {
		name  string
		calls []scriptedCall
		want  string
	}{
		{"reports branch lookup failure", []scriptedCall{failure}, "look up branch"},
		{"reports default branch lookup failure", []scriptedCall{notFound, failure}, "default branch"},
		{"requires the default branch to exist", []scriptedCall{notFound, notFound}, "does not exist"},
		{"reports branch creation failure", []scriptedCall{notFound, base, failure}, "create branch"},
		{"reports file lookup failure", []scriptedCall{branch, failure}, "look up file"},
		{"reports file update failure", []scriptedCall{branch, notFound, failure}, "write file"},
		{"reports pull request lookup failure", []scriptedCall{branch, notFound, commit, failure}, "find open pull request"},
		{"reports pull request creation failure", []scriptedCall{branch, notFound, commit, noPullRequest, failure}, "create pull request"},
		{"reports new branch API failure", []scriptedCall{notFound, base, createdRef, notFound, failure}, "write file"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			github := newTestGitHub(newScriptedRunner(t, scenario.calls...))
			_, err := github.UpsertRulesPullRequest(context.Background(), validPublicationRequest)
			assert.ErrorContains(t, err, scenario.want)
		})
	}
}

func TestPublicationResponseValidation(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		call     func(*GitHub) error
		response scriptedCall
		want     string
	}{
		{"rejects malformed branch JSON", func(g *GitHub) error { _, _, err := g.refSHA(context.Background(), "owner/repo", "main"); return err }, scriptedCall{stdout: []byte("{")}, "invalid response"},
		{"requires branch SHA", func(g *GitHub) error { _, _, err := g.refSHA(context.Background(), "owner/repo", "main"); return err }, scriptedCall{stdout: []byte(`{}`)}, "does not include sha"},
		{"rejects malformed file JSON", func(g *GitHub) error {
			_, err := g.fileSHA(context.Background(), "owner/repo", "rules.md", "main")
			return err
		}, scriptedCall{stdout: []byte("{")}, "invalid response"},
		{"rejects malformed file update JSON", func(g *GitHub) error {
			_, err := g.putContents(context.Background(), validPublicationRequest, "")
			return err
		}, scriptedCall{stdout: []byte("{")}, "invalid response"},
		{"rejects malformed pull request list", func(g *GitHub) error {
			_, _, err := g.findOpenPullRequest(context.Background(), "owner/repo", "branch")
			return err
		}, scriptedCall{stdout: []byte("{")}, "invalid response"},
		{"rejects malformed pull request creation", func(g *GitHub) error {
			_, err := g.createPullRequest(context.Background(), validPublicationRequest)
			return err
		}, scriptedCall{stdout: []byte("{")}, "invalid response"},
		{"requires pull request URL", func(g *GitHub) error {
			_, err := g.createPullRequest(context.Background(), validPublicationRequest)
			return err
		}, scriptedCall{stdout: []byte(`{}`)}, "does not include html_url"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			github := newTestGitHub(newScriptedRunner(t, scenario.response))
			assert.ErrorContains(t, scenario.call(github), scenario.want)
		})
	}

	testigo.Run(t, "finds no pull request for an owner-only repository", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`[]`)}))
		_, found, err := github.findOpenPullRequest(context.Background(), "owner", "branch")
		assert.NoError(t, err)
		assert.False(t, found, "pull request should be absent")
	})
}
