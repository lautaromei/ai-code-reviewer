package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type dbtx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Database struct {
	db *sql.DB
	tx *sql.Tx
}

func Open(ctx context.Context, dsn string, busyTimeout time.Duration) (*Database, error) {
	return openWith(ctx, dsn, busyTimeout, localSQLite{})
}

type sqliteRuntime interface {
	MakeDirectory(string) error
	OpenDatabase(string) (*sql.DB, error)
	Migrate(context.Context, *sql.DB) error
}

type localSQLite struct{}

func (localSQLite) MakeDirectory(path string) error { return os.MkdirAll(path, 0o755) }
func (localSQLite) OpenDatabase(dsn string) (*sql.DB, error) {
	return sql.Open("sqlite", dsn)
}
func (localSQLite) Migrate(ctx context.Context, db *sql.DB) error {
	return applyMigrations(ctx, db)
}

func openWith(ctx context.Context, dsn string, busyTimeout time.Duration, runtime sqliteRuntime) (*Database, error) {
	if dsn == "" {
		return nil, fmt.Errorf("sqlite: DSN is empty")
	}

	if path := filePathFromDSN(dsn); path != "" && path != ":memory:" {
		dir := filepath.Dir(path)
		if dir != "" && dir != "." {
			if err := runtime.MakeDirectory(dir); err != nil {
				return nil, fmt.Errorf("sqlite: create directory %q: %w", dir, err)
			}
		}
	}

	db, err := runtime.OpenDatabase(dsn)
	if err != nil {
		return nil, fmt.Errorf("sqlite: abriendo %q: %w", dsn, err)
	}

	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: ping %q: %w", dsn, err)
	}

	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA foreign_keys=ON;",
		fmt.Sprintf("PRAGMA busy_timeout=%d;", busyTimeout.Milliseconds()),
	}
	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			db.Close()
			return nil, fmt.Errorf("sqlite: aplicando %q: %w", p, err)
		}
	}
	if path := filePathFromDSN(dsn); path != "" && path != ":memory:" {
		if needsBackup(ctx, db) {
			if err := backupFile(path); err != nil {
				db.Close()
				return nil, fmt.Errorf("sqlite: backup before migration: %w", err)
			}
		}
	}

	if err := runtime.Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}

	return &Database{db: db}, nil
}
func needsBackup(ctx context.Context, db *sql.DB) bool {
	var n int
	return db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE version=1 AND NOT EXISTS(SELECT 1 FROM schema_migrations WHERE version=2)`).Scan(&n) == nil && n > 0
}
func backupFile(path string) error {
	src, e := os.Open(path)
	if e != nil {
		return e
	}
	defer src.Close()
	dst, e := os.OpenFile(path+".pre-v2.bak", os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if os.IsExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func filePathFromDSN(dsn string) string {
	path := dsn
	if strings.HasPrefix(path, "file:") {
		path = strings.TrimPrefix(path, "file:")
	}
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	if path == ":memory:" || strings.Contains(path, ":memory:") {
		return ":memory:"
	}
	return path
}

func (s *Database) conn() dbtx {
	if s.tx != nil {
		return s.tx
	}
	return s.db
}

func NewRepositoryDirectory(database *Database) *repositoryRepo { return &repositoryRepo{s: database} }
func NewCommentCursor(database *Database) *cursorRepo           { return &cursorRepo{s: database} }
func NewCommentArchive(database *Database) *commentRepo         { return &commentRepo{s: database} }
func NewRuleFeedback(database *Database) *reactionRepo          { return &reactionRepo{s: database} }
func NewRuleCatalog(database *Database) *ruleRepo               { return &ruleRepo{s: database} }
func NewCandidateGroups(database *Database) *candidateRepo      { return &candidateRepo{s: database} }
func NewClassificationHistory(database *Database) *classificationRepo {
	return &classificationRepo{s: database}
}
func NewRunJournal(database *Database) *runRepo                 { return &runRepo{s: database} }
func NewSnapshotArchive(database *Database) *snapshotRepo       { return &snapshotRepo{s: database} }
func NewPublicationJournal(database *Database) *publicationRepo { return &publicationRepo{s: database} }
func NewReviewStore(database *Database) *ReviewStore            { return &ReviewStore{s: database} }
func NewPromptStore(database *Database) *PromptStore            { return &PromptStore{s: database} }

func (s *Database) WithinRuleRepositoryTx(ctx context.Context, fn func(*ruleRepo) error) error {
	return s.withinTx(ctx, func(txStore *Database) error {
		return fn(NewRuleCatalog(txStore))
	})
}

func (s *Database) withinTx(ctx context.Context, fn func(*Database) error) error {
	if s.tx != nil {
		return fn(s)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sqlite: begin transaction: %w", err)
	}

	txStore := &Database{tx: tx}
	if err := fn(txStore); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback also failed: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("sqlite: haciendo commit: %w", err)
	}
	return nil
}

func (s *Database) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}
