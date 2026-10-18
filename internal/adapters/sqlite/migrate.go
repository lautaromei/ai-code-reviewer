package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

const createSchemaMigrationsTable = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);`

type migrationFile struct {
	version int
	name    string
	sql     string
}

type migrationSource interface {
	ReadDir(string) ([]fs.DirEntry, error)
	ReadFile(string) ([]byte, error)
}

func loadMigrationsFrom(source migrationSource) ([]migrationFile, error) {
	entries, err := source.ReadDir("migrations")
	if err != nil {
		return nil, fmt.Errorf("sqlite: leyendo migrations embebidas: %w", err)
	}

	migrations := make([]migrationFile, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		versionPart := strings.SplitN(entry.Name(), "_", 2)[0]
		version, err := strconv.Atoi(versionPart)
		if err != nil {
			return nil, fmt.Errorf("sqlite: invalid migration filename %q: %w", entry.Name(), err)
		}
		content, err := source.ReadFile(filepath.Join("migrations", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("sqlite: read migration %q: %w", entry.Name(), err)
		}
		migrations = append(migrations, migrationFile{version: version, name: entry.Name(), sql: string(content)})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

func applyMigrations(ctx context.Context, db *sql.DB) error {
	return applyMigrationsFrom(ctx, db, migrationsFS)
}

func applyMigrationsFrom(ctx context.Context, db *sql.DB, source migrationSource) error {
	if _, err := db.ExecContext(ctx, createSchemaMigrationsTable); err != nil {
		return fmt.Errorf("sqlite: creando schema_migrations: %w", err)
	}

	migrations, err := loadMigrationsFrom(source)
	if err != nil {
		return err
	}

	applied := make(map[int]bool)
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("sqlite: listando migraciones aplicadas: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("sqlite: read applied version: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyOneMigration(ctx, db, m); err != nil {
			return fmt.Errorf("sqlite: apply migration %s: %w", m.name, err)
		}
	}
	return nil
}

func applyOneMigration(ctx context.Context, db *sql.DB, m migrationFile) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, stmt := range splitStatements(m.sql) {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("ejecutando statement %q: %w", stmt, err)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		m.version, nowRFC3339(),
	); err != nil {
		return err
	}

	return tx.Commit()
}

func splitStatements(script string) []string {
	raw := strings.Split(script, ";")
	out := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}
