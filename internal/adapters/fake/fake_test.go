package fake

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

var contractNow = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

func storedComment(t *testing.T, archive *commentRepository, repositoryID, githubID int64) domain.Comment {
	t.Helper()
	stored, found, err := archive.GetByGitHubID(context.Background(), repositoryID, githubID)
	if err != nil || !found {
		t.Fatalf("stored comment %d/%d: found=%v err=%v", repositoryID, githubID, found, err)
	}
	return stored
}

func mustRecordUsage(t *testing.T, catalog *ruleRepository, usage domain.RuleUsage) {
	t.Helper()
	if err := catalog.RecordUsage(context.Background(), usage); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineGitHubContract(t *testing.T) {
	testigo.Run(t, "keeps the comment cursor when no comments are available", func(t *testing.T) {
		cursor := ports.Cursor{LastCommentID: 7}
		comments, next, err := NewGitHub().ListReviewComments(context.Background(), "owner/repo", cursor)
		assert.NoError(t, err)
		assert.Empty(t, comments)
		assert.Equal(t, next, cursor)
	})

	testigo.Run(t, "returns an empty reaction page", func(t *testing.T) {
		page, err := NewGitHub().ListReactions(context.Background(), "owner/repo", 1, "etag")
		assert.NoError(t, err)
		assert.Equal(t, page, ports.ReactionPage{})
	})

	testigo.Run(t, "returns no repository languages", func(t *testing.T) {
		languages, err := NewGitHub().RepositoryLanguages(context.Background(), "owner/repo")
		assert.NoError(t, err)
		assert.Empty(t, languages)
	})

	testigo.Run(t, "uses main as the default branch", func(t *testing.T) {
		branch, err := NewGitHub().DefaultBranch(context.Background(), "owner/repo")
		assert.NoError(t, err)
		assert.Equal(t, branch, "main")
	})

	testigo.Run(t, "returns no remote file content", func(t *testing.T) {
		content, err := NewGitHub().ReadFile(context.Background(), "owner/repo", "docs/rules.md", "main")
		assert.NoError(t, err)
		assert.Empty(t, content)
	})

	testigo.Run(t, "reports an already current publication", func(t *testing.T) {
		publication, err := NewGitHub().UpsertRulesPullRequest(context.Background(), ports.PublicationRequest{})
		assert.NoError(t, err)
		assert.Equal(t, publication.Status, domain.PublicationAlreadyCurrent)
	})
}

func TestOfflineClassifierContract(t *testing.T) {
	testigo.Run(t, "returns an empty classification", func(t *testing.T) {
		classification, err := NewClassifier().Classify(context.Background(), ports.ClassificationInput{})
		assert.NoError(t, err)
		assert.Equal(t, classification, ports.ClassificationResult{})
	})

	testigo.Run(t, "returns an empty compaction", func(t *testing.T) {
		compaction, err := NewClassifier().Compact(context.Background(), ports.CompactionInput{})
		assert.NoError(t, err)
		assert.Equal(t, compaction, ports.CompactionResult{})
	})

	testigo.Run(t, "returns an empty draft", func(t *testing.T) {
		draft, err := NewClassifier().DraftRule(context.Background(), ports.DraftRuleInput{})
		assert.NoError(t, err)
		assert.Equal(t, draft, ports.RuleDraft{})
	})
}

func TestRepositoryDirectoryContract(t *testing.T) {
	testigo.Run(t, "reports an unknown repository as absent", func(t *testing.T) {
		_, found, err := NewRepositoryDirectory().GetByFullName(context.Background(), "owner/missing")
		assert.NoError(t, err)
		assert.False(t, found, "repository should be absent")
	})

	testigo.Run(t, "preserves identity across upserts", func(t *testing.T) {
		directory := NewRepositoryDirectory()
		created, err := directory.Upsert(context.Background(), domain.Repository{FullName: "owner/repo"})
		if err != nil {
			t.Fatal(err)
		}
		updated, err := directory.Upsert(context.Background(), domain.Repository{FullName: "owner/repo"})
		assert.NoError(t, err)
		assert.Equal(t, updated.ID, created.ID)
	})

	testigo.Run(t, "returns the most recently stored repository", func(t *testing.T) {
		directory := NewRepositoryDirectory()
		_, err := directory.Upsert(context.Background(), domain.Repository{FullName: "owner/repo", Languages: []string{"go"}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = directory.Upsert(context.Background(), domain.Repository{FullName: "owner/repo", Languages: []string{"php"}})
		if err != nil {
			t.Fatal(err)
		}
		stored, _, err := directory.GetByFullName(context.Background(), "owner/repo")
		assert.NoError(t, err)
		assert.Equal(t, stored.Languages, []string{"php"})
	})

	testigo.Run(t, "lists each repository once", func(t *testing.T) {
		directory := NewRepositoryDirectory()
		_, err := directory.Upsert(context.Background(), domain.Repository{FullName: "owner/repo"})
		if err != nil {
			t.Fatal(err)
		}
		repositories, err := directory.List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, repositories, 1)
	})
}

func TestCommentCursorContract(t *testing.T) {
	testigo.Run(t, "reports a cursor as absent before advance", func(t *testing.T) {
		_, found, err := NewCommentCursor().Get(context.Background(), 4, "comments")
		assert.NoError(t, err)
		assert.False(t, found, "cursor should be absent")
	})

	testigo.Run(t, "returns the advanced cursor", func(t *testing.T) {
		cursors := NewCommentCursor()
		want := domain.Cursor{RepositoryID: 4, Stream: "comments", LastCommentID: 9}
		if err := cursors.Advance(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		got, _, err := cursors.Get(context.Background(), want.RepositoryID, want.Stream)
		assert.NoError(t, err)
		assert.Equal(t, got, want)
	})
}

type commentArchiveScenario struct {
	archive     *commentRepository
	rules       *ruleRepository
	human       domain.Comment
	aiCandidate domain.Comment
	aiUsage     domain.Comment
}

func newCommentArchiveScenario(t *testing.T) commentArchiveScenario {
	t.Helper()
	rules := NewRuleCatalog()
	archive := NewCommentArchive(NewClassificationHistory(), rules)
	applicationtest.NewCommentArchiveSeeder(t, archive, context.Background()).Many(
		applicationtest.Comment.WithRepositoryID(2).WithGitHubCommentID(2).WithOrigin(domain.OriginHuman).WithInputHash("human").WithCreatedAt(contractNow).Bare(),
		applicationtest.Comment.WithRepositoryID(1).WithGitHubCommentID(3).WithOrigin(domain.OriginAICandidate).WithInputHash("candidate").WithCreatedAt(contractNow.Add(time.Hour)).Bare(),
		applicationtest.Comment.WithRepositoryID(1).WithGitHubCommentID(1).WithOrigin(domain.OriginAIRuleUsage).WithInputHash("usage").WithCreatedAt(contractNow).Bare(),
		applicationtest.Comment.WithRepositoryID(1).WithGitHubCommentID(2).WithOrigin(domain.OriginBotNoise).WithInputHash("noise").WithCreatedAt(contractNow).Bare(),
	)
	human := storedComment(t, archive, 2, 2)
	aiCandidate := storedComment(t, archive, 1, 3)
	aiUsage := storedComment(t, archive, 1, 1)
	return commentArchiveScenario{archive: archive, rules: rules, human: human, aiCandidate: aiCandidate, aiUsage: aiUsage}
}

func TestCommentArchiveContract(t *testing.T) {
	testigo.Run(t, "preserves local identity when a GitHub comment changes", func(t *testing.T) {
		scenario := newCommentArchiveScenario(t)
		updated := scenario.human
		updated.Body = "updated"
		applicationtest.NewCommentArchiveSeeder(t, scenario.archive, context.Background()).One(updated)
		stored := storedComment(t, scenario.archive, updated.RepositoryID, updated.GitHubCommentID)
		assert.Equal(t, stored.ID, scenario.human.ID)
	})

	testigo.Run(t, "returns the latest comment body", func(t *testing.T) {
		scenario := newCommentArchiveScenario(t)
		updated := scenario.human
		updated.Body = "updated"
		applicationtest.NewCommentArchiveSeeder(t, scenario.archive, context.Background()).One(updated)
		stored, _, err := scenario.archive.GetByID(context.Background(), scenario.human.ID)
		assert.NoError(t, err)
		assert.Equal(t, stored.Body, "updated")
	})

	testigo.Run(t, "reports an unknown local identity as absent", func(t *testing.T) {
		_, found, err := newCommentArchiveScenario(t).archive.GetByID(context.Background(), 999)
		assert.NoError(t, err)
		assert.False(t, found, "comment should be absent")
	})

	testigo.Run(t, "reports an unknown GitHub identity as absent", func(t *testing.T) {
		_, found, err := newCommentArchiveScenario(t).archive.GetByGitHubID(context.Background(), 2, 999)
		assert.NoError(t, err)
		assert.False(t, found, "comment should be absent")
	})

	testigo.Run(t, "returns a comment by GitHub identity", func(t *testing.T) {
		scenario := newCommentArchiveScenario(t)
		stored, _, err := scenario.archive.GetByGitHubID(context.Background(), 1, 3)
		assert.NoError(t, err)
		assert.Equal(t, stored.ID, scenario.aiCandidate.ID)
	})

	testigo.Run(t, "lists every unclassified comment", func(t *testing.T) {
		pending, err := newCommentArchiveScenario(t).archive.ListPendingClassification(context.Background(), "v1")
		assert.NoError(t, err)
		assert.Len(t, pending, 4)
	})

	testigo.Run(t, "excludes a classification with the current input", func(t *testing.T) {
		classifications := NewClassificationHistory()
		archive := NewCommentArchive(classifications, NewRuleCatalog())
		applicationtest.NewCommentArchiveSeeder(t, archive, context.Background()).One(
			applicationtest.Comment.WithRepositoryID(1).WithGitHubCommentID(1).WithInputHash("hash").Bare(),
		)
		comment := storedComment(t, archive, 1, 1)
		if err := classifications.Save(context.Background(), domain.Classification{CommentID: comment.ID, InputHash: "hash", PromptVersion: "v1"}); err != nil {
			t.Fatal(err)
		}
		pending, err := archive.ListPendingClassification(context.Background(), "v1")
		assert.NoError(t, err)
		assert.Empty(t, pending)
	})

	testigo.Run(t, "lists tracked AI comments in local identity order", func(t *testing.T) {
		scenario := newCommentArchiveScenario(t)
		tracked, err := scenario.archive.ListTrackedAI(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, tracked, []domain.Comment{scenario.aiCandidate, scenario.aiUsage})
	})

	testigo.Run(t, "lists comments that used a rule", func(t *testing.T) {
		scenario := newCommentArchiveScenario(t)
		mustRecordUsage(t, scenario.rules, domain.RuleUsage{RuleID: "AIR-GO-001", CommentID: scenario.aiUsage.ID, UsedAt: contractNow})
		mustRecordUsage(t, scenario.rules, domain.RuleUsage{RuleID: "AIR-GO-001", CommentID: scenario.aiCandidate.ID, UsedAt: contractNow})
		comments, err := scenario.archive.ListForRule(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.Equal(t, comments, []domain.Comment{scenario.aiCandidate, scenario.aiUsage})
	})

	testigo.Run(t, "orders comments by repository date and GitHub identity", func(t *testing.T) {
		comments, err := newCommentArchiveScenario(t).archive.ListAllOrdered(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []int64{comments[0].GitHubCommentID, comments[1].GitHubCommentID, comments[2].GitHubCommentID, comments[3].GitHubCommentID}, []int64{1, 2, 3, 2})
	})
}

type failingClassificationReader struct{ err error }

func (f failingClassificationReader) Get(context.Context, int64, string, string) (domain.Classification, bool, error) {
	return domain.Classification{}, false, f.err
}

func TestCommentArchiveReportsClassificationFailure(t *testing.T) {
	testigo.Run(t, "returns the classification failure", func(t *testing.T) {
		wantErr := errors.New("classification unavailable")
		archive := NewCommentArchive(failingClassificationReader{err: wantErr}, NewRuleCatalog())
		applicationtest.NewCommentArchiveSeeder(t, archive, context.Background()).One(
			applicationtest.Comment.WithRepositoryID(1).WithGitHubCommentID(1).Bare(),
		)
		_, err := archive.ListPendingClassification(context.Background(), "v1")
		assert.ErrorIs(t, err, wantErr)
	})
}

func newFeedbackScenario(t *testing.T) (*reactionRepository, *ruleRepository) {
	t.Helper()
	rules := NewRuleCatalog()
	feedback := NewRuleFeedback(rules)
	mustRecordUsage(t, rules, domain.RuleUsage{RuleID: "AIR-GO-001", CommentID: 8, UsedAt: contractNow})
	if err := feedback.ReplaceForComment(context.Background(), 8, []domain.Reaction{{Content: "+1"}, {Content: "+1"}, {Content: "-1"}}); err != nil {
		t.Fatal(err)
	}
	return feedback, rules
}

func TestRuleFeedbackContract(t *testing.T) {
	testigo.Run(t, "counts matching reactions for one comment", func(t *testing.T) {
		feedback, _ := newFeedbackScenario(t)
		count, err := feedback.CountByComment(context.Background(), 8, "+1")
		assert.NoError(t, err)
		assert.Equal(t, count, 2)
	})

	testigo.Run(t, "counts matching reactions for a rule usage", func(t *testing.T) {
		feedback, _ := newFeedbackScenario(t)
		count, err := feedback.CountByRule(context.Background(), "AIR-GO-001", "+1")
		assert.NoError(t, err)
		assert.Equal(t, count, 2)
	})
}

var catalogActiveRule = applicationtest.ActiveRule.
	WithRuleID("AIR-GO-001").
	WithLanguage("go").
	WithStatus(domain.RuleActive).
	Bare()

func newRuleCatalogScenario(t *testing.T) *ruleRepository {
	t.Helper()
	catalog := NewRuleCatalog()
	applicationtest.NewRuleCatalogSeeder(t, catalog, context.Background()).Many(
		catalogActiveRule,
		applicationtest.ActiveRule.WithRuleID("AIR-PHP-001").WithLanguage("php").WithStatus(domain.RuleDisabled).Bare(),
	)
	return catalog
}

func TestRuleCatalogContract(t *testing.T) {
	testigo.Run(t, "reports an unknown rule as absent", func(t *testing.T) {
		_, found, err := newRuleCatalogScenario(t).Get(context.Background(), "missing")
		assert.NoError(t, err)
		assert.False(t, found, "rule should be absent")
	})

	testigo.Run(t, "returns a rule by identity", func(t *testing.T) {
		got, _, err := newRuleCatalogScenario(t).Get(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.Equal(t, got, catalogActiveRule)
	})

	testigo.Run(t, "lists rules in the requested status", func(t *testing.T) {
		got, err := newRuleCatalogScenario(t).ListByStatus(context.Background(), domain.RuleActive)
		assert.NoError(t, err)
		assert.Equal(t, got, []domain.Rule{catalogActiveRule})
	})

	testigo.Run(t, "lists active rules in requested languages", func(t *testing.T) {
		got, err := newRuleCatalogScenario(t).ListActiveByLanguages(context.Background(), []string{"go", "php"})
		assert.NoError(t, err)
		assert.Equal(t, got, []domain.Rule{catalogActiveRule})
	})

	testigo.Run(t, "records a disabled status", func(t *testing.T) {
		catalog := newRuleCatalogScenario(t)
		event := applicationtest.RuleStatusEvent.WithRuleID("AIR-GO-001").WithToState(domain.RuleDisabled).WithReason(domain.DisabledManual).WithOccurredAt(contractNow).Bare()
		if err := catalog.UpdateStatus(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		got, _, err := catalog.Get(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.Equal(t, got.Status, domain.RuleDisabled)
	})

	testigo.Run(t, "records when a rule was disabled", func(t *testing.T) {
		catalog := newRuleCatalogScenario(t)
		event := applicationtest.RuleStatusEvent.WithRuleID("AIR-GO-001").WithToState(domain.RuleDisabled).WithOccurredAt(contractNow).Bare()
		if err := catalog.UpdateStatus(context.Background(), event); err != nil {
			t.Fatal(err)
		}
		got, _, err := catalog.Get(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.Equal(t, *got.DisabledAt, contractNow)
	})

	testigo.Run(t, "reactivating clears the disabled reason", func(t *testing.T) {
		catalog := newRuleCatalogScenario(t)
		disabled := applicationtest.RuleStatusEvent.WithRuleID("AIR-GO-001").WithToState(domain.RuleDisabled).WithReason(domain.DisabledManual).WithOccurredAt(contractNow).Bare()
		if err := catalog.UpdateStatus(context.Background(), disabled); err != nil {
			t.Fatal(err)
		}
		active := applicationtest.RuleStatusEvent.WithRuleID("AIR-GO-001").WithToState(domain.RuleActive).WithOccurredAt(contractNow.Add(time.Hour)).Bare()
		if err := catalog.UpdateStatus(context.Background(), active); err != nil {
			t.Fatal(err)
		}
		got, _, err := catalog.Get(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.Empty(t, got.DisabledReason)
	})

	testigo.Run(t, "reactivating clears the disabled time", func(t *testing.T) {
		catalog := newRuleCatalogScenario(t)
		disabled := applicationtest.RuleStatusEvent.WithRuleID("AIR-GO-001").WithToState(domain.RuleDisabled).WithOccurredAt(contractNow).Bare()
		if err := catalog.UpdateStatus(context.Background(), disabled); err != nil {
			t.Fatal(err)
		}
		active := applicationtest.RuleStatusEvent.WithRuleID("AIR-GO-001").WithToState(domain.RuleActive).WithOccurredAt(contractNow.Add(time.Hour)).Bare()
		if err := catalog.UpdateStatus(context.Background(), active); err != nil {
			t.Fatal(err)
		}
		got, _, err := catalog.Get(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.Nil(t, got.DisabledAt)
	})

	testigo.Run(t, "keeps the latest usage time", func(t *testing.T) {
		catalog := newRuleCatalogScenario(t)
		mustRecordUsage(t, catalog, domain.RuleUsage{RuleID: "AIR-GO-001", CommentID: 1, UsedAt: contractNow})
		mustRecordUsage(t, catalog, domain.RuleUsage{RuleID: "AIR-GO-001", CommentID: 1, UsedAt: contractNow.Add(-time.Hour)})
		got, _, err := catalog.Get(context.Background(), "AIR-GO-001")
		assert.NoError(t, err)
		assert.Equal(t, *got.LastUsedAt, contractNow)
	})

	testigo.Run(t, "increments each language sequence", func(t *testing.T) {
		catalog := NewRuleCatalog()
		_, err := catalog.NextSequence(context.Background(), "GO")
		if err != nil {
			t.Fatal(err)
		}
		second, err := catalog.NextSequence(context.Background(), "GO")
		assert.NoError(t, err)
		assert.Equal(t, second, 2)
	})
}

func newCandidateScenario(t *testing.T) (*candidateRepository, domain.CandidateCluster, domain.CandidateCluster) {
	t.Helper()
	candidates := NewCandidateGroups()
	primary := applicationtest.Candidate.WithID("primary").WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
	secondary := applicationtest.Candidate.WithID("secondary").WithLanguage("go").WithStatus(domain.CandidateOpen).Bare()
	if _, err := candidates.Upsert(context.Background(), primary); err != nil {
		t.Fatal(err)
	}
	if _, err := candidates.Upsert(context.Background(), secondary); err != nil {
		t.Fatal(err)
	}
	return candidates, primary, secondary
}

func TestCandidateGroupsContract(t *testing.T) {
	testigo.Run(t, "reports an unknown candidate as absent", func(t *testing.T) {
		candidates, _, _ := newCandidateScenario(t)
		_, found, err := candidates.Get(context.Background(), "missing")
		assert.NoError(t, err)
		assert.False(t, found, "candidate should be absent")
	})

	testigo.Run(t, "lists open candidates for one language", func(t *testing.T) {
		candidates, primary, secondary := newCandidateScenario(t)
		open, err := candidates.ListOpen(context.Background(), "go")
		assert.NoError(t, err)
		assert.Equal(t, open, []domain.CandidateCluster{primary, secondary})
	})

	testigo.Run(t, "lists attached evidence", func(t *testing.T) {
		candidates, primary, _ := newCandidateScenario(t)
		want := domain.CandidateEvidence{CandidateID: primary.ID, CommentID: 1, Kind: domain.EvidenceHuman}
		if err := candidates.AttachEvidence(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		got, err := candidates.ListEvidence(context.Background(), primary.ID)
		assert.NoError(t, err)
		assert.Equal(t, got, []domain.CandidateEvidence{want})
	})

	testigo.Run(t, "lists candidates merged into the survivor", func(t *testing.T) {
		candidates, primary, secondary := newCandidateScenario(t)
		if err := candidates.Merge(context.Background(), primary.ID, []string{secondary.ID, "missing"}); err != nil {
			t.Fatal(err)
		}
		merged, err := candidates.ListMergedInto(context.Background(), primary.ID)
		assert.NoError(t, err)
		assert.Equal(t, merged[0].ID, secondary.ID)
	})

	testigo.Run(t, "records the promoted rule", func(t *testing.T) {
		candidates, primary, _ := newCandidateScenario(t)
		if err := candidates.Promote(context.Background(), primary.ID, "AIR-GO-001"); err != nil {
			t.Fatal(err)
		}
		if err := candidates.Promote(context.Background(), "missing", "AIR-GO-999"); err != nil {
			t.Fatal(err)
		}
		got, _, err := candidates.Get(context.Background(), primary.ID)
		assert.NoError(t, err)
		assert.Equal(t, got.PromotedRuleID, "AIR-GO-001")
	})
}

func TestClassificationHistoryContract(t *testing.T) {
	testigo.Run(t, "reports an unknown classification as absent", func(t *testing.T) {
		_, found, err := NewClassificationHistory().Get(context.Background(), 1, "hash", "v1")
		assert.NoError(t, err)
		assert.False(t, found, "classification should be absent")
	})

	testigo.Run(t, "returns a saved classification", func(t *testing.T) {
		history := NewClassificationHistory()
		want := domain.Classification{CommentID: 1, InputHash: "hash", PromptVersion: "v1", Verdict: domain.VerdictNew}
		if err := history.Save(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		got, _, err := history.Get(context.Background(), 1, "hash", "v1")
		assert.NoError(t, err)
		assert.Equal(t, got, want)
	})
}

func TestSnapshotArchiveContract(t *testing.T) {
	testigo.Run(t, "reports no snapshot before save", func(t *testing.T) {
		_, found, err := NewSnapshotArchive().Latest(context.Background())
		assert.NoError(t, err)
		assert.False(t, found, "snapshot should be absent")
	})

	testigo.Run(t, "returns the latest saved snapshot", func(t *testing.T) {
		archive := NewSnapshotArchive()
		want := domain.Snapshot{Hash: "snapshot-hash"}
		if err := archive.Save(context.Background(), want); err != nil {
			t.Fatal(err)
		}
		got, _, err := archive.Latest(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, got, want)
	})
}

func TestPublicationJournalContract(t *testing.T) {
	testigo.Run(t, "reports no publication before save", func(t *testing.T) {
		_, found, err := NewPublicationJournal().LastForRepository(context.Background(), 4)
		assert.NoError(t, err)
		assert.False(t, found, "publication should be absent")
	})

	testigo.Run(t, "assigns an identity before returning a publication", func(t *testing.T) {
		journal := NewPublicationJournal()
		if err := journal.Save(context.Background(), domain.Publication{RepositoryID: 4, Status: domain.PublicationCreated}); err != nil {
			t.Fatal(err)
		}
		got, _, err := journal.LastForRepository(context.Background(), 4)
		assert.NoError(t, err)
		assert.Positive(t, got.ID)
	})

	testigo.Run(t, "preserves an explicit publication identity", func(t *testing.T) {
		journal := NewPublicationJournal()
		if err := journal.Save(context.Background(), domain.Publication{ID: 9, RepositoryID: 4}); err != nil {
			t.Fatal(err)
		}
		got, _, err := journal.LastForRepository(context.Background(), 4)
		assert.NoError(t, err)
		assert.Equal(t, got.ID, int64(9))
	})
}

func TestRunJournalContract(t *testing.T) {
	testigo.Run(t, "assigns an identity when a run starts", func(t *testing.T) {
		run, err := NewRunJournal().Start(context.Background(), domain.Run{Command: "sync", Status: domain.RunRunning})
		assert.NoError(t, err)
		assert.Positive(t, run.ID)
	})

	testigo.Run(t, "accepts the final run state", func(t *testing.T) {
		journal := NewRunJournal()
		run, err := journal.Start(context.Background(), domain.Run{Command: "sync", Status: domain.RunRunning})
		if err != nil {
			t.Fatal(err)
		}
		run.Status = domain.RunSucceeded
		assert.NoError(t, journal.Finish(context.Background(), run))
	})
}
