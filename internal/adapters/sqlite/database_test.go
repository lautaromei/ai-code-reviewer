package sqlite

import (
	"context"
	"database/sql"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/lautaromei/ai-code-reviewer/internal/adapters/sqlite/sqlitetest"
	"github.com/lautaromei/ai-code-reviewer/internal/domain"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

func openTestStore(t *testing.T) (*Database, string) {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "ai-rules.db")
	store, err := Open(context.Background(), dsn, 5*time.Second)
	if err != nil {
		t.Fatalf("open sqlite test store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dsn
}

func reopenTestStore(t *testing.T) *Database {
	t.Helper()
	first, dsn := openTestStore(t)
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), dsn, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestDatabaseMigrationContract(t *testing.T) {
	testigo.Run(t, "reopening preserves applied migrations", func(t *testing.T) {
		store := reopenTestStore(t)
		var count int
		err := store.conn().QueryRowContext(context.Background(), `SELECT COUNT(*) FROM schema_migrations`).Scan(&count)
		assert.NoError(t, err)
		assert.Positive(t, count)
	})

	testigo.Run(t, "reopening does not duplicate a migration", func(t *testing.T) {
		store := reopenTestStore(t)
		var version int
		err := store.conn().QueryRowContext(context.Background(), `SELECT version FROM schema_migrations GROUP BY version HAVING COUNT(*) > 1 LIMIT 1`).Scan(&version)
		assert.ErrorIs(t, err, sql.ErrNoRows)
	})
}

var fixtureTime = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

var storedRule = sqlitetest.Rule.
	WithRuleID("GO-GO-001").
	WithLanguage("go").
	WithTitle("Avoid err shadowing").
	WithScope("go").
	WithBody("Do not reassign err without checking it first.").
	WithContentHash("abc123").
	WithStatus(domain.RuleActive).
	WithSource("seed").
	WithCreatedAt(fixtureTime).
	WithActivatedAt(fixtureTime).
	WithLastUsedAt(nil).
	WithDisabledAt(nil).
	Bare()

func TestRuleStorageContract(t *testing.T) {
	testigo.Run(t, "returns a stored rule", func(t *testing.T) {
		store, _ := openTestStore(t)
		sqlitetest.NewRuleCatalogSeeder(t, NewRuleCatalog(store), context.Background()).One(storedRule)
		got, found, err := NewRuleCatalog(store).Get(context.Background(), storedRule.RuleID)
		assert.NoError(t, err)
		assert.True(t, found, "rule should exist")
		assert.Equal(t, got, storedRule)
	})

	testigo.Run(t, "reports an unknown rule", func(t *testing.T) {
		store, _ := openTestStore(t)
		_, found, err := NewRuleCatalog(store).Get(context.Background(), "UNKNOWN-001")
		assert.NoError(t, err)
		assert.False(t, found, "rule should be absent")
	})
}

func concurrentSequences(t *testing.T, store *Database, amount int) []int {
	t.Helper()
	results := make(chan int, amount)
	errors := make(chan error, amount)
	var workers sync.WaitGroup
	for range amount {
		workers.Add(1)
		go func() {
			defer workers.Done()
			sequence, err := NewRuleCatalog(store).NextSequence(context.Background(), "go")
			if err != nil {
				errors <- err
				return
			}
			results <- sequence
		}()
	}
	workers.Wait()
	close(results)
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	sequences := make([]int, 0, amount)
	for sequence := range results {
		sequences = append(sequences, sequence)
	}
	sort.Ints(sequences)
	return sequences
}

func TestRuleSequenceContract(t *testing.T) {
	testigo.Run(t, "is gapless under concurrency", func(t *testing.T) {
		store, _ := openTestStore(t)
		assert.Equal(t, concurrentSequences(t, store, 20), []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	})

	testigo.Run(t, "starts independently for each language", func(t *testing.T) {
		store, _ := openTestStore(t)
		sequence, err := NewRuleCatalog(store).NextSequence(context.Background(), "php")
		assert.NoError(t, err)
		assert.Equal(t, sequence, 1)
	})
}

func ruleAfterNewerAndOlderUsage(t *testing.T) domain.Rule {
	t.Helper()
	store, _ := openTestStore(t)
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rule := sqlitetest.Rule.WithRuleID("AIR-GO-001").WithStatus(domain.RuleActive).WithCreatedAt(now).WithActivatedAt(now).Bare()
	sqlitetest.NewRuleCatalogSeeder(t, NewRuleCatalog(store), context.Background()).One(rule)
	repositoryInput := sqlitetest.Repository.WithID(0).WithFullName("owner/repository").Bare()
	sqlitetest.NewRepositoryDirectorySeeder(t, NewRepositoryDirectory(store), context.Background()).One(repositoryInput)
	repository, found, err := NewRepositoryDirectory(store).GetByFullName(context.Background(), repositoryInput.FullName)
	if err != nil || !found {
		t.Fatalf("get seeded repository: found=%t err=%v", found, err)
	}
	commentInput := sqlitetest.AIRuleUsageComment.WithID(0).WithGitHubCommentID(42).WithRepositoryID(repository.ID).Bare()
	sqlitetest.NewCommentArchiveSeeder(t, NewCommentArchive(store), context.Background()).One(commentInput)
	comment, found, err := NewCommentArchive(store).GetByGitHubID(context.Background(), repository.ID, commentInput.GitHubCommentID)
	if err != nil || !found {
		t.Fatalf("get seeded comment: found=%t err=%v", found, err)
	}
	usage := sqlitetest.NewRuleUsageHistorySeeder(t, NewRuleCatalog(store), context.Background())
	usage.Many(
		sqlitetest.RuleUsage.WithRuleID(rule.RuleID).WithCommentID(comment.ID).WithUsedAt(now.Add(48*time.Hour)).Bare(),
		sqlitetest.RuleUsage.WithRuleID(rule.RuleID).WithCommentID(comment.ID).WithUsedAt(now.Add(24*time.Hour)).Bare(),
	)
	got, found, err := NewRuleCatalog(store).Get(context.Background(), rule.RuleID)
	if err != nil || !found {
		t.Fatalf("get used rule: found=%t err=%v", found, err)
	}
	return got
}

func TestRuleUsageContract(t *testing.T) {
	testigo.Run(t, "older usage does not regress last activity", func(t *testing.T) {
		rule := ruleAfterNewerAndOlderUsage(t)
		assert.Equal(t, *rule.LastUsedAt, time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC))
	})
}

type transactionFailure struct{}

func (transactionFailure) Error() string { return "deliberate failure" }

func rollbackRule(t *testing.T) (*Database, error) {
	t.Helper()
	store, _ := openTestStore(t)
	err := store.WithinRuleRepositoryTx(context.Background(), func(rules *ruleRepo) error {
		if err := rules.Insert(context.Background(), sqlitetest.Rule.WithRuleID("ROLLBACK-001").Bare()); err != nil {
			return err
		}
		return transactionFailure{}
	})
	return store, err
}

func TestRuleTransactionContract(t *testing.T) {
	testigo.Run(t, "returns the use-case failure", func(t *testing.T) {
		_, err := rollbackRule(t)
		assert.ErrorIs(t, err, transactionFailure{})
	})

	testigo.Run(t, "rollback leaves no rule", func(t *testing.T) {
		store, _ := rollbackRule(t)
		_, found, err := NewRuleCatalog(store).Get(context.Background(), "ROLLBACK-001")
		assert.NoError(t, err)
		assert.False(t, found, "rule should be absent")
	})

	testigo.Run(t, "commit preserves the rule", func(t *testing.T) {
		store, _ := openTestStore(t)
		err := store.WithinRuleRepositoryTx(context.Background(), func(rules *ruleRepo) error {
			return rules.Insert(context.Background(), sqlitetest.Rule.WithRuleID("COMMIT-001").Bare())
		})
		if err != nil {
			t.Fatal(err)
		}
		_, found, err := NewRuleCatalog(store).Get(context.Background(), "COMMIT-001")
		assert.NoError(t, err)
		assert.True(t, found, "rule should exist")
	})
}

func TestForeignKeyContract(t *testing.T) {
	testigo.Run(t, "rejects a comment for an unknown repository", func(t *testing.T) {
		store, _ := openTestStore(t)
		comment := sqlitetest.Comment.WithID(0).WithRepositoryID(999999).Bare()
		_, err := NewCommentArchive(store).Upsert(context.Background(), comment)
		assert.Error(t, err)
	})
}

var repositoryRecord = sqlitetest.Repository.
	WithID(0).
	WithFullName("example-org/ai-reviewer").
	WithAnalyzed(true).
	WithActionable(true).
	WithRulesPath("rules").
	WithLanguages([]string{"go", "php"}).
	WithDefaultBranch("main").
	Bare()

func saveRepository(t *testing.T, store *Database, repository domain.Repository) domain.Repository {
	t.Helper()
	saved, err := NewRepositoryDirectory(store).Upsert(context.Background(), repository)
	if err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestRepositoryDirectoryContract(t *testing.T) {
	testigo.Run(t, "assigns an identity", func(t *testing.T) {
		store, _ := openTestStore(t)
		assert.Positive(t, saveRepository(t, store, repositoryRecord).ID)
	})

	testigo.Run(t, "returns configured languages", func(t *testing.T) {
		store, _ := openTestStore(t)
		saveRepository(t, store, repositoryRecord)
		got, found, err := NewRepositoryDirectory(store).GetByFullName(context.Background(), repositoryRecord.FullName)
		assert.NoError(t, err)
		assert.True(t, found, "repository should exist")
		assert.Equal(t, got.Languages, []string{"go", "php"})
	})

	testigo.Run(t, "upsert preserves its identity", func(t *testing.T) {
		store, _ := openTestStore(t)
		first := saveRepository(t, store, repositoryRecord)
		updated := repositoryRecord
		updated.Actionable = false
		second := saveRepository(t, store, updated)
		assert.Equal(t, second.ID, first.ID)
	})

	testigo.Run(t, "upsert keeps one directory entry", func(t *testing.T) {
		store, _ := openTestStore(t)
		saveRepository(t, store, repositoryRecord)
		saveRepository(t, store, repositoryRecord)
		all, err := NewRepositoryDirectory(store).List(context.Background())
		assert.NoError(t, err)
		assert.Len(t, all, 1)
	})
}
