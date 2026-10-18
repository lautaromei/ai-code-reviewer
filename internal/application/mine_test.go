package application

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

type listReviewCommentsCall struct {
	repository string
	since      ports.Cursor
}

type mineGitHubFixture struct {
	comments []ports.ReviewComment
	listErr  error
	calls    []listReviewCommentsCall
}

func (github *mineGitHubFixture) ListReviewComments(_ context.Context, repository string, since ports.Cursor) ([]ports.ReviewComment, ports.Cursor, error) {
	github.calls = append(github.calls, listReviewCommentsCall{repository: repository, since: since})
	return github.comments, since, github.listErr
}
func (*mineGitHubFixture) ListReactions(context.Context, string, int64, string) (ports.ReactionPage, error) {
	return ports.ReactionPage{}, nil
}
func (*mineGitHubFixture) RepositoryLanguages(context.Context, string) ([]ports.Language, error) {
	return nil, nil
}
func (*mineGitHubFixture) DefaultBranch(context.Context, string) (string, error) { return "", nil }
func (*mineGitHubFixture) ReadFile(context.Context, string, string, string) ([]byte, error) {
	return nil, nil
}
func (*mineGitHubFixture) UpsertRulesPullRequest(context.Context, ports.PublicationRequest) (ports.PublicationResult, error) {
	return ports.PublicationResult{}, nil
}

func ruleIDPattern(t *testing.T) *regexp.Regexp {
	t.Helper()
	pattern, err := BuildRuleIDPattern("AIR")
	if err != nil {
		t.Fatal(err)
	}
	return pattern
}

type mineStoreScenario struct {
	store      *fakeMineStore
	repository domain.Repository
}

func newMineStoreScenario(t *testing.T) mineStoreScenario {
	t.Helper()
	store := newFakeMineStore()
	input := applicationtest.Repository.WithAnalyzed(true).Bare()
	applicationtest.NewRepositoryDirectorySeeder(t, store.repositories, context.Background()).One(input)
	repository, found, err := store.repositories.GetByFullName(context.Background(), input.FullName)
	if err != nil || !found {
		t.Fatalf("get seeded repository: found=%t err=%v", found, err)
	}
	return mineStoreScenario{store: store, repository: repository}
}

type firstMineScenario struct {
	store      *fakeMineStore
	repository domain.Repository
	summary    MineSummary
	calls      []listReviewCommentsCall
	comment    domain.Comment
	cursor     domain.Cursor
}

func firstMine(t *testing.T) firstMineScenario {
	t.Helper()
	scenario := newMineStoreScenario(t)
	initialSince := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	createdAt := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	github := &mineGitHubFixture{comments: applicationtest.ReviewComment.With(applicationtest.AsHumanReviewComment(createdAt)).Times(1)}
	summary, err := MineRepository(context.Background(), github, scenario.store.cursors, scenario.store.comments, scenario.store.rules, applicationtest.Clock{Time: createdAt}, MineRepositoryInput{
		Repository: scenario.repository, InitialSince: initialSince,
		ExtensionToLanguage: map[string]string{".go": "go"}, RulePattern: ruleIDPattern(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	comment, found, err := scenario.store.comments.GetByGitHubID(context.Background(), scenario.repository.ID, 1)
	if err != nil || !found {
		t.Fatalf("get mined comment: found=%t err=%v", found, err)
	}
	cursor, found, err := scenario.store.cursors.Get(context.Background(), scenario.repository.ID, commentsStream)
	if err != nil || !found {
		t.Fatalf("get comment cursor: found=%t err=%v", found, err)
	}
	return firstMineScenario{store: scenario.store, repository: scenario.repository, summary: summary, calls: github.calls, comment: comment, cursor: cursor}
}

func TestFirstMiningConversation(t *testing.T) {
	testigo.Run(t, "starts at the configured initial date", func(t *testing.T) {
		scenario := firstMine(t)
		assert.Equal(t, scenario.calls[0].since.Since, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	})

	testigo.Run(t, "reports the seen comment", func(t *testing.T) {
		assert.Equal(t, firstMine(t).summary.CommentsSeen, 1)
	})

	testigo.Run(t, "persists the comment", func(t *testing.T) {
		assert.Equal(t, firstMine(t).summary.CommentsUpserted, 1)
	})

	testigo.Run(t, "classifies a human author", func(t *testing.T) {
		assert.Equal(t, firstMine(t).comment.AuthorKind, domain.AuthorHuman)
	})

	testigo.Run(t, "classifies human origin", func(t *testing.T) {
		assert.Equal(t, firstMine(t).comment.Origin, domain.OriginHuman)
	})

	testigo.Run(t, "detects the file language", func(t *testing.T) {
		assert.Equal(t, firstMine(t).comment.Language, "go")
	})

	testigo.Run(t, "advances to the comment update date", func(t *testing.T) {
		assert.Equal(t, firstMine(t).cursor.HighWatermark, time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC))
	})
}

func cursorRequest(t *testing.T, initialSince, watermark time.Time, overlap time.Duration) listReviewCommentsCall {
	t.Helper()
	scenario := newMineStoreScenario(t)
	applicationtest.NewCommentCursorSeeder(t, scenario.store.cursors, context.Background()).One(domain.Cursor{
		RepositoryID: scenario.repository.ID, Stream: commentsStream, HighWatermark: watermark,
	})
	github := &mineGitHubFixture{}
	_, err := MineRepository(context.Background(), github, scenario.store.cursors, scenario.store.comments, scenario.store.rules, applicationtest.Clock{Time: watermark}, MineRepositoryInput{
		Repository: scenario.repository, CursorOverlap: overlap, InitialSince: initialSince, RulePattern: ruleIDPattern(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return github.calls[0]
}

func TestSubsequentMiningCursor(t *testing.T) {
	testigo.Run(t, "subtracts the configured overlap", func(t *testing.T) {
		watermark := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
		call := cursorRequest(t, time.Date(2008, 1, 1, 0, 0, 0, 0, time.UTC), watermark, 10*time.Minute)
		assert.Equal(t, call.since.Since, watermark.Add(-10*time.Minute))
	})

	testigo.Run(t, "never precedes the initial date", func(t *testing.T) {
		initial := time.Date(2026, 8, 1, 11, 55, 0, 0, time.UTC)
		call := cursorRequest(t, initial, initial.Add(2*time.Minute), time.Hour)
		assert.Equal(t, call.since.Since, initial)
	})
}

type replyScenario struct {
	summary     MineSummary
	rootExists  bool
	replyExists bool
}

func mineWithoutReplies(t *testing.T) replyScenario {
	t.Helper()
	scenario := newMineStoreScenario(t)
	createdAt := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	github := &mineGitHubFixture{comments: []ports.ReviewComment{
		applicationtest.ReviewComment.With(applicationtest.AsHumanReviewComment(createdAt)).Bare(),
		applicationtest.ReviewComment.With(applicationtest.AsHumanReviewComment(createdAt), applicationtest.AsReply(1)).Bare(),
	}}
	summary, err := MineRepository(context.Background(), github, scenario.store.cursors, scenario.store.comments, scenario.store.rules, applicationtest.Clock{Time: createdAt}, MineRepositoryInput{
		Repository: scenario.repository, RulePattern: ruleIDPattern(t), IncludeReplies: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, rootExists, err := scenario.store.comments.GetByGitHubID(context.Background(), scenario.repository.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	_, replyExists, err := scenario.store.comments.GetByGitHubID(context.Background(), scenario.repository.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	return replyScenario{summary: summary, rootExists: rootExists, replyExists: replyExists}
}

func TestReplyFilteringConversation(t *testing.T) {
	testigo.Run(t, "counts only the root comment", func(t *testing.T) {
		assert.Equal(t, mineWithoutReplies(t).summary.CommentsSeen, 1)
	})
	testigo.Run(t, "persists the root comment", func(t *testing.T) {
		assert.True(t, mineWithoutReplies(t).rootExists, "root comment should exist")
	})
	testigo.Run(t, "does not persist the reply", func(t *testing.T) {
		assert.False(t, mineWithoutReplies(t).replyExists, "reply should be absent")
	})
}

func minedRuleUsage(t *testing.T) domain.Rule {
	t.Helper()
	scenario := newMineStoreScenario(t)
	usedAt := time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)
	applicationtest.NewRuleCatalogSeeder(t, scenario.store.rules, context.Background()).One(
		applicationtest.ActiveRule.WithActiveRuleIdentity("AIR-GO-001", usedAt.Add(-time.Hour)).Bare(),
	)
	github := &mineGitHubFixture{comments: []ports.ReviewComment{applicationtest.ReviewComment.With(applicationtest.AsAIRuleUsage(usedAt)).Bare()}}
	_, err := MineRepository(context.Background(), github, scenario.store.cursors, scenario.store.comments, scenario.store.rules, applicationtest.Clock{Time: usedAt}, MineRepositoryInput{
		Repository: scenario.repository, AIAuthors: []string{"coderabbitai[bot]"}, RulePattern: ruleIDPattern(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	rule, found, err := scenario.store.rules.Get(context.Background(), "AIR-GO-001")
	if err != nil || !found {
		t.Fatalf("get used rule: found=%t err=%v", found, err)
	}
	return rule
}

func TestAIRuleUsageConversation(t *testing.T) {
	testigo.Run(t, "updates the rule's last use date", func(t *testing.T) {
		assert.Equal(t, *minedRuleUsage(t).LastUsedAt, time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC))
	})
}

type failedMineScenario struct {
	summary      MineSummary
	err          error
	calls        []listReviewCommentsCall
	cursorExists bool
}

func failedMine(t *testing.T, nilContext bool) failedMineScenario {
	t.Helper()
	scenario := newMineStoreScenario(t)
	github := &mineGitHubFixture{comments: applicationtest.ReviewComment.Times(1), listErr: context.DeadlineExceeded}
	ctx := context.Background()
	if nilContext {
		ctx = nil
	}
	summary, err := MineRepository(ctx, github, scenario.store.cursors, scenario.store.comments, scenario.store.rules, applicationtest.Clock{}, MineRepositoryInput{
		Repository: scenario.repository, RulePattern: ruleIDPattern(t),
	})
	_, cursorExists, cursorErr := scenario.store.cursors.Get(context.Background(), scenario.repository.ID, commentsStream)
	if cursorErr != nil {
		t.Fatal(cursorErr)
	}
	return failedMineScenario{summary: summary, err: err, calls: github.calls, cursorExists: cursorExists}
}

func TestFailedMiningConversation(t *testing.T) {
	testigo.Run(t, "preserves the listing failure", func(t *testing.T) {
		assert.ErrorIs(t, failedMine(t, false).err, context.DeadlineExceeded)
	})
	testigo.Run(t, "does not count comments after listing failure", func(t *testing.T) {
		assert.Zero(t, failedMine(t, false).summary.CommentsSeen)
	})
	testigo.Run(t, "does not advance the cursor after listing failure", func(t *testing.T) {
		assert.False(t, failedMine(t, false).cursorExists, "cursor should be absent")
	})
	testigo.Run(t, "rejects a nil context", func(t *testing.T) {
		assert.Error(t, failedMine(t, true).err)
	})
	testigo.Run(t, "does not call GitHub with a nil context", func(t *testing.T) {
		assert.Empty(t, failedMine(t, true).calls)
	})
	testigo.Run(t, "does not advance the cursor with a nil context", func(t *testing.T) {
		assert.False(t, failedMine(t, true).cursorExists, "cursor should be absent")
	})
}
