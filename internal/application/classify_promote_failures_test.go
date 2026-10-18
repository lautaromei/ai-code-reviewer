package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

var collaboratorUnavailable = errors.New("collaborator unavailable")
var functionalTime = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

type classifierContract struct {
	classify       func(ports.ClassificationInput) (ports.ClassificationResult, error)
	compact        func(ports.CompactionInput) (ports.CompactionResult, error)
	draft          func(ports.DraftRuleInput) (ports.RuleDraft, error)
	classifyErr    error
	compactErr     error
	draftErr       error
	classification *ports.ClassificationResult
}

func (c classifierContract) Classify(_ context.Context, input ports.ClassificationInput) (ports.ClassificationResult, error) {
	if c.classify != nil {
		return c.classify(input)
	}
	if c.classifyErr != nil {
		return applicationtest.ClassificationResult.Bare(), c.classifyErr
	}
	if c.classification != nil {
		return *c.classification, nil
	}
	verdicts := make([]ports.CommentVerdict, 0, len(input.Comments))
	for _, comment := range input.Comments {
		verdicts = append(verdicts, applicationtest.CommentVerdict.WithCommentID(comment.CommentID).WithVerdict(domain.VerdictCovered).Bare())
	}
	return applicationtest.ClassificationResult.WithModel("functional-classifier").WithVerdicts(verdicts).Bare(), nil
}

func (c classifierContract) Compact(_ context.Context, input ports.CompactionInput) (ports.CompactionResult, error) {
	if c.compact != nil {
		return c.compact(input)
	}
	if c.compactErr != nil {
		return applicationtest.CompactionResult.Bare(), c.compactErr
	}
	return applicationtest.CompactionResult.Bare(), nil
}

func (c classifierContract) DraftRule(_ context.Context, input ports.DraftRuleInput) (ports.RuleDraft, error) {
	if c.draft != nil {
		return c.draft(input)
	}
	if c.draftErr != nil {
		return applicationtest.RuleDraft.Bare(), c.draftErr
	}
	return applicationtest.RuleDraft.WithTitle("Group imports").WithScope("**/*.go").WithBody("Group imports.").Bare(), nil
}

type candidateGroupsFault struct {
	testCandidates
	get             func(string) (domain.CandidateCluster, bool, error)
	listOpen        func(string) ([]domain.CandidateCluster, error)
	listMerged      func(string) ([]domain.CandidateCluster, error)
	listEvidence    func(string) ([]domain.CandidateEvidence, error)
	upsert          func(domain.CandidateCluster) (domain.CandidateCluster, error)
	attach          func(domain.CandidateEvidence) error
	merge           func(string, []string) error
	promote         func(string, string) error
	getErr          error
	listOpenErr     error
	listMergedErr   error
	listEvidenceErr error
	upsertErr       error
	attachErr       error
	mergeErr        error
	promoteErr      error
}

func (f candidateGroupsFault) Get(ctx context.Context, id string) (domain.CandidateCluster, bool, error) {
	if f.get != nil {
		return f.get(id)
	}
	if f.getErr != nil {
		return domain.CandidateCluster{}, false, f.getErr
	}
	return f.testCandidates.Get(ctx, id)
}
func (f candidateGroupsFault) ListOpen(ctx context.Context, language string) ([]domain.CandidateCluster, error) {
	if f.listOpen != nil {
		return f.listOpen(language)
	}
	if f.listOpenErr != nil {
		return nil, f.listOpenErr
	}
	return f.testCandidates.ListOpen(ctx, language)
}
func (f candidateGroupsFault) ListMergedInto(ctx context.Context, id string) ([]domain.CandidateCluster, error) {
	if f.listMerged != nil {
		return f.listMerged(id)
	}
	if f.listMergedErr != nil {
		return nil, f.listMergedErr
	}
	return f.testCandidates.ListMergedInto(ctx, id)
}
func (f candidateGroupsFault) ListEvidence(ctx context.Context, id string) ([]domain.CandidateEvidence, error) {
	if f.listEvidence != nil {
		return f.listEvidence(id)
	}
	if f.listEvidenceErr != nil {
		return nil, f.listEvidenceErr
	}
	return f.testCandidates.ListEvidence(ctx, id)
}
func (f candidateGroupsFault) Upsert(ctx context.Context, candidate domain.CandidateCluster) (domain.CandidateCluster, error) {
	if f.upsert != nil {
		return f.upsert(candidate)
	}
	if f.upsertErr != nil {
		return domain.CandidateCluster{}, f.upsertErr
	}
	return f.testCandidates.Upsert(ctx, candidate)
}
func (f candidateGroupsFault) AttachEvidence(ctx context.Context, evidence domain.CandidateEvidence) error {
	if f.attach != nil {
		return f.attach(evidence)
	}
	if f.attachErr != nil {
		return f.attachErr
	}
	return f.testCandidates.AttachEvidence(ctx, evidence)
}
func (f candidateGroupsFault) Merge(ctx context.Context, survivor string, merged []string) error {
	if f.merge != nil {
		return f.merge(survivor, merged)
	}
	if f.mergeErr != nil {
		return f.mergeErr
	}
	return f.testCandidates.Merge(ctx, survivor, merged)
}
func (f candidateGroupsFault) Promote(ctx context.Context, candidateID, ruleID string) error {
	if f.promote != nil {
		return f.promote(candidateID, ruleID)
	}
	if f.promoteErr != nil {
		return f.promoteErr
	}
	return f.testCandidates.Promote(ctx, candidateID, ruleID)
}

type commentsFault struct {
	testComments
	pending    func(string) ([]domain.Comment, error)
	get        func(int64) (domain.Comment, bool, error)
	pendingErr error
	getErr     error
}

func (f commentsFault) ListPendingClassification(ctx context.Context, version string) ([]domain.Comment, error) {
	if f.pending != nil {
		return f.pending(version)
	}
	if f.pendingErr != nil {
		return nil, f.pendingErr
	}
	return f.testComments.ListPendingClassification(ctx, version)
}
func (f commentsFault) GetByID(ctx context.Context, id int64) (domain.Comment, bool, error) {
	if f.get != nil {
		return f.get(id)
	}
	if f.getErr != nil {
		return domain.Comment{}, false, f.getErr
	}
	return f.testComments.GetByID(ctx, id)
}

type classificationsFault struct {
	testClassifications
	save    func(domain.Classification) error
	saveErr error
}

func (f classificationsFault) Save(ctx context.Context, classification domain.Classification) error {
	if f.save != nil {
		return f.save(classification)
	}
	if f.saveErr != nil {
		return f.saveErr
	}
	return f.testClassifications.Save(ctx, classification)
}

type feedbackFault struct {
	testFeedback
	count       func(string) (int, error)
	positiveErr error
	negativeErr error
}

func (f feedbackFault) CountByComment(ctx context.Context, id int64, content string) (int, error) {
	if f.count != nil {
		return f.count(content)
	}
	if content == "+1" && f.positiveErr != nil {
		return 0, f.positiveErr
	}
	if content == "-1" && f.negativeErr != nil {
		return 0, f.negativeErr
	}
	return f.testFeedback.CountByComment(ctx, id, content)
}

type rulesFault struct {
	testRules
	list        func(domain.RuleStatus) ([]domain.Rule, error)
	sequence    func(string) (int, error)
	get         func(string) (domain.Rule, bool, error)
	insert      func(domain.Rule) error
	listErr     error
	sequenceErr error
	getErr      error
	insertErr   error
}

func (f rulesFault) ListByStatus(ctx context.Context, status domain.RuleStatus) ([]domain.Rule, error) {
	if f.list != nil {
		return f.list(status)
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.testRules.ListByStatus(ctx, status)
}
func (f rulesFault) NextSequence(ctx context.Context, code string) (int, error) {
	if f.sequence != nil {
		return f.sequence(code)
	}
	if f.sequenceErr != nil {
		return 0, f.sequenceErr
	}
	return f.testRules.NextSequence(ctx, code)
}
func (f rulesFault) Get(ctx context.Context, id string) (domain.Rule, bool, error) {
	if f.get != nil {
		return f.get(id)
	}
	if f.getErr != nil {
		return domain.Rule{}, false, f.getErr
	}
	return f.testRules.Get(ctx, id)
}
func (f rulesFault) Insert(ctx context.Context, rule domain.Rule) error {
	if f.insert != nil {
		return f.insert(rule)
	}
	if f.insertErr != nil {
		return f.insertErr
	}
	return f.testRules.Insert(ctx, rule)
}

func classifyScenario(t *testing.T, origin domain.CommentOrigin, authorKind domain.AuthorKind) (*fakeClassifyStore, domain.Comment) {
	t.Helper()
	store := newFakeClassifyStore()
	comment := applicationtest.Comment.With(applicationtest.WithHumanComment(1, "go", "hash", functionalTime)).Bare()
	comment.Origin, comment.AuthorKind = origin, authorKind
	applicationtest.NewCommentArchiveSeeder(t, store.comments, context.Background()).One(comment)
	stored, found, err := store.comments.GetByGitHubID(context.Background(), 1, 1)
	if err != nil || !found {
		t.Fatalf("seed comment: found=%v err=%v", found, err)
	}
	return store, stored
}

func newVerdictClassifier(verdict domain.ClassificationVerdict, key string) classifierContract {
	return classifierContract{classify: func(input ports.ClassificationInput) (ports.ClassificationResult, error) {
		return applicationtest.ClassificationResult.WithModel("functional-classifier").WithVerdicts([]ports.CommentVerdict{
			applicationtest.CommentVerdict.WithCommentID(input.Comments[0].CommentID).WithVerdict(verdict).WithCandidateKey(key).Bare(),
		}).Bare(), nil
	}}
}

func runClassification(t *testing.T, classifier ports.Classifier, store *fakeClassifyStore, deps ClassifyAndPromoteDeps) (ClassifyAndPromoteSummary, error) {
	t.Helper()
	if deps.Comments == nil {
		deps = classifyDeps(store)
	}
	return ClassifyAndPromote(context.Background(), classifier, deps, applicationtest.Clock{Time: functionalTime}, classifyInput())
}

func TestClassificationReportsCollaboratorFailures(t *testing.T) {
	testigo.Run(t, "reports an unavailable classification queue", func(t *testing.T) {
		store := newFakeClassifyStore()
		deps := classifyDeps(store)
		deps.Comments = commentsFault{testComments: store.comments, pendingErr: collaboratorUnavailable}
		_, err := runClassification(t, classifierContract{}, store, deps)
		assert.ErrorContains(t, err, "list pending comments")
	})

	testigo.Run(t, "reports an unavailable rule catalogue", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		deps := classifyDeps(store)
		deps.Rules = rulesFault{testRules: store.rules, listErr: collaboratorUnavailable}
		_, err := runClassification(t, classifierContract{}, store, deps)
		assert.ErrorContains(t, err, "application: catalog")
	})

	testigo.Run(t, "reports unavailable open candidates", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		deps := classifyDeps(store)
		deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, listOpenErr: collaboratorUnavailable}
		_, err := runClassification(t, classifierContract{}, store, deps)
		assert.ErrorIs(t, err, collaboratorUnavailable)
	})

	testigo.Run(t, "reports an unavailable classifier", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		classifier := classifierContract{classifyErr: collaboratorUnavailable}
		_, err := runClassification(t, classifier, store, ClassifyAndPromoteDeps{})
		assert.ErrorContains(t, err, "application: classify: go")
	})

	testigo.Run(t, "rejects a response without a verdict", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		classifier := classifierContract{classification: &ports.ClassificationResult{}}
		_, err := runClassification(t, classifier, store, ClassifyAndPromoteDeps{})
		assert.ErrorContains(t, err, "has no verdict")
	})

	testigo.Run(t, "reports an unavailable classification history", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		deps := classifyDeps(store)
		deps.Classifications = classificationsFault{testClassifications: store.classifications, saveErr: collaboratorUnavailable}
		_, err := runClassification(t, classifierContract{}, store, deps)
		assert.ErrorContains(t, err, "save classification")
	})
}

func TestCandidateEvidenceReportsCollaboratorFailures(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		build func(*fakeClassifyStore) ClassifyAndPromoteDeps
		want  string
	}{
		{"requires a candidate key", func(store *fakeClassifyStore) ClassifyAndPromoteDeps { return classifyDeps(store) }, "has no candidate_key"},
		{"reports a candidate lookup failure", func(store *fakeClassifyStore) ClassifyAndPromoteDeps {
			deps := classifyDeps(store)
			deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, getErr: collaboratorUnavailable}
			return deps
		}, "look up candidate"},
		{"reports a candidate creation failure", func(store *fakeClassifyStore) ClassifyAndPromoteDeps {
			deps := classifyDeps(store)
			deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, upsertErr: collaboratorUnavailable}
			return deps
		}, "create candidate"},
		{"reports an evidence persistence failure", func(store *fakeClassifyStore) ClassifyAndPromoteDeps {
			deps := classifyDeps(store)
			deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, attachErr: collaboratorUnavailable}
			return deps
		}, "attach evidence"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
			key := "group-imports"
			if scenario.want == "has no candidate_key" {
				key = ""
			}
			_, err := runClassification(t, newVerdictClassifier(domain.VerdictNew, key), store, scenario.build(store))
			assert.ErrorContains(t, err, scenario.want)
		})
	}

	testigo.Run(t, "reports unavailable positive feedback", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginAICandidate, domain.AuthorAI)
		deps := classifyDeps(store)
		deps.Reactions = feedbackFault{testFeedback: store.feedback, positiveErr: collaboratorUnavailable}
		_, err := runClassification(t, newVerdictClassifier(domain.VerdictNew, "candidate"), store, deps)
		assert.ErrorContains(t, err, "contar +1")
	})

	testigo.Run(t, "reports unavailable negative feedback", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginAICandidate, domain.AuthorAI)
		deps := classifyDeps(store)
		deps.Reactions = feedbackFault{testFeedback: store.feedback, negativeErr: collaboratorUnavailable}
		_, err := runClassification(t, newVerdictClassifier(domain.VerdictNew, "candidate"), store, deps)
		assert.ErrorContains(t, err, "contar -1")
	})

	testigo.Run(t, "keeps evidence on the surviving candidate", func(t *testing.T) {
		store, comment := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		id := candidateID("go", "candidate")
		candidate := applicationtest.Candidate.WithID(id).WithLanguage("go").WithStatus(domain.CandidateMerged).WithMergedInto("survivor").Bare()
		_, err := store.candidates.Upsert(context.Background(), candidate)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runClassification(t, newVerdictClassifier(domain.VerdictNew, "candidate"), store, ClassifyAndPromoteDeps{})
		if err != nil {
			t.Fatal(err)
		}
		evidence, err := store.candidates.ListEvidence(context.Background(), "survivor")
		assert.NoError(t, err)
		assert.Equal(t, evidence[0].CommentID, comment.ID)
	})

	testigo.Run(t, "does not attach evidence to a closed candidate", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		id := candidateID("go", "candidate")
		candidate := applicationtest.Candidate.WithID(id).WithLanguage("go").WithStatus(domain.CandidatePromoted).Bare()
		_, err := store.candidates.Upsert(context.Background(), candidate)
		if err != nil {
			t.Fatal(err)
		}
		_, err = runClassification(t, newVerdictClassifier(domain.VerdictNew, "candidate"), store, ClassifyAndPromoteDeps{})
		assert.NoError(t, err)
		evidence, err := store.candidates.ListEvidence(context.Background(), id)
		assert.NoError(t, err)
		assert.Empty(t, evidence)
	})
}

func promotionScenario(t *testing.T) (*fakeClassifyStore, domain.CandidateCluster) {
	t.Helper()
	store, comment := classifyScenario(t, domain.OriginBotNoise, domain.AuthorHuman)
	candidate := applicationtest.Candidate.WithID("candidate").WithLanguage("go").WithLabel("candidate").WithStatus(domain.CandidateOpen).WithCreatedAt(functionalTime).WithUpdatedAt(functionalTime).Bare()
	if _, err := store.candidates.Upsert(context.Background(), candidate); err != nil {
		t.Fatal(err)
	}
	if err := store.candidates.AttachEvidence(context.Background(), applicationtest.CandidateEvidence.WithCandidateID(candidate.ID).WithCommentID(comment.ID).WithKind(domain.EvidenceHuman).WithEvaluatedAt(functionalTime).Bare()); err != nil {
		t.Fatal(err)
	}
	return store, candidate
}

func promotionInput() ClassifyAndPromoteInput {
	input := classifyInput()
	input.Thresholds = PromotionThresholds{MinRepeatedComments: 1}
	return input
}

func runPromotion(t *testing.T, classifier ports.Classifier, store *fakeClassifyStore, deps ClassifyAndPromoteDeps, input ClassifyAndPromoteInput) (ClassifyAndPromoteSummary, error) {
	t.Helper()
	if deps.Comments == nil {
		deps = classifyDeps(store)
	}
	return ClassifyAndPromote(context.Background(), classifier, deps, applicationtest.Clock{Time: functionalTime}, input)
}

func TestCompactionConversation(t *testing.T) {
	testigo.Run(t, "reports unavailable candidates before compaction", func(t *testing.T) {
		store := newFakeClassifyStore()
		deps := classifyDeps(store)
		deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, listOpenErr: collaboratorUnavailable}
		_, err := runPromotion(t, classifierContract{}, store, deps, classifyInput())
		assert.ErrorContains(t, err, "list candidates")
	})

	testigo.Run(t, "reports an unavailable compaction classifier", func(t *testing.T) {
		store := newFakeClassifyStore()
		for _, id := range []string{"one", "two"} {
			candidate := applicationtest.Candidate.WithID(id).WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
			_, err := store.candidates.Upsert(context.Background(), candidate)
			if err != nil {
				t.Fatal(err)
			}
		}
		classifier := classifierContract{compactErr: collaboratorUnavailable}
		_, err := runPromotion(t, classifier, store, ClassifyAndPromoteDeps{}, classifyInput())
		assert.ErrorContains(t, err, "application: compact: go")
	})

	testigo.Run(t, "reports a merge persistence failure", func(t *testing.T) {
		store := newFakeClassifyStore()
		for _, id := range []string{"one", "two"} {
			candidate := applicationtest.Candidate.WithID(id).WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
			_, err := store.candidates.Upsert(context.Background(), candidate)
			if err != nil {
				t.Fatal(err)
			}
		}
		deps := classifyDeps(store)
		deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, mergeErr: collaboratorUnavailable}
		classifier := classifierContract{compact: func(ports.CompactionInput) (ports.CompactionResult, error) {
			return applicationtest.CompactionResult.WithMerges([]ports.CompactionMerge{applicationtest.CompactionMerge.WithSurvivingCandidateID("one").WithMergedCandidateIDs([]string{"two"}).Bare()}).Bare(), nil
		}}
		_, err := runPromotion(t, classifier, store, deps, classifyInput())
		assert.ErrorContains(t, err, "fusionar")
	})

	testigo.Run(t, "counts every merged candidate", func(t *testing.T) {
		store := newFakeClassifyStore()
		for _, id := range []string{"one", "two", "three"} {
			candidate := applicationtest.Candidate.WithID(id).WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
			_, err := store.candidates.Upsert(context.Background(), candidate)
			if err != nil {
				t.Fatal(err)
			}
		}
		classifier := classifierContract{compact: func(ports.CompactionInput) (ports.CompactionResult, error) {
			return applicationtest.CompactionResult.WithMerges([]ports.CompactionMerge{applicationtest.CompactionMerge.WithSurvivingCandidateID("one").WithMergedCandidateIDs([]string{"two", "three"}).Bare()}).Bare(), nil
		}}
		summary, err := runPromotion(t, classifier, store, ClassifyAndPromoteDeps{}, classifyInput())
		assert.NoError(t, err)
		assert.Equal(t, summary.CandidatesMerged, 2)
	})
}

func TestPromotionReportsCollaboratorFailures(t *testing.T) {
	testigo.Run(t, "reports unavailable candidates before promotion", func(t *testing.T) {
		store := newFakeClassifyStore()
		calls := 0
		deps := classifyDeps(store)
		deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, listOpen: func(string) ([]domain.CandidateCluster, error) {
			calls++
			if calls == 2 {
				return nil, collaboratorUnavailable
			}
			return nil, nil
		}}
		_, err := runPromotion(t, classifierContract{}, store, deps, classifyInput())
		assert.ErrorContains(t, err, "application: promote: list candidates")
	})

	for _, scenario := range []struct {
		name  string
		build func(*fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput)
		want  string
	}{
		{"requires a language code", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			input := promotionInput()
			input.LanguageCodes = nil
			return classifyDeps(store), classifierContract{}, input
		}, "has no code configured"},
		{"reports unavailable evidence", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			deps := classifyDeps(store)
			deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, listEvidenceErr: collaboratorUnavailable}
			return deps, classifierContract{}, promotionInput()
		}, "list evidence"},
		{"reports unavailable merged candidates", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			deps := classifyDeps(store)
			deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, listMergedErr: collaboratorUnavailable}
			return deps, classifierContract{}, promotionInput()
		}, "list candidates merged"},
		{"reports an unavailable evidence comment", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			deps := classifyDeps(store)
			deps.Comments = commentsFault{testComments: store.comments, getErr: collaboratorUnavailable}
			return deps, classifierContract{}, promotionInput()
		}, "load comment"},
		{"reports a drafting failure", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			classifier := classifierContract{draftErr: collaboratorUnavailable}
			return classifyDeps(store), classifier, promotionInput()
		}, "draft rule"},
		{"reports an unavailable rule sequence", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			deps := classifyDeps(store)
			deps.Rules = rulesFault{testRules: store.rules, sequenceErr: collaboratorUnavailable}
			return deps, classifierContract{}, promotionInput()
		}, "siguiente secuencia"},
		{"reports an unavailable identity lookup", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			deps := classifyDeps(store)
			deps.Rules = rulesFault{testRules: store.rules, getErr: collaboratorUnavailable}
			return deps, classifierContract{}, promotionInput()
		}, "check ID"},
		{"reports an unavailable rule insertion", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			deps := classifyDeps(store)
			deps.Rules = rulesFault{testRules: store.rules, insertErr: collaboratorUnavailable}
			return deps, classifierContract{}, promotionInput()
		}, "insert"},
		{"reports an unavailable candidate promotion", func(store *fakeClassifyStore) (ClassifyAndPromoteDeps, ports.Classifier, ClassifyAndPromoteInput) {
			deps := classifyDeps(store)
			deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, promoteErr: collaboratorUnavailable}
			return deps, classifierContract{}, promotionInput()
		}, "mark"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			store, _ := promotionScenario(t)
			deps, classifier, input := scenario.build(store)
			_, err := runPromotion(t, classifier, store, deps, input)
			assert.ErrorContains(t, err, scenario.want)
		})
	}
}

func TestPromotionUsesAvailableEvidence(t *testing.T) {
	testigo.Run(t, "reports unavailable evidence from a merged candidate", func(t *testing.T) {
		store, _ := promotionScenario(t)
		child := applicationtest.Candidate.WithID("child").WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
		if _, err := store.candidates.Upsert(context.Background(), child); err != nil {
			t.Fatal(err)
		}
		if err := store.candidates.Merge(context.Background(), "candidate", []string{child.ID}); err != nil {
			t.Fatal(err)
		}
		deps := classifyDeps(store)
		deps.Candidates = candidateGroupsFault{testCandidates: store.candidates, listEvidence: func(id string) ([]domain.CandidateEvidence, error) {
			if id == child.ID {
				return nil, collaboratorUnavailable
			}
			return store.candidates.ListEvidence(context.Background(), id)
		}}
		_, err := runPromotion(t, classifierContract{}, store, deps, promotionInput())
		assert.ErrorContains(t, err, "list evidence for \"child\"")
	})

	testigo.Run(t, "skips evidence whose comment disappeared", func(t *testing.T) {
		store := newFakeClassifyStore()
		candidate := applicationtest.Candidate.WithID("candidate").WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
		if _, err := store.candidates.Upsert(context.Background(), candidate); err != nil {
			t.Fatal(err)
		}
		if err := store.candidates.AttachEvidence(context.Background(), applicationtest.CandidateEvidence.WithCandidateID(candidate.ID).WithCommentID(999).WithKind(domain.EvidenceHuman).Bare()); err != nil {
			t.Fatal(err)
		}
		var evidenceCount int
		classifier := classifierContract{draft: func(input ports.DraftRuleInput) (ports.RuleDraft, error) {
			evidenceCount = len(input.Evidence)
			return applicationtest.RuleDraft.WithBody("rule").Bare(), nil
		}}
		_, err := runPromotion(t, classifier, store, ClassifyAndPromoteDeps{}, promotionInput())
		assert.NoError(t, err)
		assert.Zero(t, evidenceCount)
	})

	testigo.Run(t, "uses evidence from merged candidates", func(t *testing.T) {
		store, comment := classifyScenario(t, domain.OriginBotNoise, domain.AuthorHuman)
		primary := applicationtest.Candidate.WithID("primary").WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
		child := applicationtest.Candidate.WithID("child").WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
		for _, candidate := range []domain.CandidateCluster{primary, child} {
			if _, err := store.candidates.Upsert(context.Background(), candidate); err != nil {
				t.Fatal(err)
			}
		}
		if err := store.candidates.Merge(context.Background(), primary.ID, []string{child.ID}); err != nil {
			t.Fatal(err)
		}
		if err := store.candidates.AttachEvidence(context.Background(), applicationtest.CandidateEvidence.WithCandidateID(child.ID).WithCommentID(comment.ID).WithKind(domain.EvidenceHuman).Bare()); err != nil {
			t.Fatal(err)
		}
		summary, err := runPromotion(t, classifierContract{}, store, ClassifyAndPromoteDeps{}, promotionInput())
		assert.NoError(t, err)
		assert.Equal(t, summary.RulesPromoted, []string{"AIR-GO-001"})
	})

	testigo.Run(t, "skips a candidate below its threshold", func(t *testing.T) {
		store, _ := promotionScenario(t)
		input := promotionInput()
		input.Thresholds.MinRepeatedComments = 2
		summary, err := runPromotion(t, classifierContract{}, store, ClassifyAndPromoteDeps{}, input)
		assert.NoError(t, err)
		assert.Empty(t, summary.RulesPromoted)
	})

	testigo.Run(t, "skips an occupied rule identity", func(t *testing.T) {
		store, _ := promotionScenario(t)
		occupied := applicationtest.ActiveRule.WithRuleID("AIR-GO-001").Bare()
		if err := store.rules.Insert(context.Background(), occupied); err != nil {
			t.Fatal(err)
		}
		summary, err := runPromotion(t, classifierContract{}, store, ClassifyAndPromoteDeps{}, promotionInput())
		assert.NoError(t, err)
		assert.Equal(t, summary.RulesPromoted, []string{"AIR-GO-002"})
	})
}

func TestClassificationBatching(t *testing.T) {
	testigo.Run(t, "returns no batches without comments", func(t *testing.T) {
		assert.Nil(t, chunkComments(nil, 2))
	})

	testigo.Run(t, "keeps all comments when batch size is disabled", func(t *testing.T) {
		comments := []domain.Comment{applicationtest.Comment.WithID(1).Bare(), applicationtest.Comment.WithID(2).Bare()}
		assert.Equal(t, chunkComments(comments, 0), [][]domain.Comment{comments})
	})

	testigo.Run(t, "splits comments into exact batches", func(t *testing.T) {
		comments := []domain.Comment{applicationtest.Comment.WithID(1).Bare(), applicationtest.Comment.WithID(2).Bare()}
		assert.Equal(t, chunkComments(comments, 1), [][]domain.Comment{{comments[0]}, {comments[1]}})
	})
}

func TestClassificationCatalogueReportsLaterStatusFailure(t *testing.T) {
	testigo.Run(t, "reports failure after reading active rules", func(t *testing.T) {
		store, _ := classifyScenario(t, domain.OriginHuman, domain.AuthorHuman)
		calls := 0
		deps := classifyDeps(store)
		deps.Rules = rulesFault{testRules: store.rules, list: func(domain.RuleStatus) ([]domain.Rule, error) {
			calls++
			if calls == 2 {
				return nil, collaboratorUnavailable
			}
			return []domain.Rule{{RuleID: "AIR-PHP-001", Language: "php", Status: domain.RuleActive}}, nil
		}}
		_, err := runClassification(t, classifierContract{}, store, deps)
		assert.ErrorContains(t, err, "list disabled rules")
	})
}
