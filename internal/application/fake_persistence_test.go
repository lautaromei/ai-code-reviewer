package application

import (
	"context"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/fake"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
)

type fakeSyncStore struct {
	repositories    testRepositories
	cursors         testCursors
	comments        testComments
	feedback        testFeedback
	rules           testRules
	candidates      testCandidates
	classifications testClassifications
	runs            testRuns
	snapshots       testSnapshots
	publications    testPublications
}

type fakeClassifyStore struct {
	comments        testComments
	feedback        testFeedback
	rules           testRules
	candidates      testCandidates
	classifications testClassifications
}

type fakeMineStore struct {
	repositories testRepositories
	cursors      testCursors
	comments     testComments
	rules        testRules
}

type fakeReactionStore struct {
	repositories testRepositories
	comments     testComments
	feedback     testFeedback
	rules        testRules
}

type fakeRulesStore struct{ rules testRules }
type fakeSnapshotStore struct {
	rules     testRules
	snapshots testSnapshots
}
type fakeExportStore struct {
	repositories testRepositories
	comments     testComments
}
type fakePublishStore struct {
	repositories testRepositories
	rules        testRules
	snapshots    testSnapshots
	publications testPublications
}

type testRepositories interface {
	Upsert(context.Context, domain.Repository) (domain.Repository, error)
	GetByFullName(context.Context, string) (domain.Repository, bool, error)
	List(context.Context) ([]domain.Repository, error)
}
type testCursors interface {
	Get(context.Context, int64, string) (domain.Cursor, bool, error)
	Advance(context.Context, domain.Cursor) error
}
type testComments interface {
	Upsert(context.Context, domain.Comment) (domain.Comment, error)
	GetByID(context.Context, int64) (domain.Comment, bool, error)
	GetByGitHubID(context.Context, int64, int64) (domain.Comment, bool, error)
	ListPendingClassification(context.Context, string) ([]domain.Comment, error)
	ListTrackedAI(context.Context) ([]domain.Comment, error)
	ListForRule(context.Context, string) ([]domain.Comment, error)
	ListAllOrdered(context.Context) ([]domain.Comment, error)
}
type testFeedback interface {
	ReplaceForComment(context.Context, int64, []domain.Reaction) error
	CountByRule(context.Context, string, string) (int, error)
	CountByComment(context.Context, int64, string) (int, error)
}
type testRules interface {
	Insert(context.Context, domain.Rule) error
	Get(context.Context, string) (domain.Rule, bool, error)
	ListByStatus(context.Context, domain.RuleStatus) ([]domain.Rule, error)
	ListActiveByLanguages(context.Context, []string) ([]domain.Rule, error)
	UpdateStatus(context.Context, domain.RuleStatusEvent) error
	RecordUsage(context.Context, domain.RuleUsage) error
	NextSequence(context.Context, string) (int, error)
}
type testCandidates interface {
	Upsert(context.Context, domain.CandidateCluster) (domain.CandidateCluster, error)
	Get(context.Context, string) (domain.CandidateCluster, bool, error)
	ListOpen(context.Context, string) ([]domain.CandidateCluster, error)
	ListMergedInto(context.Context, string) ([]domain.CandidateCluster, error)
	ListEvidence(context.Context, string) ([]domain.CandidateEvidence, error)
	AttachEvidence(context.Context, domain.CandidateEvidence) error
	Merge(context.Context, string, []string) error
	Promote(context.Context, string, string) error
}
type testClassifications interface {
	Get(context.Context, int64, string, string) (domain.Classification, bool, error)
	Save(context.Context, domain.Classification) error
}
type testRuns interface {
	Start(context.Context, domain.Run) (domain.Run, error)
	Finish(context.Context, domain.Run) error
}
type testSnapshots interface {
	Save(context.Context, domain.Snapshot) error
	Latest(context.Context) (domain.Snapshot, bool, error)
}
type testPublications interface {
	Save(context.Context, domain.Publication) error
	LastForRepository(context.Context, int64) (domain.Publication, bool, error)
}

func advanceCursor(ctx context.Context, cursors testCursors) func(domain.Cursor) error {
	return func(value domain.Cursor) error { return cursors.Advance(ctx, value) }
}

func replaceReactions(ctx context.Context, commentID int64, feedback testFeedback) func([]domain.Reaction) error {
	return func(value []domain.Reaction) error { return feedback.ReplaceForComment(ctx, commentID, value) }
}

func newFakeSyncStore() *fakeSyncStore {
	rules := fake.NewRuleCatalog()
	classifications := fake.NewClassificationHistory()
	return &fakeSyncStore{
		repositories: fake.NewRepositoryDirectory(), cursors: fake.NewCommentCursor(),
		comments: fake.NewCommentArchive(classifications, rules), feedback: fake.NewRuleFeedback(rules), rules: rules,
		candidates: fake.NewCandidateGroups(), classifications: classifications, runs: fake.NewRunJournal(),
		snapshots: fake.NewSnapshotArchive(), publications: fake.NewPublicationJournal(),
	}
}

func newFakeClassifyStore() *fakeClassifyStore {
	rules := fake.NewRuleCatalog()
	classifications := fake.NewClassificationHistory()
	return &fakeClassifyStore{
		comments: fake.NewCommentArchive(classifications, rules),
		feedback: fake.NewRuleFeedback(rules),
		rules:    rules, candidates: fake.NewCandidateGroups(), classifications: classifications,
	}
}

func classifyDeps(store *fakeClassifyStore) ClassifyAndPromoteDeps {
	return ClassifyAndPromoteDeps{
		Comments:        store.comments,
		Classifications: store.classifications,
		Candidates:      store.candidates,
		Reactions:       store.feedback,
		Rules:           store.rules,
	}
}

func newFakeMineStore() *fakeMineStore {
	rules := fake.NewRuleCatalog()
	return &fakeMineStore{
		repositories: fake.NewRepositoryDirectory(), cursors: fake.NewCommentCursor(),
		comments: fake.NewCommentArchive(fake.NewClassificationHistory(), rules), rules: rules,
	}
}

func newFakeReactionStore() *fakeReactionStore {
	rules := fake.NewRuleCatalog()
	classifications := fake.NewClassificationHistory()
	return &fakeReactionStore{
		repositories: fake.NewRepositoryDirectory(),
		comments:     fake.NewCommentArchive(classifications, rules),
		feedback:     fake.NewRuleFeedback(rules), rules: rules,
	}
}

func newFakeRulesStore() *fakeRulesStore {
	return &fakeRulesStore{rules: fake.NewRuleCatalog()}
}

func newFakeSnapshotStore() *fakeSnapshotStore {
	return &fakeSnapshotStore{rules: fake.NewRuleCatalog(), snapshots: fake.NewSnapshotArchive()}
}

func newFakeExportStore() *fakeExportStore {
	return &fakeExportStore{repositories: fake.NewRepositoryDirectory(), comments: fake.NewCommentArchive(fake.NewClassificationHistory(), fake.NewRuleCatalog())}
}

func newFakePublishStore() *fakePublishStore {
	return &fakePublishStore{
		repositories: fake.NewRepositoryDirectory(), rules: fake.NewRuleCatalog(),
		snapshots: fake.NewSnapshotArchive(), publications: fake.NewPublicationJournal(),
	}
}
