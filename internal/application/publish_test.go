package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/fake"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/markdown"
	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func publishContentHash(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func filteredSnapshotHash(title string, rules ...domain.Rule) string {
	return publishContentHash(markdown.Render(title, rules))
}

func TestPublicationOptionDefaults(t *testing.T) {
	testigo.Run(t, "default branch", func(t *testing.T) {
		assert.Equal(t, NewPublishOptions(PublishOptions{}).Branch, "automation/ai-review-rules")
	})
	testigo.Run(t, "default title", func(t *testing.T) {
		assert.Equal(t, NewPublishOptions(PublishOptions{}).Title, "chore: update AI review rules")
	})
	testigo.Run(t, "default commit message", func(t *testing.T) {
		assert.Equal(t, NewPublishOptions(PublishOptions{}).CommitMessage, "chore: update AI review rules")
	})
	testigo.Run(t, "configured branch", func(t *testing.T) {
		assert.Equal(t, NewPublishOptions(PublishOptions{Branch: "custom/branch"}).Branch, "custom/branch")
	})
	testigo.Run(t, "configured title", func(t *testing.T) {
		assert.Equal(t, NewPublishOptions(PublishOptions{Title: "custom title"}).Title, "custom title")
	})
	testigo.Run(t, "configured commit message", func(t *testing.T) {
		assert.Equal(t, NewPublishOptions(PublishOptions{CommitMessage: "custom commit"}).CommitMessage, "custom commit")
	})
}

type publishGitHubFixture struct {
	requests []ports.PublicationRequest
	remote   []byte
	reads    []string
	readErr  error
}

func (*publishGitHubFixture) ListReviewComments(context.Context, string, ports.Cursor) ([]ports.ReviewComment, ports.Cursor, error) {
	return nil, ports.Cursor{}, nil
}
func (*publishGitHubFixture) ListReactions(context.Context, string, int64, string) (ports.ReactionPage, error) {
	return ports.ReactionPage{}, nil
}
func (*publishGitHubFixture) RepositoryLanguages(context.Context, string) ([]ports.Language, error) {
	return nil, nil
}
func (*publishGitHubFixture) DefaultBranch(context.Context, string) (string, error) {
	return "main", nil
}
func (github *publishGitHubFixture) ReadFile(_ context.Context, repo string, path string, branch string) ([]byte, error) {
	github.reads = append(github.reads, repo+"|"+path+"|"+branch)
	return append([]byte(nil), github.remote...), github.readErr
}
func (github *publishGitHubFixture) UpsertRulesPullRequest(_ context.Context, request ports.PublicationRequest) (ports.PublicationResult, error) {
	github.requests = append(github.requests, request)
	return ports.PublicationResult{
		Status: domain.PublicationCreated, CommitSHA: "abc123",
		PullRequestURL: "https://github.com/example-org/example-source/pull/1",
	}, nil
}

type publicationScenario struct {
	publication domain.Publication
	summary     Summary
	requests    []ports.PublicationRequest
	reads       []string
	wantHash    string
}

func newPublicationScenario(t *testing.T) publicationScenario {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	store := newFakePublishStore()
	applicationtest.NewRepositoryDirectorySeeder(t, store.repositories, ctx).One(
		applicationtest.Repository.WithFullName("example-org/example-source").WithDefaultBranch("main").Bare(),
	)
	repository, found, err := store.repositories.GetByFullName(ctx, "example-org/example-source")
	if err != nil || !found {
		t.Fatalf("get seeded repository: found=%t err=%v", found, err)
	}
	commonRule := applicationtest.ActiveRule.WithPublishRule("AIR-COMMON-001", "common", now).Bare()
	goRule := applicationtest.ActiveRule.WithPublishRule("AIR-GO-001", "go", now).Bare()
	phpRule := applicationtest.ActiveRule.WithPublishRule("AIR-PHP-001", "php", now).Bare()
	applicationtest.NewRuleCatalogSeeder(t, store.rules, ctx).Many(commonRule, goRule, phpRule)
	github := &publishGitHubFixture{remote: []byte("# Repository guidance\n\nKeep this paragraph.\n")}
	deps := PublishDeps{
		GitHub: github, Classifier: fake.NewClassifier(), Repository: store.repositories, Rules: store.rules,
		Publications: store.publications, Snapshots: store.snapshots, Clock: applicationtest.Clock{Time: now},
	}
	options := NewPublishOptions(PublishOptions{Title: "AI review rules", Repositories: []PublishActionableRepository{
		{FullName: "example-org/example-source", RulesPath: "docs/AI_REVIEW_RULES.md", Languages: []string{"go"}},
	}})
	summary, err := Publish(ctx, deps, options)
	if err != nil {
		t.Fatal(err)
	}
	publication, found, err := store.publications.LastForRepository(ctx, repository.ID)
	if err != nil || !found {
		t.Fatalf("get publication: found=%t err=%v", found, err)
	}
	return publicationScenario{
		publication: publication, summary: summary, requests: github.requests, reads: github.reads,
		wantHash: filteredSnapshotHash("AI review rules", commonRule, goRule),
	}
}

func newAlreadyCurrentScenario(t *testing.T) publicationScenario {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	store := newFakePublishStore()
	applicationtest.NewRepositoryDirectorySeeder(t, store.repositories, ctx).One(
		applicationtest.Repository.WithFullName("example-org/example-source").WithDefaultBranch("main").Bare(),
	)
	repository, found, err := store.repositories.GetByFullName(ctx, "example-org/example-source")
	if err != nil || !found {
		t.Fatalf("get seeded repository: found=%t err=%v", found, err)
	}
	commonRule := applicationtest.ActiveRule.WithPublishRule("AIR-COMMON-001", "common", now).Bare()
	goRule := applicationtest.ActiveRule.WithPublishRule("AIR-GO-001", "go", now).Bare()
	applicationtest.NewRuleCatalogSeeder(t, store.rules, ctx).Many(commonRule, goRule)
	hash := filteredSnapshotHash("AI review rules", commonRule, goRule)
	applicationtest.NewPublicationJournalSeeder(t, store.publications, ctx).One(domain.Publication{
		RepositoryID: repository.ID, FilteredSnapshotHash: hash, Status: domain.PublicationAlreadyCurrent, CreatedAt: now,
	})
	github := &publishGitHubFixture{}
	deps := PublishDeps{
		GitHub: github, Classifier: fake.NewClassifier(), Repository: store.repositories, Rules: store.rules,
		Publications: store.publications, Snapshots: store.snapshots, Clock: applicationtest.Clock{Time: now},
	}
	summary, err := Publish(ctx, deps, NewPublishOptions(PublishOptions{Title: "AI review rules", Repositories: []PublishActionableRepository{
		{FullName: "example-org/example-source", RulesPath: "docs/AI_REVIEW_RULES.md", Languages: []string{"go"}},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	return publicationScenario{summary: summary, requests: github.requests, wantHash: hash}
}

func TestPublicationConversation(t *testing.T) {
	testigo.Run(t, "When a publication is prepared, reads the configured remote file from the default branch", func(t *testing.T) {
		scenario := newPublicationScenario(t)
		assert.Equal(t, scenario.reads, []string{"example-org/example-source|docs/AI_REVIEW_RULES.md|main"})
	})

	testigo.Run(t, "When a remote file contains unmanaged text, preserves it in the publication", func(t *testing.T) {
		scenario := newPublicationScenario(t)
		assert.Contains(t, string(scenario.requests[0].Content), "Keep this paragraph.")
	})

	testigo.Run(t, "persists created status", func(t *testing.T) {
		assert.Equal(t, newPublicationScenario(t).publication.Status, domain.PublicationCreated)
	})

	testigo.Run(t, "persists the pull request URL", func(t *testing.T) {
		assert.Equal(t, newPublicationScenario(t).publication.PullRequestURL, "https://github.com/example-org/example-source/pull/1")
	})

	testigo.Run(t, "hashes only common and configured-language rules", func(t *testing.T) {
		scenario := newPublicationScenario(t)
		assert.Equal(t, scenario.publication.FilteredSnapshotHash, scenario.wantHash)
	})

	testigo.Run(t, "sends one publication request", func(t *testing.T) {
		assert.Len(t, newPublicationScenario(t).requests, 1)
	})

	testigo.Run(t, "reports an unchanged publication", func(t *testing.T) {
		scenario := newAlreadyCurrentScenario(t)
		assert.Equal(t, scenario.summary.Repositories[0].Status, domain.PublicationAlreadyCurrent)
	})

	testigo.Run(t, "does not publish unchanged content", func(t *testing.T) {
		assert.Empty(t, newAlreadyCurrentScenario(t).requests)
	})
}
