package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/fake"
	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

// commentListFault remains hand-written because syncCommentRepository combines
// several unrelated query methods and this test needs only ListTrackedAI to fail.
type commentListFault struct {
	testComments
	allErr     error
	trackedErr error
}

func (f commentListFault) ListAllOrdered(ctx context.Context) ([]domain.Comment, error) {
	if f.allErr != nil {
		return nil, f.allErr
	}
	return f.testComments.ListAllOrdered(ctx)
}

func (f commentListFault) ListTrackedAI(ctx context.Context) ([]domain.Comment, error) {
	if f.trackedErr != nil {
		return nil, f.trackedErr
	}
	return f.testComments.ListTrackedAI(ctx)
}

func TestCommentExportValidationAndFailures(t *testing.T) {
	store := newFakeExportStore()
	for _, scenario := range []struct {
		name string
		call func(t *testing.T) error
		want string
	}{
		{"requires a context", func(t *testing.T) error {
			return ExportCommentsJSONL(nil, store.repositories, store.comments, "comments.jsonl")
		}, "ctx is nil"},
		{"requires a destination", func(t *testing.T) error {
			return ExportCommentsJSONL(context.Background(), store.repositories, store.comments, "")
		}, "path is empty"},
		{"reports unavailable repositories", func(t *testing.T) error {
			return ExportCommentsJSONL(context.Background(), NewExportRepositoriesErrorsErrorStub(Repository, collaboratorUnavailable), store.comments, filepath.Join(t.TempDir(), "comments.jsonl"))
		}, "list repositories"},
		{"reports unavailable comments", func(t *testing.T) error {
			return ExportCommentsJSONL(context.Background(), store.repositories, NewExportCommentsErrorsErrorStub(Comment, collaboratorUnavailable), filepath.Join(t.TempDir(), "comments.jsonl"))
		}, "list comments"},
		{"reports an invalid destination parent", func(t *testing.T) error {
			parent := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(parent, []byte("blocker"), 0o600); err != nil {
				t.Fatal(err)
			}
			return ExportCommentsJSONL(context.Background(), store.repositories, store.comments, filepath.Join(parent, "comments.jsonl"))
		}, "create directory"},
		{"reports a destination that cannot be replaced", func(t *testing.T) error {
			destination := filepath.Join(t.TempDir(), "comments.jsonl")
			if err := os.Mkdir(destination, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(destination, "keep"), []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			return ExportCommentsJSONL(context.Background(), store.repositories, store.comments, destination)
		}, "renombrar"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			assert.ErrorContains(t, scenario.call(t), scenario.want)
		})
	}
}

type temporaryCommentFileFault struct {
	writeErr error
	syncErr  error
	closeErr error
}

func (f *temporaryCommentFileFault) Write(content []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(content), nil
}
func (*temporaryCommentFileFault) Name() string   { return "temporary-comments" }
func (f *temporaryCommentFileFault) Sync() error  { return f.syncErr }
func (f *temporaryCommentFileFault) Close() error { return f.closeErr }

type commentDestinationFault struct {
	file       temporaryCommentFile
	createErr  error
	replaceErr error
}

func (f commentDestinationFault) CreateDirectory(string) error { return nil }
func (f commentDestinationFault) CreateTemporary(string) (temporaryCommentFile, error) {
	return f.file, f.createErr
}
func (f commentDestinationFault) Replace(string, string) error { return f.replaceErr }
func (commentDestinationFault) Remove(string) error            { return nil }

type exportedComments struct{ comments []domain.Comment }

func (c exportedComments) ListAllOrdered(context.Context) ([]domain.Comment, error) {
	return c.comments, nil
}

func TestCommentExportReportsDestinationFailures(t *testing.T) {
	repositories := fake.NewRepositoryDirectory()
	for _, scenario := range []struct {
		name        string
		comments    []domain.Comment
		destination commentDestination
		want        string
	}{
		{"reports unavailable temporary storage", nil, commentDestinationFault{createErr: collaboratorUnavailable}, "create temporary file"},
		{"reports a serialization failure", []domain.Comment{applicationtest.Comment.WithGitHubCommentID(9).Bare()}, commentDestinationFault{file: &temporaryCommentFileFault{writeErr: collaboratorUnavailable}}, "serialize comment 9"},
		{"reports a synchronization failure", nil, commentDestinationFault{file: &temporaryCommentFileFault{syncErr: collaboratorUnavailable}}, "sync temporary file"},
		{"reports a close failure", nil, commentDestinationFault{file: &temporaryCommentFileFault{closeErr: collaboratorUnavailable}}, "close temporary file"},
		{"reports a replacement failure", nil, commentDestinationFault{file: &temporaryCommentFileFault{}, replaceErr: collaboratorUnavailable}, "renombrar"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			err := exportCommentsJSONL(context.Background(), repositories, exportedComments{comments: scenario.comments}, "comments.jsonl", scenario.destination)
			assert.ErrorContains(t, err, scenario.want)
		})
	}
}

type githubContract struct {
	GitHub      ports.CodeReviewGateway
	comments    []ports.ReviewComment
	listErr     error
	reactions   ports.ReactionPage
	reactErr    error
	branch      string
	branchErr   error
	readErr     error
	languages   []ports.Language
	languageErr error
	publication ports.PublicationResult
	publishErr  error
}

func (g githubContract) ListReviewComments(_ context.Context, _ string, cursor ports.Cursor) ([]ports.ReviewComment, ports.Cursor, error) {
	return g.comments, cursor, g.listErr
}
func (g githubContract) ListReactions(context.Context, string, int64, string) (ports.ReactionPage, error) {
	return g.reactions, g.reactErr
}
func (g githubContract) DefaultBranch(context.Context, string) (string, error) {
	return g.branch, g.branchErr
}
func (g githubContract) ReadFile(context.Context, string, string, string) ([]byte, error) {
	return nil, g.readErr
}
func (g githubContract) RepositoryLanguages(context.Context, string) ([]ports.Language, error) {
	return g.languages, g.languageErr
}
func (g githubContract) UpsertRulesPullRequest(context.Context, ports.PublicationRequest) (ports.PublicationResult, error) {
	return g.publication, g.publishErr
}

type cursorFault struct {
	testCursors
	getErr     error
	advanceErr error
}

func (f cursorFault) Get(context.Context, int64, string) (domain.Cursor, bool, error) {
	return domain.Cursor{}, false, f.getErr
}
func (f cursorFault) Advance(context.Context, domain.Cursor) error { return f.advanceErr }

func mineInput() MineRepositoryInput {
	pattern := regexp.MustCompile(`AIR-GO-[0-9]+`)
	return MineRepositoryInput{Repository: domain.Repository{ID: 1, FullName: "owner/repo"}, RulePattern: pattern}
}

func TestMiningValidationAndFailures(t *testing.T) {
	store := newFakeMineStore()
	ctx := context.Background()
	clock := applicationtest.Clock{Time: functionalTime}
	for _, scenario := range []struct {
		name string
		call func() error
		want string
	}{
		{"requires a code review gateway", func() error {
			_, err := MineRepository(ctx, nil, store.cursors, store.comments, store.rules, clock, mineInput())
			return err
		}, "CodeReviewGateway"},
		{"requires a clock", func() error {
			_, err := MineRepository(ctx, fake.NewGitHub(), store.cursors, store.comments, store.rules, nil, mineInput())
			return err
		}, "Clock"},
		{"requires repositories", func() error {
			_, err := MineRepository(ctx, fake.NewGitHub(), nil, store.comments, store.rules, clock, mineInput())
			return err
		}, "incomplete"},
		{"reports an unavailable cursor", func() error {
			_, err := MineRepository(ctx, fake.NewGitHub(), cursorFault{testCursors: store.cursors, getErr: collaboratorUnavailable}, store.comments, store.rules, clock, mineInput())
			return err
		}, "read cursor"},
		{"reports an unavailable comment archive", func() error {
			gh := githubContract{GitHub: fake.NewGitHub(), comments: []ports.ReviewComment{{GitHubCommentID: 1}}}
			comments := applicationtest.NewCommentArchiveErrorsErrorStub(applicationtest.Comment, collaboratorUnavailable)
			_, err := MineRepository(ctx, gh, store.cursors, comments, store.rules, clock, mineInput())
			return err
		}, "save comment"},
		{"reports an unavailable usage history", func() error {
			gh := githubContract{GitHub: fake.NewGitHub(), comments: []ports.ReviewComment{{GitHubCommentID: 1, Author: "ai", AuthorIsBot: true, Body: "AIR-GO-001"}}}
			input := mineInput()
			input.AIAuthors = []string{"ai"}
			_, err := MineRepository(ctx, gh, store.cursors, store.comments, applicationtest.NewRuleUsageHistoryErrorsErrorStub(collaboratorUnavailable), clock, input)
			return err
		}, "record rule"},
		{"reports an unavailable cursor advance", func() error {
			_, err := MineRepository(ctx, fake.NewGitHub(), cursorFault{testCursors: store.cursors, advanceErr: collaboratorUnavailable}, store.comments, store.rules, clock, mineInput())
			return err
		}, "avanzar cursor"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			assert.ErrorContains(t, scenario.call(), scenario.want)
		})
	}
}

func reactionStoreWithComment(t *testing.T) (*fakeReactionStore, domain.Comment) {
	t.Helper()
	store := newFakeReactionStore()
	repository, err := store.repositories.Upsert(context.Background(), domain.Repository{FullName: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	commentFixture := applicationtest.Comment.WithRepositoryID(repository.ID).WithGitHubCommentID(9).WithOrigin(domain.OriginAICandidate).Bare()
	comment, err := store.comments.Upsert(context.Background(), commentFixture)
	if err != nil {
		t.Fatal(err)
	}
	return store, comment
}

func TestReactionReconciliationFailures(t *testing.T) {
	testigo.Run(t, "reports unavailable tracked comments", func(t *testing.T) {
		store := newFakeReactionStore()
		_, err := ReconcileReactions(context.Background(), fake.NewGitHub(), store.repositories, NewTrackedCommentsErrorsErrorStub(Comment, collaboratorUnavailable), store.feedback, applicationtest.Clock{})
		assert.ErrorIs(t, err, collaboratorUnavailable)
	})

	testigo.Run(t, "reports unavailable repositories", func(t *testing.T) {
		store := newFakeReactionStore()
		_, err := ReconcileReactions(context.Background(), fake.NewGitHub(), NewExportRepositoriesErrorsErrorStub(Repository, collaboratorUnavailable), store.comments, store.feedback, applicationtest.Clock{})
		assert.ErrorIs(t, err, collaboratorUnavailable)
	})

	testigo.Run(t, "reports unavailable GitHub reactions", func(t *testing.T) {
		store, _ := reactionStoreWithComment(t)
		_, err := ReconcileReactions(context.Background(), githubContract{GitHub: fake.NewGitHub(), reactErr: collaboratorUnavailable}, store.repositories, store.comments, store.feedback, applicationtest.Clock{})
		assert.ErrorIs(t, err, collaboratorUnavailable)
	})

	testigo.Run(t, "keeps reactions when GitHub reports no change", func(t *testing.T) {
		store, _ := reactionStoreWithComment(t)
		summary, err := ReconcileReactions(context.Background(), githubContract{GitHub: fake.NewGitHub(), reactions: ports.ReactionPage{NotModified: true}}, store.repositories, store.comments, store.feedback, applicationtest.Clock{})
		assert.NoError(t, err)
		assert.Zero(t, summary.ReactionsReplaced)
	})

	testigo.Run(t, "reports unavailable reaction persistence", func(t *testing.T) {
		store, _ := reactionStoreWithComment(t)
		_, err := ReconcileReactions(context.Background(), fake.NewGitHub(), store.repositories, store.comments, NewReactionSinkErrorsErrorStub(collaboratorUnavailable), applicationtest.Clock{})
		assert.ErrorIs(t, err, collaboratorUnavailable)
	})
}

type lifecycleRulesFault struct {
	testRules
	listErr   error
	updateErr error
}

func (f lifecycleRulesFault) ListByStatus(ctx context.Context, status domain.RuleStatus) ([]domain.Rule, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.testRules.ListByStatus(ctx, status)
}
func (f lifecycleRulesFault) UpdateStatus(context.Context, domain.RuleStatusEvent) error {
	return f.updateErr
}

type lifecycleFeedbackFault struct {
	testFeedback
	err   error
	count int
}

func (f lifecycleFeedbackFault) CountByRule(context.Context, string, string) (int, error) {
	return f.count, f.err
}

func TestRuleLifecycleFailures(t *testing.T) {
	store := newFakeReactionStore()
	rule := applicationtest.ActiveRule.WithRuleID("AIR-GO-001").WithStatus(domain.RuleActive).WithActivatedAt(functionalTime.Add(-time.Hour)).Bare()
	if err := store.rules.Insert(context.Background(), rule); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []struct {
		name     string
		rules    lifecycleRuleRepository
		feedback LifecycleReactionRepository
		input    RuleLifecycleInput
	}{
		{"reports unavailable active rules", lifecycleRulesFault{testRules: store.rules, listErr: collaboratorUnavailable}, store.feedback, RuleLifecycleInput{}},
		{"reports unavailable feedback", store.rules, NewLifecycleFeedbackErrorsErrorStub(collaboratorUnavailable), RuleLifecycleInput{}},
		{"reports a failed downvote disable", lifecycleRulesFault{testRules: store.rules, updateErr: collaboratorUnavailable}, lifecycleFeedbackFault{testFeedback: store.feedback, count: 1}, RuleLifecycleInput{MinThumbsDown: 1}},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			_, err := EnforceRuleLifecycle(context.Background(), scenario.rules, scenario.feedback, applicationtest.Clock{Time: functionalTime}, scenario.input)
			assert.ErrorIs(t, err, collaboratorUnavailable)
		})
	}
}

func TestSnapshotFailures(t *testing.T) {
	testigo.Run(t, "reports unavailable active rules", func(t *testing.T) {
		_, err := GenerateSnapshot(context.Background(), NewSnapshotRulesErrorsErrorStub(ActiveRule, collaboratorUnavailable), fake.NewSnapshotArchive(), GenerateSnapshotInput{})
		assert.ErrorIs(t, err, collaboratorUnavailable)
	})

	testigo.Run(t, "reports unavailable snapshot persistence", func(t *testing.T) {
		_, err := GenerateSnapshot(context.Background(), fake.NewRuleCatalog(), NewSnapshotArchiveErrorsErrorStub(collaboratorUnavailable), GenerateSnapshotInput{})
		assert.ErrorIs(t, err, collaboratorUnavailable)
	})
}

type publishRepositoryFault struct {
	testRepositories
	getErr    error
	upsertErr error
}

func (f publishRepositoryFault) GetByFullName(ctx context.Context, name string) (domain.Repository, bool, error) {
	if f.getErr != nil {
		return domain.Repository{}, false, f.getErr
	}
	return f.testRepositories.GetByFullName(ctx, name)
}
func (f publishRepositoryFault) Upsert(ctx context.Context, repository domain.Repository) (domain.Repository, error) {
	if f.upsertErr != nil {
		return domain.Repository{}, f.upsertErr
	}
	return f.testRepositories.Upsert(ctx, repository)
}

type publishRulesFault struct {
	testRules
	err error
}

func (f publishRulesFault) ListActiveByLanguages(context.Context, []string) ([]domain.Rule, error) {
	return nil, f.err
}

type publicationJournalFault struct {
	testPublications
	lastErr error
	saveErr error
}

func (f publicationJournalFault) LastForRepository(context.Context, int64) (domain.Publication, bool, error) {
	return domain.Publication{}, false, f.lastErr
}
func (f publicationJournalFault) Save(context.Context, domain.Publication) error { return f.saveErr }

type latestSnapshotFault struct {
	testSnapshots
	err error
}

func (f latestSnapshotFault) Latest(context.Context) (domain.Snapshot, bool, error) {
	return domain.Snapshot{}, false, f.err
}

func publishScenario(t *testing.T) (*fakePublishStore, PublishDeps, PublishOptions) {
	t.Helper()
	store := newFakePublishStore()
	applicationtest.NewRepositoryDirectorySeeder(t, store.repositories, context.Background()).Persist(
		applicationtest.Repository.WithFullName("owner/repo"),
	)
	repository, found, err := store.repositories.GetByFullName(context.Background(), "owner/repo")
	if err != nil || !found || repository.ID == 0 {
		t.Fatalf("seed repository: found=%v err=%v", found, err)
	}
	github := githubContract{GitHub: fake.NewGitHub(), branch: "main", publication: ports.PublicationResult{Status: domain.PublicationCreated}}
	deps := PublishDeps{GitHub: github, Classifier: fake.NewClassifier(), Repository: store.repositories, Rules: store.rules, Publications: store.publications, Snapshots: store.snapshots, Clock: applicationtest.Clock{Time: functionalTime}}
	opts := NewPublishOptions(PublishOptions{Repositories: []PublishActionableRepository{{FullName: "owner/repo", RulesPath: "rules.md", Languages: []string{"go"}}}})
	return store, deps, opts
}

func publicationFailure(t *testing.T, deps PublishDeps, opts PublishOptions) string {
	t.Helper()
	summary, _ := Publish(context.Background(), deps, opts)
	if len(summary.Repositories) != 1 {
		t.Fatalf("expected one repository result, got %d", len(summary.Repositories))
	}
	return summary.Repositories[0].Error
}

func TestPublicationValidationAndDefaults(t *testing.T) {
	testigo.Run(t, "requires a context", func(t *testing.T) {
		_, deps, opts := publishScenario(t)
		_, err := Publish(nil, deps, opts)
		assert.ErrorContains(t, err, "ctx is nil")
	})

	testigo.Run(t, "requires its collaborators", func(t *testing.T) {
		_, err := Publish(context.Background(), PublishDeps{}, PublishOptions{})
		assert.ErrorContains(t, err, "dependency is nil")
	})

	testigo.Run(t, "describes a dry run without publishing", func(t *testing.T) {
		_, deps, opts := publishScenario(t)
		opts.DryRun = true
		summary, err := Publish(context.Background(), deps, opts)
		assert.NoError(t, err)
		assert.True(t, summary.DryRun, "summary should describe a dry run")
	})

	testigo.Run(t, "normalizes and deduplicates configured languages", func(t *testing.T) {
		assert.Equal(t, normalizeLanguages([]string{" Go ", "go", "", "PHP"}), []string{"go", "php"})
	})
}

func TestPublicationReportsRepositoryFailures(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(*fakePublishStore, *PublishDeps, *PublishOptions)
		want   string
	}{
		{"reports an unavailable repository lookup", func(store *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.Repository = publishRepositoryFault{testRepositories: store.repositories, getErr: collaboratorUnavailable}
		}, "collaborator unavailable"},
		{"reports an unavailable repository creation", func(store *fakePublishStore, deps *PublishDeps, opts *PublishOptions) {
			opts.Repositories[0].FullName = "owner/new"
			deps.Repository = publishRepositoryFault{testRepositories: store.repositories, upsertErr: collaboratorUnavailable}
		}, "create actionable repository"},
		{"reports an unavailable default branch", func(_ *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.GitHub = githubContract{GitHub: fake.NewGitHub(), branchErr: collaboratorUnavailable}
		}, "resolve default branch"},
		{"rejects an empty default branch", func(_ *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.GitHub = githubContract{GitHub: fake.NewGitHub()}
		}, "empty default branch"},
		{"reports an unavailable remote rules file", func(_ *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.GitHub = githubContract{GitHub: fake.NewGitHub(), branch: "main", readErr: collaboratorUnavailable}
		}, "read remote rules file"},
		{"reports unavailable language detection", func(_ *fakePublishStore, deps *PublishDeps, opts *PublishOptions) {
			opts.Repositories[0].Languages = nil
			deps.GitHub = githubContract{GitHub: fake.NewGitHub(), branch: "main", languageErr: collaboratorUnavailable}
		}, "detect languages"},
		{"reports unavailable metadata persistence", func(store *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.Repository = publishRepositoryFault{testRepositories: store.repositories, upsertErr: collaboratorUnavailable}
		}, "update repository metadata"},
		{"reports unavailable active rules", func(store *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.Rules = publishRulesFault{testRules: store.rules, err: collaboratorUnavailable}
		}, "collaborator unavailable"},
		{"reports unavailable publication history", func(store *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.Publications = publicationJournalFault{testPublications: store.publications, lastErr: collaboratorUnavailable}
		}, "collaborator unavailable"},
		{"reports unavailable snapshots", func(store *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.Snapshots = latestSnapshotFault{testSnapshots: store.snapshots, err: collaboratorUnavailable}
		}, "load latest snapshot"},
		{"reports unavailable GitHub publication", func(_ *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.GitHub = githubContract{GitHub: fake.NewGitHub(), branch: "main", publishErr: collaboratorUnavailable}
		}, "collaborator unavailable"},
		{"reports unavailable publication persistence", func(store *fakePublishStore, deps *PublishDeps, _ *PublishOptions) {
			deps.Publications = publicationJournalFault{testPublications: store.publications, saveErr: collaboratorUnavailable}
		}, "collaborator unavailable"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			store, deps, opts := publishScenario(t)
			scenario.change(store, &deps, &opts)
			assert.Contains(t, publicationFailure(t, deps, opts), scenario.want)
		})
	}
}

func TestPublicationDetectsAndRecordsCurrentLanguages(t *testing.T) {
	testigo.Run(t, "publishes rules for detected languages", func(t *testing.T) {
		_, deps, opts := publishScenario(t)
		opts.Repositories[0].Languages = nil
		deps.GitHub = githubContract{GitHub: fake.NewGitHub(), branch: "main", languages: []ports.Language{{Name: " Go "}, {Name: ""}}, publication: ports.PublicationResult{Status: domain.PublicationCreated}}
		summary, err := Publish(context.Background(), deps, opts)
		assert.NoError(t, err)
		assert.Equal(t, summary.Repositories[0].Status, domain.PublicationCreated)
	})

	testigo.Run(t, "links a publication to the latest snapshot", func(t *testing.T) {
		store, deps, opts := publishScenario(t)
		if err := store.snapshots.Save(context.Background(), domain.Snapshot{ID: 7, Hash: "snapshot"}); err != nil {
			t.Fatal(err)
		}
		_, err := Publish(context.Background(), deps, opts)
		assert.NoError(t, err)
		publication, _, err := store.publications.LastForRepository(context.Background(), 1)
		assert.NoError(t, err)
		assert.Equal(t, publication.SnapshotID, int64(7))
	})
}

type runJournalFault struct {
	testRuns
	startErr  error
	finishErr error
}

func (f runJournalFault) Start(ctx context.Context, run domain.Run) (domain.Run, error) {
	if f.startErr != nil {
		return domain.Run{}, f.startErr
	}
	return f.testRuns.Start(ctx, run)
}
func (f runJournalFault) Finish(context.Context, domain.Run) error { return f.finishErr }

type syncSnapshotFault struct {
	testSnapshots
	saveErr error
}

func (f syncSnapshotFault) Save(context.Context, domain.Snapshot) error { return f.saveErr }

func TestSyncDependencyConversations(t *testing.T) {
	store := newFakeSyncStore()
	valid := syncDeps(store, applicationtest.Clock{Time: functionalTime})
	for _, scenario := range []struct {
		name string
		deps SyncDeps
		want string
	}{
		{"requires a classifier", func() SyncDeps { deps := valid; deps.Classifier = nil; return deps }(), "Classifier"},
		{"requires every repository", func() SyncDeps { deps := valid; deps.Repositories = nil; return deps }(), "incomplete"},
		{"requires a clock", func() SyncDeps { deps := valid; deps.Clock = nil; return deps }(), "Clock"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			_, err := Sync(context.Background(), scenario.deps, SyncOptions{})
			assert.ErrorContains(t, err, scenario.want)
		})
	}

	testigo.Run(t, "describes analyzed repositories in a dry run", func(t *testing.T) {
		summary, err := Sync(context.Background(), valid, SyncOptions{DryRun: true, AnalyzedRepositories: []string{"owner/repo"}})
		assert.NoError(t, err)
		assert.Contains(t, summary.Message, "1 analyzed repository")
	})
}

func TestSyncReportsPipelineFailures(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		change func(t *testing.T, store *fakeSyncStore, deps *SyncDeps, opts *SyncOptions)
		want   string
	}{
		{"reports unavailable run start", func(_ *testing.T, store *fakeSyncStore, deps *SyncDeps, _ *SyncOptions) {
			deps.Runs = runJournalFault{testRuns: store.runs, startErr: collaboratorUnavailable}
		}, "record run start"},
		{"reports mining setup failure", func(_ *testing.T, _ *fakeSyncStore, _ *SyncDeps, opts *SyncOptions) {
			opts.AnalyzedRepositories = []string{"owner/repo"}
			opts.RulePrefix = ""
		}, "build rule ID pattern"},
		{"reports export failure", func(t *testing.T, _ *fakeSyncStore, _ *SyncDeps, opts *SyncOptions) {
			parent := filepath.Join(t.TempDir(), "file")
			if err := os.WriteFile(parent, []byte("blocker"), 0o600); err != nil {
				t.Fatal(err)
			}
			opts.JSONLPath = filepath.Join(parent, "comments.jsonl")
		}, "exportar JSONL"},
		{"reports reaction reconciliation failure", func(_ *testing.T, store *fakeSyncStore, deps *SyncDeps, _ *SyncOptions) {
			deps.Comments = commentListFault{testComments: store.comments, trackedErr: collaboratorUnavailable}
		}, "reconciliar reacciones"},
		{"reports classification failure", func(_ *testing.T, store *fakeSyncStore, deps *SyncDeps, _ *SyncOptions) {
			deps.Comments = commentsFault{testComments: store.comments, pendingErr: collaboratorUnavailable}
		}, "classify and promote"},
		{"reports lifecycle failure", func(_ *testing.T, store *fakeSyncStore, deps *SyncDeps, _ *SyncOptions) {
			deps.Rules = rulesFault{testRules: store.rules, listErr: collaboratorUnavailable}
		}, "enforce rule lifecycle"},
		{"reports unavailable run completion", func(_ *testing.T, store *fakeSyncStore, deps *SyncDeps, _ *SyncOptions) {
			deps.Runs = runJournalFault{testRuns: store.runs, finishErr: collaboratorUnavailable}
		}, "record run finish"},
		{"preserves a pipeline failure when run completion also fails", func(_ *testing.T, store *fakeSyncStore, deps *SyncDeps, _ *SyncOptions) {
			deps.Comments = commentListFault{testComments: store.comments, trackedErr: collaboratorUnavailable}
			deps.Runs = runJournalFault{testRuns: store.runs, finishErr: errors.New("finish unavailable")}
		}, "also failed to record run completion"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			store := newFakeSyncStore()
			deps := syncDeps(store, applicationtest.Clock{Time: functionalTime})
			opts := SyncOptions{}
			scenario.change(t, store, &deps, &opts)
			_, err := Sync(context.Background(), deps, opts)
			assert.ErrorContains(t, err, scenario.want)
		})
	}
}

func TestSyncRecordsPartialMining(t *testing.T) {
	testigo.Run(t, "continues when an analyzed repository cannot be resolved", func(t *testing.T) {
		store := newFakeSyncStore()
		deps := syncDeps(store, applicationtest.Clock{Time: functionalTime})
		deps.Repositories = publishRepositoryFault{testRepositories: store.repositories, getErr: collaboratorUnavailable}
		summary, err := Sync(context.Background(), deps, SyncOptions{AnalyzedRepositories: []string{"owner/repo"}, RulePrefix: "AIR"})
		assert.NoError(t, err)
		assert.Contains(t, summary.Message, "look up repository")
	})

	testigo.Run(t, "continues after a repository mining failure", func(t *testing.T) {
		store := newFakeSyncStore()
		deps := syncDeps(store, applicationtest.Clock{Time: functionalTime})
		deps.GitHub = githubContract{GitHub: fake.NewGitHub(), listErr: collaboratorUnavailable}
		summary, err := Sync(context.Background(), deps, SyncOptions{AnalyzedRepositories: []string{"owner/repo"}, RulePrefix: "AIR"})
		assert.NoError(t, err)
		assert.Contains(t, summary.Message, "1 failed")
	})

	testigo.Run(t, "exports JSONL after successful mining", func(t *testing.T) {
		store := newFakeSyncStore()
		path := filepath.Join(t.TempDir(), "comments.jsonl")
		_, err := Sync(context.Background(), syncDeps(store, applicationtest.Clock{Time: functionalTime}), SyncOptions{JSONLPath: path})
		assert.NoError(t, err)
		_, statErr := os.Stat(path)
		assert.NoError(t, statErr)
	})
}

func TestAnalyzedRepositoryResolution(t *testing.T) {
	testigo.Run(t, "reports an unavailable lookup", func(t *testing.T) {
		store := newFakeSyncStore()
		_, err := resolveAnalyzedRepository(context.Background(), publishRepositoryFault{testRepositories: store.repositories, getErr: collaboratorUnavailable}, "owner/repo")
		assert.ErrorContains(t, err, "look up repository")
	})

	testigo.Run(t, "reports an unavailable creation", func(t *testing.T) {
		store := newFakeSyncStore()
		repositories := publishRepositoryFault{testRepositories: store.repositories, upsertErr: collaboratorUnavailable}
		_, err := resolveAnalyzedRepository(context.Background(), repositories, "owner/repo")
		assert.ErrorContains(t, err, "create repository")
	})

	testigo.Run(t, "marks an existing repository as analyzed", func(t *testing.T) {
		store := newFakeSyncStore()
		applicationtest.NewRepositoryDirectorySeeder(t, store.repositories, context.Background()).Persist(
			applicationtest.Repository.WithFullName("owner/repo").WithAnalyzed(false),
		)
		repository, err := resolveAnalyzedRepository(context.Background(), store.repositories, "owner/repo")
		assert.NoError(t, err)
		assert.True(t, repository.Analyzed, "repository should become analyzed")
	})

	testigo.Run(t, "reports an unavailable analyzed update", func(t *testing.T) {
		store := newFakeSyncStore()
		applicationtest.NewRepositoryDirectorySeeder(t, store.repositories, context.Background()).Persist(
			applicationtest.Repository.WithFullName("owner/repo").WithAnalyzed(false),
		)
		repositories := publishRepositoryFault{testRepositories: store.repositories, upsertErr: collaboratorUnavailable}
		_, err := resolveAnalyzedRepository(context.Background(), repositories, "owner/repo")
		assert.ErrorContains(t, err, "mark repository as analyzed")
	})
}

func TestSeedImportSummaryIncludesParseFailures(t *testing.T) {
	testigo.Run(t, "reports malformed seed files without failing the run", func(t *testing.T) {
		store := newFakeSyncStore()
		dir := t.TempDir()
		writeFixture(t, dir, "broken.md", "## AIR-GO-BAD — Broken\n- **Rule:** broken\n")
		summary, err := runSeedImport(context.Background(), syncDeps(store, applicationtest.Clock{Time: functionalTime}), SyncOptions{SeedDir: dir})
		assert.NoError(t, err)
		assert.Contains(t, summary.Message, "error(es) de parseo")
	})
}
