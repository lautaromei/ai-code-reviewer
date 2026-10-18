package ghcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/ghcli/ghclitest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("..", "..", "..", "testdata", "github", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read test fixture %s: %v", name, err)
	}
	return data
}

type scriptedCall struct {
	stdout   []byte
	stderr   []byte
	exitCode int
	err      error
}

func newScriptedRunner(t *testing.T, calls ...scriptedCall) commandRunner {
	t.Helper()
	next := 0
	return func(context.Context, string, ...string) ([]byte, []byte, int, error) {
		if next >= len(calls) {
			t.Fatalf("unexpected command invocation %d", next+1)
		}
		call := calls[next]
		next++
		return call.stdout, call.stderr, call.exitCode, call.err
	}
}

func newTestGitHub(run commandRunner) *GitHub {
	github := New("github.com", "2022-11-28", 2*time.Second, 3)
	github.pageSize = 2
	github.run = run
	return github
}

func reviewComments(t *testing.T) ([]ports.ReviewComment, ports.Cursor, error) {
	t.Helper()
	github := newTestGitHub(newScriptedRunner(t,
		scriptedCall{stdout: readFixture(t, "review_comments_page1.json")},
		scriptedCall{stdout: readFixture(t, "review_comments_page2.json")},
	))
	return github.ListReviewComments(context.Background(), "octocat/Hello-World", ports.Cursor{})
}

func TestReviewCommentConversation(t *testing.T) {
	testigo.Run(t, "returns every paginated comment", func(t *testing.T) {
		comments, _, err := reviewComments(t)
		assert.NoError(t, err)
		assert.Len(t, comments, 3)
	})

	testigo.Run(t, "maps the first comment identity", func(t *testing.T) {
		comments, _, err := reviewComments(t)
		assert.NoError(t, err)
		assert.Equal(t, comments[0].GitHubCommentID, int64(342751))
		assert.Equal(t, comments[0].PullRequestNumber, int64(8))
	})

	testigo.Run(t, "maps a human author", func(t *testing.T) {
		comments, _, err := reviewComments(t)
		assert.NoError(t, err)
		assert.Equal(t, comments[0].Author, "rohitsingh")
		assert.False(t, comments[0].AuthorIsBot, "author should be human")
	})

	testigo.Run(t, "maps a review reply", func(t *testing.T) {
		comments, _, err := reviewComments(t)
		assert.NoError(t, err)
		assert.Equal(t, comments[1].ReviewID, int64(900001))
		assert.Equal(t, *comments[1].InReplyToID, int64(342751))
	})

	testigo.Run(t, "advances to the newest position", func(t *testing.T) {
		_, cursor, err := reviewComments(t)
		assert.NoError(t, err)
		assert.Equal(t, cursor, ports.Cursor{
			Since:         time.Date(2012, 1, 13, 0, 0, 0, 0, time.UTC),
			LastCommentID: 342753,
		})
	})

	testigo.Run(t, "an empty page keeps the previous cursor", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte("[]")}))
		previous := ghclitest.Cursor.WithSince(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)).WithLastCommentID(42).Bare()
		comments, cursor, err := github.ListReviewComments(context.Background(), "octocat/Hello-World", previous)
		assert.NoError(t, err)
		assert.Empty(t, comments)
		assert.Equal(t, cursor, previous)
	})
}

func TestReviewCommentsEndpoint(t *testing.T) {
	testigo.Run(t, "omits an absent date", func(t *testing.T) {
		endpoint, err := reviewCommentsEndpoint("o/r", ports.Cursor{})
		assert.NoError(t, err)
		assert.NotContains(t, endpoint, "since=")
	})

	testigo.Run(t, "orders comments by update date", func(t *testing.T) {
		endpoint, err := reviewCommentsEndpoint("o/r", ports.Cursor{})
		assert.NoError(t, err)
		assert.Contains(t, endpoint, "sort=updated")
		assert.Contains(t, endpoint, "direction=asc")
	})

	testigo.Run(t, "includes a known date", func(t *testing.T) {
		cursor := ghclitest.Cursor.WithSince(time.Date(2024, 3, 15, 10, 0, 0, 0, time.UTC)).Bare()
		endpoint, err := reviewCommentsEndpoint("o/r", cursor)
		assert.NoError(t, err)
		assert.Contains(t, endpoint, "since=2024-03-15")
	})
}

func TestAdvanceCursor(t *testing.T) {
	base := ghclitest.Cursor.WithSince(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)).WithLastCommentID(10).Bare()

	testigo.Run(t, "newer comment by date wins", func(t *testing.T) {
		comments := []ports.ReviewComment{ghclitest.ReviewComment.WithGitHubCommentID(1).WithUpdatedAt(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)).Bare()}
		assert.Equal(t, advanceCursor(base, comments), ghclitest.Cursor.WithSince(comments[0].UpdatedAt).WithLastCommentID(1).Bare())
	})

	testigo.Run(t, "higher identity wins on the same date", func(t *testing.T) {
		comments := []ports.ReviewComment{ghclitest.ReviewComment.WithGitHubCommentID(5).WithUpdatedAt(base.Since).Bare(), ghclitest.ReviewComment.WithGitHubCommentID(20).WithUpdatedAt(base.Since).Bare()}
		assert.Equal(t, advanceCursor(base, comments).LastCommentID, int64(20))
	})

	testigo.Run(t, "older comment does not move backward", func(t *testing.T) {
		comments := []ports.ReviewComment{ghclitest.ReviewComment.WithGitHubCommentID(999).WithUpdatedAt(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)).Bare()}
		assert.Equal(t, advanceCursor(base, comments), base)
	})

	testigo.Run(t, "no comments leaves the cursor unchanged", func(t *testing.T) {
		assert.Equal(t, advanceCursor(base, nil), base)
	})
}

func TestRepositoryMetadataConversation(t *testing.T) {
	testigo.Run(t, "returns repository languages", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: readFixture(t, "languages.json")}))
		languages, err := github.RepositoryLanguages(context.Background(), "octocat/Hello-World")
		assert.NoError(t, err)
		assert.Equal(t, languages, []ports.Language{ghclitest.Language.WithName("Go").WithBytes(123456).Bare(), ghclitest.Language.WithName("PHP").WithBytes(4242).Bare(), ghclitest.Language.WithName("Shell").WithBytes(789).Bare()})
	})

	testigo.Run(t, "returns the default branch", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: readFixture(t, "repository.json")}))
		branch, err := github.DefaultBranch(context.Background(), "octocat/Hello-World")
		assert.NoError(t, err)
		assert.Equal(t, branch, "master")
	})
}

func TestGitHubErrorResponses(t *testing.T) {
	testigo.Run(t, "classifies not found", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`{"message":"Not Found"}`), stderr: []byte("gh: Not Found (HTTP 404)"), exitCode: 1}))
		_, err := github.DefaultBranch(context.Background(), "octocat/missing")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	testigo.Run(t, "classifies permission denied", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`{"message":"Must have admin rights"}`), stderr: []byte("gh: Must have admin rights (HTTP 403)"), exitCode: 1}))
		_, err := github.DefaultBranch(context.Background(), "octocat/private")
		assert.ErrorIs(t, err, ErrPermission)
		assert.NotErrorIs(t, err, ErrRateLimited)
	})

	testigo.Run(t, "retries a rate limit response", func(t *testing.T) {
		rateLimit := scriptedCall{stdout: []byte(`{"message":"API rate limit exceeded"}`), stderr: []byte("gh: API rate limit exceeded (HTTP 403)"), exitCode: 1}
		github := newTestGitHub(newScriptedRunner(t, rateLimit, rateLimit, scriptedCall{stdout: readFixture(t, "repository.json")}))
		branch, err := github.DefaultBranch(context.Background(), "octocat/Hello-World")
		assert.NoError(t, err)
		assert.Equal(t, branch, "master")
	})

	testigo.Run(t, "reports exhausted rate limit retries", func(t *testing.T) {
		rateLimit := scriptedCall{stdout: []byte(`{"message":"API rate limit exceeded"}`), stderr: []byte("gh: API rate limit exceeded (HTTP 403)"), exitCode: 1}
		github := newTestGitHub(newScriptedRunner(t, rateLimit, rateLimit, rateLimit, rateLimit))
		_, err := github.DefaultBranch(context.Background(), "octocat/Hello-World")
		assert.ErrorIs(t, err, ErrRateLimited)
	})

	testigo.Run(t, "honors a zero retry-after delay", func(t *testing.T) {
		rateLimit := scriptedCall{stdout: []byte(`{"message":"secondary rate limit, retry after 0 seconds"}`), stderr: []byte("gh: secondary rate limit, retry-after: 0 (HTTP 429)"), exitCode: 1}
		github := newTestGitHub(newScriptedRunner(t, rateLimit, scriptedCall{stdout: readFixture(t, "repository.json")}))
		started := time.Now()
		_, err := github.DefaultBranch(context.Background(), "octocat/Hello-World")
		assert.NoError(t, err)
		assert.LessOrEqual(t, time.Since(started), 400*time.Millisecond)
	})

	testigo.Run(t, "classifies server failure as recoverable", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`{"message":"Internal Server Error"}`), stderr: []byte("HTTP 500"), exitCode: 1}))
		_, err := github.DefaultBranch(context.Background(), "octocat/Hello-World")
		assert.ErrorIs(t, err, ErrRecoverable)
	})

	testigo.Run(t, "preserves infrastructure failure", func(t *testing.T) {
		failure := errors.New("gh executable unavailable")
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{err: failure}))
		_, err := github.DefaultBranch(context.Background(), "octocat/Hello-World")
		assert.ErrorIs(t, err, failure)
	})
}

func TestReactionConversation(t *testing.T) {
	testigo.Run(t, "returns a reaction page", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: readFixture(t, "reactions.json")}))
		page, err := github.ListReactions(context.Background(), "octocat/Hello-World", 342751, "")
		assert.NoError(t, err)
		assert.Equal(t, page, ghclitest.ReactionPage.WithETag("").WithNotModified(false).WithReactions([]ports.ReactionRecord{
			ghclitest.ReactionRecord.WithGitHubReactionID(100).WithUser("alice").WithContent("+1").WithCreatedAt(time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)).Bare(),
			ghclitest.ReactionRecord.WithGitHubReactionID(101).WithUser("bob").WithContent("-1").WithCreatedAt(time.Date(2024, 3, 2, 11, 0, 0, 0, time.UTC)).Bare(),
		}).Bare())
	})

	testigo.Run(t, "preserves a known ETag when unmodified", func(t *testing.T) {
		github := newTestGitHub(newScriptedRunner(t, scriptedCall{stdout: []byte(`{"message":"Not Modified"}`), stderr: []byte("gh: Not Modified (HTTP 304)"), exitCode: 1}))
		page, err := github.ListReactions(context.Background(), "octocat/Hello-World", 342751, `"cached"`)
		assert.NoError(t, err)
		assert.Equal(t, page, ghclitest.ReactionPage.WithETag(`"cached"`).WithNotModified(true).Bare())
	})
}

func publicationRequest() ports.PublicationRequest {
	return ghclitest.PublicationRequest.WithRepository("octocat/Hello-World").WithBranch("automation/ai-review-rules").WithDefaultBranch("main").WithFilePath("docs/AI_REVIEW_RULES.md").WithContent([]byte("# rules")).WithCommitMessage("chore: update rules").WithPRTitle("chore: update AI review rules").WithPRBodyMarker("<!-- managed -->").Bare()
}

func createPublication(t *testing.T) (ports.PublicationResult, error) {
	t.Helper()
	github := newTestGitHub(newScriptedRunner(t,
		scriptedCall{stdout: []byte(`{"message":"Not Found"}`), stderr: []byte("gh: Not Found (HTTP 404)"), exitCode: 1},
		scriptedCall{stdout: []byte(`{"object":{"sha":"default-sha"}}`)},
		scriptedCall{stdout: []byte(`{"ref":"refs/heads/automation/ai-review-rules"}`)},
		scriptedCall{stdout: []byte(`{"message":"Not Found"}`), stderr: []byte("gh: Not Found (HTTP 404)"), exitCode: 1},
		scriptedCall{stdout: []byte(`{"commit":{"sha":"commit-sha-1"}}`)},
		scriptedCall{stdout: []byte(`[]`)},
		scriptedCall{stdout: []byte(`{"html_url":"https://github.com/octocat/Hello-World/pull/42"}`)},
	))
	return github.UpsertRulesPullRequest(context.Background(), publicationRequest())
}

func updatePublication(t *testing.T) (ports.PublicationResult, error) {
	t.Helper()
	github := newTestGitHub(newScriptedRunner(t,
		scriptedCall{stdout: []byte(`{"object":{"sha":"branch-sha"}}`)},
		scriptedCall{stdout: []byte(`{"sha":"old-file-sha"}`)},
		scriptedCall{stdout: []byte(`{"commit":{"sha":"commit-sha-2"}}`)},
		scriptedCall{stdout: []byte(`[{"html_url":"https://github.com/octocat/Hello-World/pull/41","head":{"ref":"automation/ai-review-rules"}}]`)},
	))
	return github.UpsertRulesPullRequest(context.Background(), publicationRequest())
}

func TestPublicationConversation(t *testing.T) {
	testigo.Run(t, "requires publication fields", func(t *testing.T) {
		_, err := New("github.com", "2022-11-28", time.Second, 1).UpsertRulesPullRequest(context.Background(), ports.PublicationRequest{})
		assert.Error(t, err)
	})

	testigo.Run(t, "reports a created publication", func(t *testing.T) {
		result, err := createPublication(t)
		assert.NoError(t, err)
		assert.Equal(t, result.Status, domain.PublicationCreated)
	})

	testigo.Run(t, "returns the created commit", func(t *testing.T) {
		result, err := createPublication(t)
		assert.NoError(t, err)
		assert.Equal(t, result.CommitSHA, "commit-sha-1")
	})

	testigo.Run(t, "returns the created pull request", func(t *testing.T) {
		result, err := createPublication(t)
		assert.NoError(t, err)
		assert.Equal(t, result.PullRequestURL, "https://github.com/octocat/Hello-World/pull/42")
	})

	testigo.Run(t, "reports an updated publication", func(t *testing.T) {
		result, err := updatePublication(t)
		assert.NoError(t, err)
		assert.Equal(t, result.Status, domain.PublicationUpdated)
	})

	testigo.Run(t, "keeps the existing pull request", func(t *testing.T) {
		result, err := updatePublication(t)
		assert.NoError(t, err)
		assert.Equal(t, result.PullRequestURL, "https://github.com/octocat/Hello-World/pull/41")
	})
}
