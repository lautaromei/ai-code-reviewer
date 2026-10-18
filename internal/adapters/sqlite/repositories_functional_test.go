package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/sqlite/sqlitetest"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

var contractTime = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

var contractRepository = sqlitetest.Repository.
	WithFullName("example/repository").
	WithAnalyzed(true).
	WithActionable(true).
	WithRulesPath("rules.md").
	WithLanguages([]string{"go"}).
	WithLanguagesAt(contractTime).
	WithDefaultBranch("main")

var contractRule = sqlitetest.Rule.
	WithRuleID("AIR-GO-001").
	WithLanguage("go").
	WithTitle("Rule").
	WithBody("Body").
	WithStatus(domain.RuleActive).
	WithCreatedAt(contractTime).
	WithActivatedAt(contractTime).
	WithSource("seed").
	WithContentHash("hash")

func mustSucceed(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func seedRepository(t *testing.T, store *Database) domain.Repository {
	t.Helper()
	directory := NewRepositoryDirectory(store)
	sqlitetest.NewRepositoryDirectorySeeder(t, directory, context.Background()).Persist(contractRepository)
	repository, found, err := directory.GetByFullName(context.Background(), "example/repository")
	if err != nil || !found {
		t.Fatalf("seed repository: found=%v err=%v", found, err)
	}
	return repository
}

func seedRule(t *testing.T, store *Database) domain.Rule {
	t.Helper()
	rule := contractRule.Bare()
	sqlitetest.NewRuleCatalogSeeder(t, NewRuleCatalog(store), context.Background()).One(rule)
	return rule
}

func seedComment(t *testing.T, store *Database, repositoryID int64, origin domain.CommentOrigin) domain.Comment {
	t.Helper()
	archive := NewCommentArchive(store)
	seed := sqlitetest.StoredReviewComment(repositoryID, origin, contractTime)
	sqlitetest.NewCommentArchiveSeeder(t, archive, context.Background()).One(seed)
	comment, found, err := archive.GetByGitHubID(context.Background(), repositoryID, seed.GitHubCommentID)
	if err != nil || !found {
		t.Fatalf("seed comment: found=%v err=%v", found, err)
	}
	return comment
}

func TestSQLiteCommentArchiveContract(t *testing.T) {
	testigo.Run(t, "gets a stored comment by local identity", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginHuman)

		got, found, err := NewCommentArchive(store).GetByID(context.Background(), comment.ID)

		assert.NoError(t, err)
		assert.True(t, found, "stored comment should exist")
		assert.Equal(t, got, comment)
	})

	testigo.Run(t, "gets a stored comment by GitHub identity", func(t *testing.T) {
		store, _ := openTestStore(t)
		repository := seedRepository(t, store)
		comment := seedComment(t, store, repository.ID, domain.OriginHuman)

		got, found, err := NewCommentArchive(store).GetByGitHubID(context.Background(), repository.ID, comment.GitHubCommentID)

		assert.NoError(t, err)
		assert.True(t, found, "stored comment should exist")
		assert.Equal(t, got, comment)
	})

	testigo.Run(t, "reports a missing comment", func(t *testing.T) {
		store, _ := openTestStore(t)
		_, found, err := NewCommentArchive(store).GetByID(context.Background(), 999)
		assert.NoError(t, err)
		assert.False(t, found, "unknown comment should be absent")
	})

	testigo.Run(t, "lists a comment that still needs classification", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginHuman)
		pending, err := NewCommentArchive(store).ListPendingClassification(context.Background(), "v1")
		assert.NoError(t, err)
		assert.Equal(t, pending, []domain.Comment{comment})
	})

	testigo.Run(t, "excludes a comment already classified for the same input", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginHuman)
		mustSucceed(t, NewClassificationHistory(store).Save(context.Background(), sqlitetest.ClassificationFor(comment, domain.VerdictCovered, contractTime)))
		pending, err := NewCommentArchive(store).ListPendingClassification(context.Background(), "v1")
		assert.NoError(t, err)
		assert.Empty(t, pending)
	})

	testigo.Run(t, "lists comments whose AI origin is tracked", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginAIRuleUsage)
		tracked, err := NewCommentArchive(store).ListTrackedAI(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, tracked, []domain.Comment{comment})
	})

	testigo.Run(t, "lists comments that used a rule", func(t *testing.T) {
		store, _ := openTestStore(t)
		repository := seedRepository(t, store)
		rule := seedRule(t, store)
		comment := seedComment(t, store, repository.ID, domain.OriginAIRuleUsage)
		mustSucceed(t, NewRuleCatalog(store).RecordUsage(context.Background(), domain.RuleUsage{
			RuleID: rule.RuleID, CommentID: comment.ID, UsedAt: contractTime,
		}))
		comments, err := NewCommentArchive(store).ListForRule(context.Background(), rule.RuleID)
		assert.NoError(t, err)
		assert.Equal(t, comments, []domain.Comment{comment})
	})

	testigo.Run(t, "orders comments by repository date and GitHub identity", func(t *testing.T) {
		store, _ := openTestStore(t)
		repository := seedRepository(t, store)
		later := seedComment(t, store, repository.ID, domain.OriginHuman)
		earlier := later
		earlier.ID = 0
		earlier.GitHubCommentID = 1
		earlier.CreatedAt = contractTime.Add(-time.Hour)
		earlier, err := NewCommentArchive(store).Upsert(context.Background(), earlier)
		mustSucceed(t, err)
		comments, err := NewCommentArchive(store).ListAllOrdered(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, []int64{comments[0].ID, comments[1].ID}, []int64{earlier.ID, later.ID})
	})
}

func TestSQLiteClassificationHistoryContract(t *testing.T) {
	testigo.Run(t, "reports a missing classification", func(t *testing.T) {
		store, _ := openTestStore(t)
		_, found, err := NewClassificationHistory(store).Get(context.Background(), 1, "hash", "v1")
		assert.NoError(t, err)
		assert.False(t, found, "unknown classification should be absent")
	})

	testigo.Run(t, "returns a saved classification", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginHuman)
		want := sqlitetest.ClassificationFor(comment, domain.VerdictNew, contractTime)
		mustSucceed(t, NewClassificationHistory(store).Save(context.Background(), want))
		got, found, err := NewClassificationHistory(store).Get(context.Background(), comment.ID, comment.InputHash, "v1")
		assert.NoError(t, err)
		assert.True(t, found, "saved classification should exist")
		assert.Equal(t, got, want)
	})
}

func TestSQLiteCommentCursorContract(t *testing.T) {
	testigo.Run(t, "reports an absent cursor", func(t *testing.T) {
		store, _ := openTestStore(t)
		repository := seedRepository(t, store)
		_, found, err := NewCommentCursor(store).Get(context.Background(), repository.ID, "comments")
		assert.NoError(t, err)
		assert.False(t, found, "cursor should be absent before first advance")
	})

	testigo.Run(t, "returns the advanced cursor", func(t *testing.T) {
		store, _ := openTestStore(t)
		repository := seedRepository(t, store)
		want := domain.Cursor{RepositoryID: repository.ID, Stream: "comments", HighWatermark: contractTime, LastCommentID: 42, ETag: "etag", LastSuccessAt: contractTime}
		mustSucceed(t, NewCommentCursor(store).Advance(context.Background(), want))
		got, found, err := NewCommentCursor(store).Get(context.Background(), repository.ID, "comments")
		assert.NoError(t, err)
		assert.True(t, found, "advanced cursor should exist")
		assert.Equal(t, got, want)
	})
}

func TestSQLiteRuleFeedbackContract(t *testing.T) {
	testigo.Run(t, "counts reactions for one comment", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginAIRuleUsage)
		mustSucceed(t, NewRuleFeedback(store).ReplaceForComment(context.Background(), comment.ID, []domain.Reaction{
			{GitHubReactionID: 1, User: "a", Content: "+1", CreatedAt: contractTime, LastSeenAt: contractTime},
			{GitHubReactionID: 2, User: "b", Content: "+1", CreatedAt: contractTime, LastSeenAt: contractTime},
		}))
		count, err := NewRuleFeedback(store).CountByComment(context.Background(), comment.ID, "+1")
		assert.NoError(t, err)
		assert.Equal(t, count, 2)
	})

	testigo.Run(t, "counts reactions for comments that used a rule", func(t *testing.T) {
		store, _ := openTestStore(t)
		rule := seedRule(t, store)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginAIRuleUsage)
		mustSucceed(t, NewRuleCatalog(store).RecordUsage(context.Background(), domain.RuleUsage{RuleID: rule.RuleID, CommentID: comment.ID, UsedAt: contractTime}))
		mustSucceed(t, NewRuleFeedback(store).ReplaceForComment(context.Background(), comment.ID, []domain.Reaction{
			{GitHubReactionID: 1, User: "a", Content: "+1", CreatedAt: contractTime, LastSeenAt: contractTime},
		}))
		count, err := NewRuleFeedback(store).CountByRule(context.Background(), rule.RuleID, "+1")
		assert.NoError(t, err)
		assert.Equal(t, count, 1)
	})

	testigo.Run(t, "replacement removes reactions no longer returned", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginAIRuleUsage)
		feedback := NewRuleFeedback(store)
		mustSucceed(t, feedback.ReplaceForComment(context.Background(), comment.ID, []domain.Reaction{{GitHubReactionID: 1, Content: "+1"}}))
		mustSucceed(t, feedback.ReplaceForComment(context.Background(), comment.ID, nil))
		count, err := feedback.CountByComment(context.Background(), comment.ID, "+1")
		assert.NoError(t, err)
		assert.Zero(t, count)
	})
}

func TestSQLiteRuleCatalogContract(t *testing.T) {
	testigo.Run(t, "filters active rules by status", func(t *testing.T) {
		store, _ := openTestStore(t)
		rule := seedRule(t, store)
		got, err := NewRuleCatalog(store).ListByStatus(context.Background(), domain.RuleActive)
		assert.NoError(t, err)
		assert.Equal(t, got, []domain.Rule{rule})
	})

	testigo.Run(t, "filters active rules by language", func(t *testing.T) {
		store, _ := openTestStore(t)
		rule := seedRule(t, store)
		got, err := NewRuleCatalog(store).ListActiveByLanguages(context.Background(), []string{"go", "php"})
		assert.NoError(t, err)
		assert.Equal(t, got, []domain.Rule{rule})
	})

	testigo.Run(t, "an empty language selection returns no rules", func(t *testing.T) {
		store, _ := openTestStore(t)
		seedRule(t, store)
		got, err := NewRuleCatalog(store).ListActiveByLanguages(context.Background(), nil)
		assert.NoError(t, err)
		assert.Empty(t, got)
	})

	testigo.Run(t, "status change is visible through Get", func(t *testing.T) {
		store, _ := openTestStore(t)
		rule := seedRule(t, store)
		event := sqlitetest.RuleStatusEvent.
			WithEventKey("disable").
			WithRuleID(rule.RuleID).
			WithFromState(domain.RuleActive).
			WithToState(domain.RuleDisabled).
			WithReason(domain.DisabledManual).
			WithOccurredAt(contractTime).
			Bare()
		mustSucceed(t, NewRuleCatalog(store).UpdateStatus(context.Background(), event))
		got, found, err := NewRuleCatalog(store).Get(context.Background(), rule.RuleID)
		assert.NoError(t, err)
		assert.True(t, found, "updated rule should exist")
		assert.Equal(t, got.Status, domain.RuleDisabled)
	})

	testigo.Run(t, "delete makes a rule unavailable", func(t *testing.T) {
		store, _ := openTestStore(t)
		rule := seedRule(t, store)
		mustSucceed(t, NewRuleCatalog(store).Delete(context.Background(), rule.RuleID))
		_, found, err := NewRuleCatalog(store).Get(context.Background(), rule.RuleID)
		assert.NoError(t, err)
		assert.False(t, found, "deleted rule should be absent")
	})
}

func seedCandidates(t *testing.T, store *Database) (domain.CandidateCluster, domain.CandidateCluster) {
	t.Helper()
	primary := sqlitetest.Candidate.WithID("primary").WithLanguage("go").WithLabel("primary").WithStatus(domain.CandidateOpen).WithClassifierVersion("v1").WithCreatedAt(contractTime).WithUpdatedAt(contractTime).Bare()
	secondary := sqlitetest.Candidate.WithID("secondary").WithLanguage("go").WithLabel("secondary").WithStatus(domain.CandidateOpen).WithClassifierVersion("v1").WithCreatedAt(contractTime).WithUpdatedAt(contractTime).Bare()
	sqlitetest.NewCandidateGroupsSeeder(t, NewCandidateGroups(store), context.Background()).Many(primary, secondary)
	return primary, secondary
}

func TestSQLiteCandidateGroupsContract(t *testing.T) {
	testigo.Run(t, "gets an upserted candidate", func(t *testing.T) {
		store, _ := openTestStore(t)
		primary, _ := seedCandidates(t, store)
		got, found, err := NewCandidateGroups(store).Get(context.Background(), primary.ID)
		assert.NoError(t, err)
		assert.True(t, found, "upserted candidate should exist")
		assert.Equal(t, got, primary)
	})

	testigo.Run(t, "reports a missing candidate", func(t *testing.T) {
		store, _ := openTestStore(t)
		_, found, err := NewCandidateGroups(store).Get(context.Background(), "missing")
		assert.NoError(t, err)
		assert.False(t, found, "unknown candidate should be absent")
	})

	testigo.Run(t, "lists open candidates for a language", func(t *testing.T) {
		store, _ := openTestStore(t)
		primary, secondary := seedCandidates(t, store)
		got, err := NewCandidateGroups(store).ListOpen(context.Background(), "go")
		assert.NoError(t, err)
		assert.Equal(t, got, []domain.CandidateCluster{primary, secondary})
	})

	testigo.Run(t, "lists attached evidence", func(t *testing.T) {
		store, _ := openTestStore(t)
		primary, _ := seedCandidates(t, store)
		comment := seedComment(t, store, seedRepository(t, store).ID, domain.OriginHuman)
		want := domain.CandidateEvidence{CandidateID: primary.ID, CommentID: comment.ID, Kind: domain.EvidenceHuman, ThumbsUp: 2, EvaluatedAt: contractTime}
		mustSucceed(t, NewCandidateGroups(store).AttachEvidence(context.Background(), want))
		got, err := NewCandidateGroups(store).ListEvidence(context.Background(), primary.ID)
		assert.NoError(t, err)
		assert.Equal(t, got, []domain.CandidateEvidence{want})
	})

	testigo.Run(t, "merge marks the secondary candidate", func(t *testing.T) {
		store, _ := openTestStore(t)
		primary, secondary := seedCandidates(t, store)
		mustSucceed(t, NewCandidateGroups(store).Merge(context.Background(), primary.ID, []string{secondary.ID}))
		got, err := NewCandidateGroups(store).ListMergedInto(context.Background(), primary.ID)
		assert.NoError(t, err)
		assert.Equal(t, got[0].ID, secondary.ID)
	})

	testigo.Run(t, "promotion records the resulting rule", func(t *testing.T) {
		store, _ := openTestStore(t)
		primary, _ := seedCandidates(t, store)
		mustSucceed(t, NewCandidateGroups(store).Promote(context.Background(), primary.ID, "AIR-GO-001"))
		got, found, err := NewCandidateGroups(store).Get(context.Background(), primary.ID)
		assert.NoError(t, err)
		assert.True(t, found, "promoted candidate should exist")
		assert.Equal(t, got.PromotedRuleID, "AIR-GO-001")
	})
}

func TestSQLiteSnapshotArchiveContract(t *testing.T) {
	testigo.Run(t, "Latest is empty before the first snapshot", func(t *testing.T) {
		store, _ := openTestStore(t)
		_, found, err := NewSnapshotArchive(store).Latest(context.Background())
		assert.NoError(t, err)
		assert.False(t, found, "snapshot should be absent before Save")
	})

	testigo.Run(t, "Save makes the snapshot visible", func(t *testing.T) {
		store, _ := openTestStore(t)
		seedRule(t, store)
		mustSucceed(t, NewSnapshotArchive(store).Save(context.Background(), domain.Snapshot{
			Hash: "snapshot", RendererVersion: "v1", CreatedAt: contractTime, RuleIDs: []string{"AIR-GO-001"},
		}))
		got, found, err := NewSnapshotArchive(store).Latest(context.Background())
		assert.NoError(t, err)
		assert.True(t, found, "saved snapshot should exist")
		assert.Equal(t, got.Hash, "snapshot")
	})

	testigo.Run(t, "Save preserves rule order", func(t *testing.T) {
		store, _ := openTestStore(t)
		first := seedRule(t, store)
		second := first
		second.RuleID = "AIR-GO-002"
		mustSucceed(t, NewRuleCatalog(store).Insert(context.Background(), second))
		mustSucceed(t, NewSnapshotArchive(store).Save(context.Background(), domain.Snapshot{
			Hash: "snapshot", RendererVersion: "v1", CreatedAt: contractTime,
			RuleIDs: []string{second.RuleID, first.RuleID},
		}))
		got, _, err := NewSnapshotArchive(store).Latest(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, got.RuleIDs, []string{second.RuleID, first.RuleID})
	})
}

func TestSQLitePublicationJournalContract(t *testing.T) {
	testigo.Run(t, "reports no publication before Save", func(t *testing.T) {
		store, _ := openTestStore(t)
		repository := seedRepository(t, store)
		_, found, err := NewPublicationJournal(store).LastForRepository(context.Background(), repository.ID)
		assert.NoError(t, err)
		assert.False(t, found, "publication should be absent before Save")
	})

	testigo.Run(t, "returns the last saved publication", func(t *testing.T) {
		store, _ := openTestStore(t)
		repository := seedRepository(t, store)
		seedRule(t, store)
		mustSucceed(t, NewSnapshotArchive(store).Save(context.Background(), domain.Snapshot{
			Hash: "snapshot", RendererVersion: "v1", CreatedAt: contractTime,
		}))
		snapshot, _, err := NewSnapshotArchive(store).Latest(context.Background())
		mustSucceed(t, err)
		mustSucceed(t, NewPublicationJournal(store).Save(context.Background(), domain.Publication{
			SnapshotID: snapshot.ID, RepositoryID: repository.ID, FilteredSnapshotHash: "filtered", Branch: "rules",
			Status: domain.PublicationCreated, CreatedAt: contractTime,
		}))
		got, found, err := NewPublicationJournal(store).LastForRepository(context.Background(), repository.ID)
		assert.NoError(t, err)
		assert.True(t, found, "saved publication should exist")
		assert.Equal(t, got.Status, domain.PublicationCreated)
	})
}

func TestSQLiteRunJournalContract(t *testing.T) {
	testigo.Run(t, "Start assigns a run identity", func(t *testing.T) {
		store, _ := openTestStore(t)
		run, err := NewRunJournal(store).Start(context.Background(), domain.Run{Command: "sync", Status: domain.RunRunning, StartedAt: contractTime})
		assert.NoError(t, err)
		assert.Positive(t, run.ID)
	})

	testigo.Run(t, "Finish persists the observable run status", func(t *testing.T) {
		store, _ := openTestStore(t)
		run, err := NewRunJournal(store).Start(context.Background(), domain.Run{Command: "sync", Status: domain.RunRunning, StartedAt: contractTime})
		mustSucceed(t, err)
		finishedAt := contractTime.Add(time.Minute)
		run.Status, run.FinishedAt = domain.RunSucceeded, &finishedAt
		mustSucceed(t, NewRunJournal(store).Finish(context.Background(), run))
		var status string
		err = store.conn().QueryRowContext(context.Background(), `SELECT status FROM runs WHERE id = ?`, run.ID).Scan(&status)
		assert.NoError(t, err)
		assert.Equal(t, status, string(domain.RunSucceeded))
	})
}

func TestSQLiteRepositoriesReportUnavailableDatabase(t *testing.T) {
	store, _ := openTestStore(t)
	mustSucceed(t, store.Close())
	ctx := context.Background()
	now := time.Now().UTC()

	cases := []struct {
		name string
		call func() error
	}{
		{"upsert candidate", func() error {
			_, err := NewCandidateGroups(store).Upsert(ctx, domain.CandidateCluster{ID: "x", CreatedAt: now, UpdatedAt: now})
			return err
		}},
		{"get candidate", func() error { _, _, err := NewCandidateGroups(store).Get(ctx, "x"); return err }},
		{"list merged candidates", func() error { _, err := NewCandidateGroups(store).ListMergedInto(ctx, "x"); return err }},
		{"list candidate evidence", func() error { _, err := NewCandidateGroups(store).ListEvidence(ctx, "x"); return err }},
		{"list open candidates", func() error { _, err := NewCandidateGroups(store).ListOpen(ctx, "go"); return err }},
		{"attach evidence", func() error { return NewCandidateGroups(store).AttachEvidence(ctx, domain.CandidateEvidence{}) }},
		{"merge candidates", func() error { return NewCandidateGroups(store).Merge(ctx, "x", []string{"y"}) }},
		{"promote candidate", func() error { return NewCandidateGroups(store).Promote(ctx, "x", "rule") }},
		{"get classification", func() error { _, _, err := NewClassificationHistory(store).Get(ctx, 1, "hash", "v1"); return err }},
		{"save classification", func() error { return NewClassificationHistory(store).Save(ctx, domain.Classification{}) }},
		{"get comment", func() error { _, _, err := NewCommentArchive(store).GetByID(ctx, 1); return err }},
		{"get github comment", func() error { _, _, err := NewCommentArchive(store).GetByGitHubID(ctx, 1, 1); return err }},
		{"list pending comments", func() error { _, err := NewCommentArchive(store).ListPendingClassification(ctx, "v1"); return err }},
		{"list tracked comments", func() error { _, err := NewCommentArchive(store).ListTrackedAI(ctx); return err }},
		{"list comments for rule", func() error { _, err := NewCommentArchive(store).ListForRule(ctx, "rule"); return err }},
		{"list comments", func() error { _, err := NewCommentArchive(store).ListAllOrdered(ctx); return err }},
		{"get cursor", func() error { _, _, err := NewCommentCursor(store).Get(ctx, 1, "comments"); return err }},
		{"advance cursor", func() error { return NewCommentCursor(store).Advance(ctx, domain.Cursor{}) }},
		{"save publication", func() error { return NewPublicationJournal(store).Save(ctx, domain.Publication{}) }},
		{"get publication", func() error { _, _, err := NewPublicationJournal(store).LastForRepository(ctx, 1); return err }},
		{"replace reactions", func() error { return NewRuleFeedback(store).ReplaceForComment(ctx, 1, nil) }},
		{"count reactions by rule", func() error { _, err := NewRuleFeedback(store).CountByRule(ctx, "rule", "+1"); return err }},
		{"count reactions by comment", func() error { _, err := NewRuleFeedback(store).CountByComment(ctx, 1, "+1"); return err }},
		{"list rules", func() error { _, err := NewRuleCatalog(store).ListByStatus(ctx, domain.RuleActive); return err }},
		{"list rules by language", func() error { _, err := NewRuleCatalog(store).ListActiveByLanguages(ctx, []string{"go"}); return err }},
		{"update rule", func() error { return NewRuleCatalog(store).UpdateStatus(ctx, domain.RuleStatusEvent{}) }},
		{"delete rule", func() error { return NewRuleCatalog(store).Delete(ctx, "rule") }},
		{"start run", func() error { _, err := NewRunJournal(store).Start(ctx, domain.Run{}); return err }},
		{"finish run", func() error { return NewRunJournal(store).Finish(ctx, domain.Run{}) }},
		{"save snapshot", func() error { return NewSnapshotArchive(store).Save(ctx, domain.Snapshot{}) }},
		{"get snapshot", func() error { _, _, err := NewSnapshotArchive(store).Latest(ctx); return err }},
	}
	for _, tc := range cases {
		testigo.Run(t, tc.name, func(t *testing.T) {
			assert.Error(t, tc.call())
		})
	}
}
