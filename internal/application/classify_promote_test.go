package application

import (
	"context"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/application/applicationtest"
	"github.com/lautaromei/ai-code-reviewer/internal/application/ports"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

type classifierCalls struct {
	classify int
	draft    int
}

type classifierFixture struct {
	verdict      domain.ClassificationVerdict
	candidateKey string
	calls        *classifierCalls
}

func newClassifier(verdict domain.ClassificationVerdict, candidateKey string) (*classifierFixture, *classifierCalls) {
	calls := &classifierCalls{}
	return &classifierFixture{verdict: verdict, candidateKey: candidateKey, calls: calls}, calls
}

func (classifier *classifierFixture) Classify(_ context.Context, input ports.ClassificationInput) (ports.ClassificationResult, error) {
	classifier.calls.classify++
	verdicts := make([]ports.CommentVerdict, 0, len(input.Comments))
	for _, comment := range input.Comments {
		verdicts = append(verdicts, applicationtest.CommentVerdict.
			WithCommentID(comment.CommentID).WithVerdict(classifier.verdict).
			WithCandidateKey(classifier.candidateKey).Bare())
	}
	return applicationtest.ClassificationResult.WithModel("test-model").WithVerdicts(verdicts).Bare(), nil
}

func (classifier *classifierFixture) Compact(context.Context, ports.CompactionInput) (ports.CompactionResult, error) {
	return applicationtest.CompactionResult.Bare(), nil
}

func (classifier *classifierFixture) DraftRule(context.Context, ports.DraftRuleInput) (ports.RuleDraft, error) {
	classifier.calls.draft++
	return applicationtest.RuleDraft.WithTitle("Group imports").WithScope("**/*.go").WithBody("Group imports by origin.").Bare(), nil
}

func classifyInput() ClassifyAndPromoteInput {
	return ClassifyAndPromoteInput{
		PromptVersion: "v1", MaxCommentsPerCall: 100, RulePrefix: "AIR", IDWidth: 3,
		Languages: []string{"go"}, LanguageCodes: map[string]string{"go": "GO"},
		Thresholds: PromotionThresholds{MinRepeatedComments: 3, MinThumbsUp: 2},
	}
}

type classificationScenario struct {
	store   *fakeClassifyStore
	summary ClassifyAndPromoteSummary
	calls   classifierCalls
}

func repeatedEvidenceScenario(t *testing.T) classificationScenario {
	t.Helper()
	ctx := context.Background()
	store := newFakeClassifyStore()
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	comments := applicationtest.Comment.With(applicationtest.WithHumanComment(1, "go", "hash", now)).Times(3)
	for i := range comments {
		comments[i].GitHubCommentID = int64(i + 1)
		comments[i].InputHash = "hash-" + string(rune('1'+i))
	}
	applicationtest.NewCommentArchiveSeeder(t, store.comments, ctx).Many(comments...)
	classifier, calls := newClassifier(domain.VerdictNew, "group-imports")
	summary, err := ClassifyAndPromote(ctx, classifier, classifyDeps(store), applicationtest.Clock{Time: now}, classifyInput())
	if err != nil {
		t.Fatal(err)
	}
	return classificationScenario{store: store, summary: summary, calls: *calls}
}

func skippedOriginScenario(t *testing.T) classificationScenario {
	t.Helper()
	ctx := context.Background()
	store := newFakeClassifyStore()
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	comment := applicationtest.Comment.With(applicationtest.WithSkippedComment(1, domain.OriginBotNoise, now)).Bare()
	applicationtest.NewCommentArchiveSeeder(t, store.comments, ctx).One(comment)
	classifier, calls := newClassifier(domain.VerdictNew, "ignored")
	summary, err := ClassifyAndPromote(ctx, classifier, classifyDeps(store), applicationtest.Clock{Time: now}, classifyInput())
	if err != nil {
		t.Fatal(err)
	}
	return classificationScenario{store: store, summary: summary, calls: *calls}
}

func coveredCommentScenario(t *testing.T) classificationScenario {
	t.Helper()
	ctx := context.Background()
	store := newFakeClassifyStore()
	now := time.Date(2026, 8, 8, 0, 0, 0, 0, time.UTC)
	comment := applicationtest.Comment.With(applicationtest.WithHumanComment(1, "go", "hash-covered", now)).Bare()
	applicationtest.NewCommentArchiveSeeder(t, store.comments, ctx).One(comment)
	applicationtest.NewRuleCatalogSeeder(t, store.rules, ctx).One(applicationtest.ActiveRule.WithCatalogRule(now).Bare())
	classifier, calls := newClassifier(domain.VerdictCovered, "")
	summary, err := ClassifyAndPromote(ctx, classifier, classifyDeps(store), applicationtest.Clock{Time: now}, classifyInput())
	if err != nil {
		t.Fatal(err)
	}
	return classificationScenario{store: store, summary: summary, calls: *calls}
}

func TestRepeatedEvidenceConversation(t *testing.T) {
	testigo.Run(t, "classifies every human comment", func(t *testing.T) {
		assert.Equal(t, repeatedEvidenceScenario(t).summary.CommentsClassified, 3)
	})

	testigo.Run(t, "promotes the next rule identity", func(t *testing.T) {
		assert.Equal(t, repeatedEvidenceScenario(t).summary.RulesPromoted, []string{"AIR-GO-001"})
	})

	testigo.Run(t, "does not report an unrelated merge", func(t *testing.T) {
		assert.Zero(t, repeatedEvidenceScenario(t).summary.CandidatesMerged)
	})

	testigo.Run(t, "classifies and drafts once", func(t *testing.T) {
		assert.Equal(t, repeatedEvidenceScenario(t).calls, classifierCalls{classify: 1, draft: 1})
	})

	testigo.Run(t, "persists the promoted rule as active", func(t *testing.T) {
		scenario := repeatedEvidenceScenario(t)
		rule, found, err := scenario.store.rules.Get(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.True(t, found, "rule should exist")
		assert.Equal(t, rule.Status, domain.RuleActive)
	})
}

func TestSkippedOriginConversation(t *testing.T) {
	testigo.Run(t, "does not classify bot noise", func(t *testing.T) {
		assert.Zero(t, skippedOriginScenario(t).summary.CommentsClassified)
	})

	testigo.Run(t, "does not call the classifier", func(t *testing.T) {
		assert.Equal(t, skippedOriginScenario(t).calls, classifierCalls{})
	})
}

func TestCoveredCommentConversation(t *testing.T) {
	testigo.Run(t, "classifies the comment", func(t *testing.T) {
		assert.Equal(t, coveredCommentScenario(t).summary.CommentsClassified, 1)
	})

	testigo.Run(t, "does not promote a rule", func(t *testing.T) {
		assert.Empty(t, coveredCommentScenario(t).summary.RulesPromoted)
	})

	testigo.Run(t, "does not draft a rule", func(t *testing.T) {
		assert.Equal(t, coveredCommentScenario(t).calls, classifierCalls{classify: 1})
	})
}
