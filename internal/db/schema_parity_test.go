package db

import (
	"fmt"
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

// schemaParityColumnExemptions lists "<table>.<column>" pairs allowed to differ
// across dialects — to exist on one engine only, or to sit in different type
// families — each with its one-line reason.
//
// It is empty on purpose, and an empty allowlist is worth more than a
// pre-populated one: every column the migrations create today exists on both
// engines under the same name and in the same type family, so an exemption-free
// comparison is also what covers the columns a later migration adds. A column
// present on one engine only, or storing text on one engine and a number on the
// other, is a schema divergence until someone writes down why it is not.
var schemaParityColumnExemptions = map[string]string{}

// columnParity is what one column is compared by across dialects: its type
// FAMILY, plus the raw type the driver reported, carried only so a failure can
// name what it saw.
type columnParity struct {
	Family  string
	RawType string
}

// dialectSchema is one engine's migrated schema: table name -> column name ->
// columnParity. Both names are lowercased, because the case an identifier is
// stored in is a dialect artifact (Postgres folds unquoted identifiers to lower
// case) rather than a schema difference.
//
// Two axes are deliberately out of the comparison, each for a measured reason:
//
//   - The RAW SQL type. integer/bigint, text/varchar, real/double precision and
//     integer/bigserial differ legitimately between the engines, so comparing
//     the reported type string would report the dialects being dialects. Only
//     the family it normalizes to is compared — see columnTypeFamily.
//   - NULLABILITY — tried, and dropped. It looks dialect-neutral and is not, as
//     the drivers report it: comparing ColumnTypes().Nullable() failed on eight
//     columns of a schema that does not diverge, and every one of them was a
//     PRIMARY KEY (users.id, daily_logs.id, symptom_types.id,
//     oidc_identities.id, app_state.key, oidc_logout_states.session_id,
//     register_pickup_tokens.nonce, schema_migrations.version) — SQLite reports
//     an implicitly-NOT-NULL key column as nullable, Postgres as not nullable.
//     Shipping eight standing exemptions to keep the check would have hidden a
//     real NOT NULL divergence among them, so nullability stays out. Measured
//     2026-08-20; recorded here so the idea is not re-tried blind.
type dialectSchema map[string]map[string]columnParity

// TestMigratedSchemasMatchAcrossDialects holds the two supported engines to the
// same migrated schema: the same tables, the same column names within each
// table, and the same type family for each of those columns.
//
// It exists because OpenDatabase applies the embedded migrations OF THE DRIVER
// IT OPENED (applyEmbeddedMigrations → loadEmbeddedMigrations), so the SQLite
// and Postgres sets are two separate bodies of SQL. The existing
// TestEmbeddedMigrationSetsMatchAcrossDialects compares those sets by version
// and file name — that the two trees consist of identically numbered steps —
// and nothing compares what the steps DO. A migration that creates a table in
// one tree and forgets it in the other, or writes the same column as INTEGER
// here and TEXT there, keeps both names and both versions aligned, leaves that
// test green, and still ships two different schemas.
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

	// Positive anchor for the type axis: two drivers both reporting nothing
	// would make every column "unknown()" on both sides and the family
	// comparison would pass while measuring nothing.
	assertParityTypeAxisResolves(t, sqliteSchema, string(DriverSQLite))
	assertParityTypeAxisResolves(t, postgresSchema, string(DriverPostgres))

	assertParitySchemaTablesMatch(t, sqliteSchema, postgresSchema)
	assertParitySchemaColumnsMatch(t, sqliteSchema, postgresSchema)
}

// assertParityTypeAxisResolves fails when no column of a whole schema landed in
// a named family — the shape a driver that reports no type at all would
// produce, and the shape in which the family comparison below would agree with
// itself about nothing.
func assertParityTypeAxisResolves(t *testing.T, schema dialectSchema, dialect string) {
	t.Helper()

	for _, table := range sortedParityNames(schema) {
		for _, column := range sortedParityColumnNames(schema[table]) {
			if !strings.HasPrefix(schema[table][column].Family, "unknown(") {
				return
			}
		}
	}
	t.Errorf("no column of the %s schema resolved to a named type family — the driver reported no usable types, so the family comparison is vacuous", dialect)
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

		columns := make(map[string]columnParity, len(columnTypes))
		for _, columnType := range columnTypes {
			columnName := strings.ToLower(columnType.Name())
			if reason, exempt := schemaParityColumnExemptions[tableName+"."+columnName]; exempt {
				t.Logf("skipping %s column %s.%s: %s", dialect, tableName, columnName, reason)
				continue
			}
			rawType := columnType.DatabaseTypeName()
			columns[columnName] = columnParity{Family: columnTypeFamily(rawType), RawType: rawType}
		}
		schema[tableName] = columns
	}
	return schema
}

// columnTypeFamily normalizes a driver-reported SQL type to the coarse family
// the two engines must agree on. It is built from what ColumnTypes() actually
// returns on each engine — SQLite echoes the declared type (TEXT, INTEGER,
// BOOLEAN, DATETIME, REAL), Postgres reports its own spellings (text, int8,
// bool, timestamptz, float8) — not from what the migration files say, so the
// same normalization holds whichever driver produced the string.
//
// Inside a family every difference is ignored: integer vs bigint vs bigserial,
// text vs varchar(255), real vs double precision, boolean however each engine
// spells it. Only crossing a family boundary is a failure, which is the whole
// point — a column written INTEGER in one migration tree and TEXT in the other
// hands the application a number on one engine and a string on the other.
//
// A type matching no family becomes its own "unknown(<normalized>)" family
// rather than being skipped: two engines reporting the same unfamiliar type
// still compare equal, while a divergence involving one stays visible. Silently
// passing an unrecognized type would make the guard quietly narrower than its
// name every time a migration reaches for a type this list has not met.
func columnTypeFamily(rawType string) string {
	normalized := strings.ToLower(strings.TrimSpace(rawType))
	// Drop any size or precision: varchar(255), numeric(10,2), timestamp(6).
	if open := strings.Index(normalized, "("); open >= 0 {
		normalized = strings.TrimSpace(normalized[:open])
	}
	if normalized == "" {
		return "unknown()"
	}

	// Order matters: the probes are substrings, and several type names carry
	// more than one of them. "interval" and "datetime" contain "int" and
	// "date"/"time"; "timestamptz" contains "time". Temporal is therefore
	// tested before integer, and the floating/exact-numeric family before
	// integer so "numeric" is not read as an int.
	families := []struct {
		family string
		probes []string
	}{
		{family: "temporal", probes: []string{"timestamp", "datetime", "date", "time", "interval"}},
		{family: "boolean", probes: []string{"bool"}},
		{family: "text", probes: []string{"text", "char", "clob", "string", "citext"}},
		{family: "binary", probes: []string{"blob", "bytea", "binary"}},
		{family: "float", probes: []string{"float", "double", "real", "numeric", "decimal", "money"}},
		{family: "integer", probes: []string{"int", "serial"}},
	}
	for _, candidate := range families {
		for _, probe := range candidate.probes {
			if strings.Contains(normalized, probe) {
				return candidate.family
			}
		}
	}
	return fmt.Sprintf("unknown(%s)", normalized)
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

// assertParitySchemaColumnsMatch compares, within every table both engines
// have, the column names and then the type family of each shared column.
// Tables only one engine has are already named by
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
		if len(sqliteOnly) > 0 || len(postgresOnly) > 0 {
			t.Errorf("table %s holds different columns across dialects: sqlite-only %v, postgres-only %v — add the missing column to the migration tree that lacks it, or record it in schemaParityColumnExemptions with the reason", table, sqliteOnly, postgresOnly)
		}

		// The family pass runs even when the names already diverged: skipping
		// the table there would let one missing column hide every type
		// divergence beside it, and both come from the same hand-copied
		// statement often enough that the second is exactly what gets missed.
		// Columns absent on one side are named above, so they are stepped over
		// here rather than reported twice.
		for _, column := range sortedParityColumnNames(sqliteColumns) {
			sqliteColumn := sqliteColumns[column]
			postgresColumn, sharedColumn := postgresColumns[column]
			if !sharedColumn {
				continue
			}
			if sqliteColumn.Family == postgresColumn.Family {
				continue
			}
			t.Errorf("column %s.%s sits in different type families across dialects: sqlite %s (%s), postgres %s (%s) — the two migration trees declare it as different kinds of value; align the declaration in the tree that is wrong, or record the column in schemaParityColumnExemptions with the reason", table, column, sqliteColumn.Family, sqliteColumn.RawType, postgresColumn.Family, postgresColumn.RawType)
		}
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
func parityColumnsMissingFrom(have map[string]columnParity, want map[string]columnParity) []string {
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

// sortedParityColumnNames returns a table's column names in a stable order, so
// several family divergences in one table report in the same sequence on every
// run.
func sortedParityColumnNames(columns map[string]columnParity) []string {
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
