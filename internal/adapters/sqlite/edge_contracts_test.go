package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/lautaromei/testigo"
	"github.com/lautaromei/testigo/assert"
)

var databaseUnavailable = errors.New("database unavailable")

type sqliteRuntimeFixture struct {
	database     *sql.DB
	directoryErr error
	openErr      error
	migrationErr error
}

func (f sqliteRuntimeFixture) MakeDirectory(string) error { return f.directoryErr }
func (f sqliteRuntimeFixture) OpenDatabase(string) (*sql.DB, error) {
	return f.database, f.openErr
}
func (f sqliteRuntimeFixture) Migrate(context.Context, *sql.DB) error { return f.migrationErr }

type migrationSourceFixture struct {
	files       fstest.MapFS
	readDirErr  error
	readFileErr error
}

func (f migrationSourceFixture) ReadDir(name string) ([]fs.DirEntry, error) {
	if f.readDirErr != nil {
		return nil, f.readDirErr
	}
	return fs.ReadDir(f.files, name)
}

func (f migrationSourceFixture) ReadFile(name string) ([]byte, error) {
	if f.readFileErr != nil {
		return nil, f.readFileErr
	}
	return fs.ReadFile(f.files, name)
}

func mockedSQLDatabase(t *testing.T, monitorPing bool) (*sql.DB, sqlmock.Sqlmock) {
	t.Helper()
	var (
		database *sql.DB
		mock     sqlmock.Sqlmock
		err      error
	)
	if monitorPing {
		database, mock, err = sqlmock.New(sqlmock.MonitorPingsOption(true))
	} else {
		database, mock, err = sqlmock.New()
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database, mock
}

func mockedDatabase(t *testing.T) (*Database, sqlmock.Sqlmock) {
	t.Helper()
	database, mock := mockedSQLDatabase(t, false)
	return &Database{db: database}, mock
}

func TestSQLiteValueMappings(t *testing.T) {
	for _, scenario := range []struct {
		name  string
		check func(*testing.T)
	}{
		{"formats zero time as empty", func(t *testing.T) { assert.Empty(t, formatTime(time.Time{})) }},
		{"parses empty time as zero", func(t *testing.T) { assert.True(t, parseTime("").IsZero(), "time should be zero") }},
		{"parses RFC3339 seconds", func(t *testing.T) { assert.False(t, parseTime("2026-08-10T12:00:00Z").IsZero(), "time should parse") }},
		{"turns invalid time into zero", func(t *testing.T) { assert.True(t, parseTime("invalid").IsZero(), "time should be zero") }},
		{"turns invalid time pointer into nil", func(t *testing.T) { assert.Nil(t, parseTimePtr("invalid")) }},
		{"formats a present integer pointer", func(t *testing.T) {
			value := int64(7)
			assert.Equal(t, formatInt64Ptr(&value), any(int64(7)))
		}},
		{"turns absent integer into nil", func(t *testing.T) { assert.Nil(t, parseInt64Ptr(sql.NullInt64{})) }},
		{"turns present integer into pointer", func(t *testing.T) { assert.Equal(t, *parseInt64Ptr(sql.NullInt64{Valid: true, Int64: 7}), int64(7)) }},
		{"marshals nil strings as an array", func(t *testing.T) { assert.Equal(t, marshalStrings(nil), "[]") }},
		{"unmarshals empty strings as nil", func(t *testing.T) { assert.Nil(t, unmarshalStrings("")) }},
		{"rejects malformed string arrays", func(t *testing.T) { assert.Nil(t, unmarshalStrings("{")) }},
		{"maps true to one", func(t *testing.T) { assert.Equal(t, boolToInt(true), 1) }},
	} {
		testigo.Run(t, scenario.name, scenario.check)
	}
}

func TestSQLiteDSNMapping(t *testing.T) {
	for _, scenario := range []struct {
		name string
		dsn  string
		want string
	}{
		{"plain path", "var/store.db", "var/store.db"},
		{"file path", "file:var/store.db", "var/store.db"},
		{"query parameters", "file:var/store.db?mode=rwc", "var/store.db"},
		{"memory path", "file::memory:?cache=shared", ":memory:"},
	} {
		testigo.Run(t, scenario.name, func(t *testing.T) {
			assert.Equal(t, filePathFromDSN(scenario.dsn), scenario.want)
		})
	}
}

func TestSQLiteOpenFailures(t *testing.T) {
	testigo.Run(t, "requires a DSN", func(t *testing.T) {
		_, err := Open(context.Background(), "", time.Second)
		assert.ErrorContains(t, err, "DSN is empty")
	})

	testigo.Run(t, "reports an invalid database directory", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "blocker")
		if err := writeTestFile(blocker); err != nil {
			t.Fatal(err)
		}
		_, err := Open(context.Background(), filepath.Join(blocker, "store.db"), time.Second)
		assert.ErrorContains(t, err, "create directory")
	})

	testigo.Run(t, "a nil database closes successfully", func(t *testing.T) {
		assert.NoError(t, (&Database{}).Close())
	})

	testigo.Run(t, "reports connector failure", func(t *testing.T) {
		_, err := openWith(context.Background(), ":memory:", time.Second, sqliteRuntimeFixture{openErr: databaseUnavailable})
		assert.ErrorContains(t, err, "abriendo")
	})

	testigo.Run(t, "reports ping failure", func(t *testing.T) {
		database, mock := mockedSQLDatabase(t, true)
		mock.ExpectPing().WillReturnError(databaseUnavailable)
		_, err := openWith(context.Background(), ":memory:", time.Second, sqliteRuntimeFixture{database: database})
		assert.ErrorContains(t, err, "ping")
	})

	testigo.Run(t, "reports pragma failure", func(t *testing.T) {
		database, mock := mockedSQLDatabase(t, true)
		mock.ExpectPing()
		mock.ExpectExec("PRAGMA").WillReturnError(databaseUnavailable)
		_, err := openWith(context.Background(), ":memory:", time.Second, sqliteRuntimeFixture{database: database})
		assert.ErrorContains(t, err, "aplicando")
	})

	testigo.Run(t, "reports migration failure", func(t *testing.T) {
		database, mock := mockedSQLDatabase(t, true)
		mock.ExpectPing()
		mock.ExpectExec("PRAGMA").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("PRAGMA").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("PRAGMA").WillReturnResult(sqlmock.NewResult(0, 0))
		_, err := openWith(context.Background(), ":memory:", time.Second, sqliteRuntimeFixture{database: database, migrationErr: databaseUnavailable})
		assert.ErrorIs(t, err, databaseUnavailable)
	})
}

func writeTestFile(path string) error {
	return os.WriteFile(path, []byte("blocker"), 0o600)
}

func TestTransactionFailures(t *testing.T) {
	testigo.Run(t, "reports begin failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin().WillReturnError(databaseUnavailable)
		assert.ErrorContains(t, store.withinTx(context.Background(), func(*Database) error { return nil }), "begin transaction")
	})

	testigo.Run(t, "reports rollback failure together with use case failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin()
		mock.ExpectRollback().WillReturnError(databaseUnavailable)
		err := store.withinTx(context.Background(), func(*Database) error { return errors.New("use case failed") })
		assert.ErrorContains(t, err, "rollback also failed")
	})

	testigo.Run(t, "reports commit failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin()
		mock.ExpectCommit().WillReturnError(databaseUnavailable)
		assert.ErrorContains(t, store.withinTx(context.Background(), func(*Database) error { return nil }), "haciendo commit")
	})

	testigo.Run(t, "reuses an existing transaction", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin()
		tx, err := store.db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		assert.NoError(t, (&Database{tx: tx}).withinTx(context.Background(), func(*Database) error { return nil }))
	})
}

func TestMigrationFailures(t *testing.T) {
	testigo.Run(t, "reports unavailable migration source", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		err := applyMigrationsFrom(context.Background(), store.db, migrationSourceFixture{readDirErr: databaseUnavailable})
		assert.ErrorContains(t, err, "leyendo migrations")
	})

	testigo.Run(t, "reports unapplied migration failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"version"}))
		mock.ExpectBegin().WillReturnError(databaseUnavailable)
		source := migrationSourceFixture{files: fstest.MapFS{
			"migrations/001_one.sql": {Data: []byte("SELECT 1")},
		}}
		assert.ErrorContains(t, applyMigrationsFrom(context.Background(), store.db, source), "apply migration")
	})

	testigo.Run(t, "reports schema table creation failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("CREATE TABLE").WillReturnError(databaseUnavailable)
		assert.ErrorContains(t, applyMigrations(context.Background(), store.db), "creando schema_migrations")
	})

	testigo.Run(t, "reports applied migration query failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT version").WillReturnError(databaseUnavailable)
		assert.ErrorContains(t, applyMigrations(context.Background(), store.db), "listando migraciones")
	})

	testigo.Run(t, "reports invalid applied migration value", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow("invalid"))
		assert.ErrorContains(t, applyMigrations(context.Background(), store.db), "read applied version")
	})

	testigo.Run(t, "reports applied migration row failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"version"}).RowError(0, databaseUnavailable).AddRow(1))
		assert.ErrorIs(t, applyMigrations(context.Background(), store.db), databaseUnavailable)
	})

	testigo.Run(t, "skips an already applied migration", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectQuery("SELECT version").WillReturnRows(sqlmock.NewRows([]string{"version"}).AddRow(1).AddRow(2).AddRow(3).AddRow(4).AddRow(5).AddRow(6).AddRow(7).AddRow(8).AddRow(9).AddRow(10).AddRow(11).AddRow(12).AddRow(13).AddRow(14))
		assert.NoError(t, applyMigrations(context.Background(), store.db))
	})
}

func TestMigrationDiscovery(t *testing.T) {
	testigo.Run(t, "rejects a migration without numeric version", func(t *testing.T) {
		source := migrationSourceFixture{files: fstest.MapFS{"migrations/invalid_name.sql": {Data: []byte("SELECT 1")}}}
		_, err := loadMigrationsFrom(source)
		assert.ErrorContains(t, err, "invalid migration filename")
	})

	testigo.Run(t, "reports unreadable migration", func(t *testing.T) {
		source := migrationSourceFixture{
			files:       fstest.MapFS{"migrations/001_one.sql": {Data: []byte("SELECT 1")}},
			readFileErr: databaseUnavailable,
		}
		_, err := loadMigrationsFrom(source)
		assert.ErrorContains(t, err, "read migration")
	})

	testigo.Run(t, "sorts migrations and ignores unrelated entries", func(t *testing.T) {
		source := migrationSourceFixture{files: fstest.MapFS{
			"migrations/002_two.sql": {Data: []byte("SELECT 2")},
			"migrations/001_one.sql": {Data: []byte("SELECT 1")},
			"migrations/readme.md":   {Data: []byte("documentation")},
			"migrations/archive":     {Mode: fs.ModeDir},
		}}
		migrations, err := loadMigrationsFrom(source)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, []int{migrations[0].version, migrations[1].version}, []int{1, 2})
	})
}

func TestSingleMigrationFailures(t *testing.T) {
	migration := migrationFile{version: 1, name: "one.sql", sql: "CREATE TABLE example (id INTEGER);"}

	testigo.Run(t, "reports transaction begin failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin().WillReturnError(databaseUnavailable)
		assert.ErrorIs(t, applyOneMigration(context.Background(), store.db, migration), databaseUnavailable)
	})

	testigo.Run(t, "reports statement failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin()
		mock.ExpectExec("CREATE TABLE").WillReturnError(databaseUnavailable)
		mock.ExpectRollback()
		assert.ErrorContains(t, applyOneMigration(context.Background(), store.db, migration), "ejecutando statement")
	})

	testigo.Run(t, "reports migration journal failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin()
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("INSERT INTO schema_migrations").WillReturnError(databaseUnavailable)
		mock.ExpectRollback()
		assert.ErrorIs(t, applyOneMigration(context.Background(), store.db, migration), databaseUnavailable)
	})

	testigo.Run(t, "reports commit failure", func(t *testing.T) {
		store, mock := mockedDatabase(t)
		mock.ExpectBegin()
		mock.ExpectExec("CREATE TABLE").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("INSERT INTO schema_migrations").WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit().WillReturnError(databaseUnavailable)
		assert.ErrorIs(t, applyOneMigration(context.Background(), store.db, migration), databaseUnavailable)
	})
}
