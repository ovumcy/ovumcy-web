package db

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// schemaParityTableExemptions lists tables allowed to exist in one dialect's
// migrated schema and not in the other, each with the one-line reason that
// makes the asymmetry legitimate.
//
// Its single entry is engine bookkeeping, not schema: no migration creates it.
// An entry here is a hole in the guarantee that the two supported engines run
// the same schema, so it may only be added together with the reason the table
// belongs to one engine alone (an engine-internal bookkeeping table, a
// dialect-specific implementation of a feature the other engine gets for
// free). Every table the migrations themselves create exists on both engines
// today, and keeping this list to engine internals is what makes the
// comparison cover the tables a later migration adds.
//
// schema_migrations is deliberately NOT here: the runner creates it on both
// engines from the same statement, so it is compared like any other table.
var schemaParityTableExemptions = map[string]string{
	// SQLite maintains this counter table itself for AUTOINCREMENT keys; no
	// migration creates it, and Postgres keeps the equivalent state in
	// sequences owned by the serial columns, which are not tables.
	"sqlite_sequence": "engine-internal AUTOINCREMENT bookkeeping SQLite creates on its own; Postgres holds the equivalent in column-owned sequences",
}

// schemaParityColumnExemptions lists "<table>.<column>" pairs allowed to exist
// in one dialect and not the other, each with its one-line reason.
//
// It is empty on purpose, and an empty allowlist is worth more than a
// pre-populated one: every column the migrations create today exists on both
// engines under the same name, so an exemption-free comparison is also what
// covers the columns a later migration adds. A column present on one engine
// only is a schema divergence until someone writes down why it is not.
var schemaParityColumnExemptions = map[string]string{}

// dialectSchema is one engine's migrated schema: table name -> set of column
// names. Both names are lowercased, because the case an identifier is stored
// in is a dialect artifact (Postgres folds unquoted identifiers to lower case)
// rather than a schema difference.
//
// Names are all this comparison holds the dialects to, and the boundary is not
// laziness on two counts:
//
//   - The SQL TYPE differs legitimately — integer/bigint, text/varchar, and the
//     two boolean representations — so comparing types would report the
//     dialects being dialects as divergence.
//   - NULLABILITY looks dialect-neutral and is not, at least not as the drivers
//     report it. Measured 2026-08-20 against the current schema: comparing
//     ColumnTypes().Nullable() failed on eight columns, and every one of them
//     was a PRIMARY KEY (users.id, daily_logs.id, symptom_types.id,
//     oidc_identities.id, app_state.key, oidc_logout_states.session_id,
//     register_pickup_tokens.nonce, schema_migrations.version) — SQLite reports
//     an implicitly-NOT-NULL key column as nullable, Postgres as not nullable.
//     That is a driver artifact on a schema that does not diverge, so
//     nullability stays out rather than shipping eight standing exemptions that
//     would hide a real NOT NULL divergence among them.
type dialectSchema map[string]map[string]struct{}

// TestMigratedSchemasMatchAcrossDialects holds the two supported engines to the
// same migrated schema: the same tables, and the same column names within each
// table.
//
// It exists because OpenDatabase applies the embedded migrations OF THE DRIVER
// IT OPENED (applyEmbeddedMigrations → loadEmbeddedMigrations), so the SQLite
// and Postgres sets are two separate bodies of SQL. The existing
// TestEmbeddedMigrationSetsMatchAcrossDialects compares those sets by version
// and file name — that the two trees consist of identically numbered steps —
// and nothing compares what the steps DO. A migration that creates a table or a
// column in one tree and forgets it in the other keeps both names and both
// versions aligned, leaves that test green, and still ships two different
// schemas.
//
// Everything derived from the live schema then quietly narrows on one engine:
// the account-erasure sweep in delete_account_completeness_test.go derives its
// user-scoped tables from the user_id column, so a table existing on one engine
// only drops out of that sweep on the other without any test saying so.
//
// The WHOLE test skips when docker is absent, which is unusual in this package
// and deliberate here: the assertion IS the comparison of two schemas, so there
// is no SQLite-only half that could prove anything on its own. Read the skip as
// "this guard was not measured on this host", never as a vacuous pass — it is
// measured wherever the Postgres container runs.
func TestMigratedSchemasMatchAcrossDialects(t *testing.T) {
	// First, so a host without docker skips before opening anything.
	postgresConfig := startPostgresTestConfig(t)

	sqliteSchema := readMigratedSchemaForParity(t, openSQLiteForSchemaParityTest(t), string(DriverSQLite))
	postgresSchema := readMigratedSchemaForParity(t, openPostgresForMigrationBootstrapTest(t, postgresConfig), string(DriverPostgres))

	if len(sqliteSchema) == 0 || len(postgresSchema) == 0 {
		t.Fatalf("read an empty schema from a migrated database (sqlite=%d tables, postgres=%d tables) — the reader is broken, not the schema", len(sqliteSchema), len(postgresSchema))
	}

	assertParitySchemaTablesMatch(t, sqliteSchema, postgresSchema)
	assertParitySchemaColumnsMatch(t, sqliteSchema, postgresSchema)
}

// openSQLiteForSchemaParityTest opens a throwaway SQLite database through
// OpenDatabase, so the schema under comparison is the one the real migrations
// produce rather than one this test built.
func openSQLiteForSchemaParityTest(t *testing.T) *gorm.DB {
	t.Helper()

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "schema-parity.db")})
	requireNoErr(t, err, "open sqlite")
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database
}

// readMigratedSchemaForParity reads the live schema through GORM's migrator
// (GetTables/ColumnTypes) rather than a dialect-specific sqlite_master or
// information_schema query, so one reader serves both engines and a divergence
// it reports cannot be an artifact of two different queries.
func readMigratedSchemaForParity(t *testing.T, database *gorm.DB, dialect string) dialectSchema {
	t.Helper()

	migrator := database.Migrator()
	tables, err := migrator.GetTables()
	requireNoErr(t, err, "list "+dialect+" tables")

	schema := make(dialectSchema, len(tables))
	for _, table := range tables {
		tableName := strings.ToLower(table)
		if reason, exempt := schemaParityTableExemptions[tableName]; exempt {
			t.Logf("skipping %s table %s: %s", dialect, tableName, reason)
			continue
		}

		columnTypes, err := migrator.ColumnTypes(table)
		requireNoErr(t, err, "column types of "+dialect+" table "+table)

		columns := make(map[string]struct{}, len(columnTypes))
		for _, columnType := range columnTypes {
			columnName := strings.ToLower(columnType.Name())
			if reason, exempt := schemaParityColumnExemptions[tableName+"."+columnName]; exempt {
				t.Logf("skipping %s column %s.%s: %s", dialect, tableName, columnName, reason)
				continue
			}
			columns[columnName] = struct{}{}
		}
		schema[tableName] = columns
	}
	return schema
}

// assertParitySchemaTablesMatch reports every table one engine has and the
// other does not, in both directions and in one failure, so a divergence is
// read off the message instead of bisected over repeated runs.
func assertParitySchemaTablesMatch(t *testing.T, sqliteSchema dialectSchema, postgresSchema dialectSchema) {
	t.Helper()

	sqliteOnly := parityTablesMissingFrom(sqliteSchema, postgresSchema)
	postgresOnly := parityTablesMissingFrom(postgresSchema, sqliteSchema)
	if len(sqliteOnly) == 0 && len(postgresOnly) == 0 {
		return
	}

	t.Errorf("migrated schemas hold different tables: sqlite-only %v, postgres-only %v — the two migration trees create different tables; add the missing statement to the tree that lacks it, or record the table in schemaParityTableExemptions with the reason", sqliteOnly, postgresOnly)
}

// assertParitySchemaColumnsMatch compares column names within every table both
// engines have. Tables only one engine has are already named by
// assertParitySchemaTablesMatch, so this pass stays on the intersection rather
// than reporting them twice.
func assertParitySchemaColumnsMatch(t *testing.T, sqliteSchema dialectSchema, postgresSchema dialectSchema) {
	t.Helper()

	for _, table := range sortedParityNames(sqliteSchema) {
		postgresColumns, shared := postgresSchema[table]
		if !shared {
			continue
		}
		sqliteColumns := sqliteSchema[table]

		sqliteOnly := parityColumnsMissingFrom(sqliteColumns, postgresColumns)
		postgresOnly := parityColumnsMissingFrom(postgresColumns, sqliteColumns)
		if len(sqliteOnly) == 0 && len(postgresOnly) == 0 {
			continue
		}

		t.Errorf("table %s holds different columns across dialects: sqlite-only %v, postgres-only %v — add the missing column to the migration tree that lacks it, or record it in schemaParityColumnExemptions with the reason", table, sqliteOnly, postgresOnly)
	}
}

// parityTablesMissingFrom returns the tables of have that want lacks, sorted so
// the failure message is stable across runs.
func parityTablesMissingFrom(have dialectSchema, want dialectSchema) []string {
	missing := make([]string, 0)
	for table := range have {
		if _, present := want[table]; !present {
			missing = append(missing, table)
		}
	}
	sort.Strings(missing)
	return missing
}

// parityColumnsMissingFrom returns the columns of have that want lacks, sorted
// so the failure message is stable across runs.
func parityColumnsMissingFrom(have map[string]struct{}, want map[string]struct{}) []string {
	missing := make([]string, 0)
	for column := range have {
		if _, present := want[column]; !present {
			missing = append(missing, column)
		}
	}
	sort.Strings(missing)
	return missing
}

// sortedParityNames returns the schema's table names in a stable order, so the
// per-table comparison reports in the same sequence on every run.
func sortedParityNames(schema dialectSchema) []string {
	tables := make([]string, 0, len(schema))
	for table := range schema {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables
}
