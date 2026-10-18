package sqlite

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lautaromei/ai-code-reviewer/internal/adapters/sqlite/sqlitetest"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func TestCandidateRepositoryReportsMalformedRows(t *testing.T) {
	testigo.Run(t, "while listing merged candidates", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(malformedRow())
		_, err := NewCandidateGroups(store).ListMergedInto(context.Background(), "survivor")
		assert.ErrorContains(t, err, "scan candidate")
	})

	testigo.Run(t, "while listing candidate evidence", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT candidate_id").WillReturnRows(malformedRow())
		_, err := NewCandidateGroups(store).ListEvidence(context.Background(), "candidate")
		assert.ErrorContains(t, err, "scan evidence")
	})

	testigo.Run(t, "while listing open candidates", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(malformedRow())
		_, err := NewCandidateGroups(store).ListOpen(context.Background(), "go")
		assert.ErrorContains(t, err, "scan candidate")
	})
}

func TestCommentRepositoryReportsExceptionalRows(t *testing.T) {
	testigo.Run(t, "an unknown GitHub identity is absent", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		_, found, err := NewCommentArchive(store).GetByGitHubID(context.Background(), 1, 2)
		assert.NoError(t, err)
		assert.False(t, found, "comment should be absent")
	})

	testigo.Run(t, "a malformed list row is reported", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(malformedRow())
		_, err := NewCommentArchive(store).ListAllOrdered(context.Background())
		assert.ErrorContains(t, err, "scan comment")
	})

	testigo.Run(t, "a list stream failure is reported", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(failedRowStream())
		_, err := NewCommentArchive(store).ListAllOrdered(context.Background())
		assert.ErrorIs(t, err, databaseUnavailable)
	})
}

func TestReactionRepositoryReportsInsertFailure(t *testing.T) {
	testigo.Run(t, "after clearing previous reactions", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("DELETE FROM reactions").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO reactions").WillReturnError(databaseUnavailable)
		reaction := sqlitetest.Reaction.Bare()
		err := NewRuleFeedback(store).ReplaceForComment(context.Background(), reaction.CommentID, []domain.Reaction{reaction})
		assert.ErrorContains(t, err, "insert reaction")
	})
}

func TestRepositoryDirectoryReportsExceptionalResponses(t *testing.T) {
	testigo.Run(t, "upsert without identity", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("INSERT INTO repositories").WillReturnRows(malformedRow())
		_, err := NewRepositoryDirectory(store).Upsert(context.Background(), sqlitetest.Repository.Bare())
		assert.ErrorContains(t, err, "upsert repository")
	})

	testigo.Run(t, "unknown repository", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(sqlmock.NewRows([]string{"id"}))
		_, found, err := NewRepositoryDirectory(store).GetByFullName(context.Background(), "owner/repository")
		assert.NoError(t, err)
		assert.False(t, found, "repository should be absent")
	})

	testigo.Run(t, "malformed repository", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(malformedRow())
		_, _, err := NewRepositoryDirectory(store).GetByFullName(context.Background(), "owner/repository")
		assert.ErrorContains(t, err, "get repository")
	})

	testigo.Run(t, "unavailable repository list", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnError(databaseUnavailable)
		_, err := NewRepositoryDirectory(store).List(context.Background())
		assert.ErrorContains(t, err, "list repositories")
	})

	testigo.Run(t, "malformed repository list row", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(malformedRow())
		_, err := NewRepositoryDirectory(store).List(context.Background())
		assert.ErrorContains(t, err, "scan repository")
	})

	testigo.Run(t, "repository list stream failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(failedRowStream())
		_, err := NewRepositoryDirectory(store).List(context.Background())
		assert.ErrorIs(t, err, databaseUnavailable)
	})
}

func TestRuleCatalogReportsExceptionalResponses(t *testing.T) {
	testigo.Run(t, "rule insertion failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("INSERT INTO rules").WillReturnError(databaseUnavailable)
		err := NewRuleCatalog(store).Insert(context.Background(), sqlitetest.Rule.Bare())
		assert.ErrorContains(t, err, "insert rule")
	})

	testigo.Run(t, "malformed rule", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT rule_id").WillReturnRows(malformedRow())
		_, _, err := NewRuleCatalog(store).Get(context.Background(), "AIR-GO-001")
		assert.ErrorContains(t, err, "get rule")
	})

	testigo.Run(t, "rule status update failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("UPDATE rules").WillReturnError(databaseUnavailable)
		err := NewRuleCatalog(store).UpdateStatus(context.Background(), sqlitetest.RuleStatusEvent.Bare())
		assert.ErrorContains(t, err, "updating rule status")
	})

	testigo.Run(t, "rule status history failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("UPDATE rules").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO rule_status_history").WillReturnError(databaseUnavailable)
		err := NewRuleCatalog(store).UpdateStatus(context.Background(), sqlitetest.RuleStatusEvent.Bare())
		assert.ErrorContains(t, err, "recording rule status history")
	})

	testigo.Run(t, "rule deletion failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin()
		mock.ExpectExec("DELETE FROM snapshot_rules").WillReturnError(databaseUnavailable)
		mock.ExpectRollback()
		err := NewRuleCatalog(store).Delete(context.Background(), "AIR-GO-001")
		assert.ErrorContains(t, err, "delete rule")
	})

	testigo.Run(t, "rule usage insertion failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("INSERT INTO rule_usages").WillReturnError(databaseUnavailable)
		err := NewRuleCatalog(store).RecordUsage(context.Background(), sqlitetest.RuleUsage.Bare())
		assert.ErrorContains(t, err, "recording usage")
	})

	testigo.Run(t, "last usage update failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("INSERT INTO rule_usages").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE rules").WillReturnError(databaseUnavailable)
		err := NewRuleCatalog(store).RecordUsage(context.Background(), sqlitetest.RuleUsage.Bare())
		assert.ErrorContains(t, err, "updating last_used_at")
	})

	testigo.Run(t, "rule sequence failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("INSERT INTO rule_sequences").WillReturnError(databaseUnavailable)
		_, err := NewRuleCatalog(store).NextSequence(context.Background(), "GO")
		assert.ErrorContains(t, err, "next sequence")
	})

	testigo.Run(t, "malformed rule list row", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT rule_id").WillReturnRows(malformedRow())
		_, err := NewRuleCatalog(store).ListByStatus(context.Background(), domain.RuleActive)
		assert.ErrorContains(t, err, "scan rule")
	})

	testigo.Run(t, "rule list stream failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT rule_id").WillReturnRows(failedRowStream())
		_, err := NewRuleCatalog(store).ListByStatus(context.Background(), domain.RuleActive)
		assert.ErrorIs(t, err, databaseUnavailable)
	})
}

func TestSnapshotArchiveReportsExceptionalResponses(t *testing.T) {
	testigo.Run(t, "snapshot identity failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("INSERT INTO snapshots").WillReturnRows(malformedRow())
		err := NewSnapshotArchive(store).Save(context.Background(), sqlitetest.Snapshot.Bare())
		assert.ErrorContains(t, err, "save snapshot")
	})

	testigo.Run(t, "previous rule clearing failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("INSERT INTO snapshots").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectExec("DELETE FROM snapshot_rules").WillReturnError(databaseUnavailable)
		err := NewSnapshotArchive(store).Save(context.Background(), sqlitetest.Snapshot.Bare())
		assert.ErrorContains(t, err, "clearing snapshot_rules")
	})

	testigo.Run(t, "snapshot rule link failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("INSERT INTO snapshots").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
		mock.ExpectExec("DELETE FROM snapshot_rules").WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("INSERT INTO snapshot_rules").WillReturnError(databaseUnavailable)
		snapshot := sqlitetest.Snapshot.WithRuleIDs([]string{"AIR-GO-001"}).Bare()
		assert.ErrorContains(t, NewSnapshotArchive(store).Save(context.Background(), snapshot), "linking rule")
	})

	testigo.Run(t, "malformed latest snapshot", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(malformedRow())
		_, _, err := NewSnapshotArchive(store).Latest(context.Background())
		assert.ErrorContains(t, err, "latest snapshot")
	})

	testigo.Run(t, "unavailable snapshot rules", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(snapshotRow())
		mock.ExpectQuery("SELECT rule_id").WillReturnError(databaseUnavailable)
		_, _, err := NewSnapshotArchive(store).Latest(context.Background())
		assert.ErrorContains(t, err, "loading rules")
	})

	testigo.Run(t, "malformed snapshot rule", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(snapshotRow())
		mock.ExpectQuery("SELECT rule_id").WillReturnRows(sqlmock.NewRows([]string{"rule_id"}).AddRow(nil))
		_, _, err := NewSnapshotArchive(store).Latest(context.Background())
		assert.ErrorContains(t, err, "scanning snapshot rule")
	})

	testigo.Run(t, "snapshot rule stream failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectQuery("SELECT id").WillReturnRows(snapshotRow())
		mock.ExpectQuery("SELECT rule_id").WillReturnRows(failedRowStream())
		_, _, err := NewSnapshotArchive(store).Latest(context.Background())
		assert.ErrorIs(t, err, databaseUnavailable)
	})
}

func malformedRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"malformed"}).AddRow("value")
}

func failedRowStream() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"value"}).AddRow("value").RowError(0, databaseUnavailable)
}

func snapshotRow() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "hash", "run_id", "renderer_version", "created_at"}).
		AddRow(1, "hash", 1, "v1", "2026-08-10T12:00:00Z")
}
