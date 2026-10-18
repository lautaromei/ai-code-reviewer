//go:build testigo_bootstrap

package sqlite

//go:generate go tool testigo-gen -output zz_testigo_stubs_gen_test.go -type testigoStubsSpec
type testigoStubsSpec struct {
	SQLiteRuntime   sqliteRuntime   `testigo:"spy"`
	MigrationSource migrationSource `testigo:"spy"`
}
