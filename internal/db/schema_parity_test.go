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
// across dialects on EVERY axis — to exist on one engine only, or to differ in
// type family, default, nullability or key membership — each with its one-line
// reason.
//
// It is empty on purpose, and an empty allowlist is worth more than a
// pre-populated one: every column the migrations create today exists on both
// engines under the same name and in the same type family, so an exemption-free
// comparison is also what covers the columns a later migration adds. A column
// present on one engine only, or storing text on one engine and a number on the
// other, is a schema divergence until someone writes down why it is not. A
// column that differs on one axis only belongs in that axis's list below, so
// the other axes keep comparing it.
var schemaParityColumnExemptions = map[string]string{}

// schemaParityDefaultExemptions lists "<table>.<column>" pairs whose column
// DEFAULT may differ across dialects, each with its reason. Only the default
// axis is waived; the column's type, nullability and key membership are still
// compared.
//
// An entry must still diverge: once the two trees agree again the entry fails
// the test as stale, so a waiver cannot outlive the asymmetry it was written
// for and quietly cover the next one on the same column.
var schemaParityDefaultExemptions = map[string]string{
	// Migration 035 flipped the default to FALSE on Postgres only; the SQLite
	// half is a declared no-op (deliberateNoOpMigrationFiles["sqlite/035"]).
	// No insert path relies on the column default, which
	// TestNewAccountsCarryAutoPeriodFillOff asserts on both engines.
	"users.auto_period_fill": "migration 035 changed the default on Postgres only; SQLite would need a users rebuild, and every insert path sets the value explicitly",
}

// columnParity is what one column is compared by across dialects: its type
// FAMILY, its normalized DEFAULT, whether it may be NULL and whether it is part
// of the primary key, plus the raw strings the driver reported, carried only so
// a failure can name what it saw.
type columnParity struct {
	Family     string
	RawType    string
	HasDefault bool
	Default    string
	RawDefault string
	Nullable   bool
	PrimaryKey bool
}

// dialectSchema is one engine's migrated schema: table name -> column name ->
// columnParity. Both names are lowercased, because the case an identifier is
// stored in is a dialect artifact (Postgres folds unquoted identifiers to lower
// case) rather than a schema difference.
//
// The RAW SQL type is deliberately out of the comparison: integer/bigint,
// text/varchar, real/double precision and integer/bigserial differ legitimately
// between the engines, so comparing the reported type string would report the
// dialects being dialects. Only the family it normalizes to is compared — see
// columnTypeFamily. Defaults are compared after normalizeParityDefault, for the
// same reason.
//
// Nullability is compared with one normalization: a PRIMARY KEY column counts
// as NOT NULL on both engines. The drivers disagree there and nowhere else —
// comparing ColumnTypes().Nullable() raw failed on exactly the eight key
// columns (users.id, daily_logs.id, symptom_types.id, oidc_identities.id,
// app_state.key, oidc_logout_states.session_id, register_pickup_tokens.nonce,
// schema_migrations.version): SQLite reports a key column as nullable unless it
// is declared NOT NULL, Postgres as not nullable. Key membership is compared as
// its own axis, so the normalization cannot hide a column that is a key on one
// engine only.
type dialectSchema map[string]map[string]columnParity

// indexParity is what one index is compared by across dialects: uniqueness and
// the normalized key list and partial-index predicate, plus the raw definition
// for the failure message.
type indexParity struct {
	Unique bool
	Keys   string
	Where  string
	Raw    string
}

// dialectIndexes is one engine's secondary indexes: table name -> index key ->
// indexParity. An index a migration names (CREATE INDEX) is keyed by its name,
// which both trees spell identically. An index an engine generates for an
// inline UNIQUE constraint is keyed by "unique(<keys>)", because the engines
// name those themselves (sqlite_autoindex_users_1 vs users_email_key).
// Primary-key indexes are left out: SQLite keeps an INTEGER PRIMARY KEY in the
// rowid and builds no index for it, and key membership is already compared per
// column.
type dialectIndexes map[string]map[string]indexParity

// TestMigratedSchemasMatchAcrossDialects holds the two supported engines to the
// same migrated schema: the same tables, the same column names within each
// table, the same type family, default, nullability and key membership for each
// of those columns, and the same secondary indexes over them.
//
// It exists because OpenDatabase applies the embedded migrations OF THE DRIVER
// IT OPENED (applyEmbeddedMigrations → loadEmbeddedMigrations), so the SQLite
// and Postgres sets are two separate bodies of SQL. The existing
// TestEmbeddedMigrationSetsMatchAcrossDialects compares those sets by version
// and file name — that the two trees consist of identically numbered steps —
// and nothing compares what the steps DO. A migration that creates a table in
// one tree and forgets it in the other, writes the same column as INTEGER here
// and TEXT there, gives it a different default, or forgets a UNIQUE index on
// one engine keeps both names and both versions aligned, leaves that test
// green, and still ships two different schemas.
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

	sqliteDatabase := openSQLiteForSchemaParityTest(t)
	postgresDatabase := openPostgresForMigrationBootstrapTest(t, postgresConfig)

	sqliteSchema := readMigratedSchemaForParity(t, sqliteDatabase, string(DriverSQLite))
	postgresSchema := readMigratedSchemaForParity(t, postgresDatabase, string(DriverPostgres))

	if len(sqliteSchema) == 0 || len(postgresSchema) == 0 {
		t.Fatalf("read an empty schema from a migrated database (sqlite=%d tables, postgres=%d tables) — the reader is broken, not the schema", len(sqliteSchema), len(postgresSchema))
	}

	sqliteIndexes := readSQLiteIndexesForParity(t, sqliteDatabase)
	postgresIndexes := readPostgresIndexesForParity(t, postgresDatabase)

	// Positive anchor for the type axis: two drivers both reporting nothing
	// would make every column "unknown()" on both sides and the family
	// comparison would pass while measuring nothing.
	assertParityTypeAxisResolves(t, sqliteSchema, string(DriverSQLite))
	assertParityTypeAxisResolves(t, postgresSchema, string(DriverPostgres))

	// Positive anchors for the default, nullability and index axes, by name:
	// two readers that both lost the default, the NOT NULL flag or the index
	// would agree with each other about nothing.
	assertParityAnchorsResolve(t, sqliteSchema, sqliteIndexes, string(DriverSQLite))
	assertParityAnchorsResolve(t, postgresSchema, postgresIndexes, string(DriverPostgres))

	assertParitySchemaTablesMatch(t, sqliteSchema, postgresSchema)
	assertParitySchemaColumnsMatch(t, sqliteSchema, postgresSchema)
	assertParityIndexesMatch(t, sqliteIndexes, postgresIndexes)
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

// assertParityAnchorsResolve checks, on one engine, values the comparison must
// be able to see for its default, nullability and index axes to mean anything.
// Each anchor is load-bearing in its own right:
//
//   - users.auth_session_version DEFAULT 1 NOT NULL: a session minted for a
//     fresh account carries the version this default leaves, and a NULL there
//     would break the compare-and-set every revocation bumps through.
//   - users.age_group DEFAULT '': an empty-string default is the case where
//     "no default" and "default empty" are easiest to confuse.
//   - idx_users_email_normalized UNIQUE on lower(trim(email)): the login
//     identity both the credential path and the OIDC email match resolve an
//     account by; it is an expression index, which GORM's own index reader
//     cannot read on SQLite and reads without its expression on Postgres.
//   - idx_users_calendar_feed_selector UNIQUE WHERE calendar_feed_selector <>
//     '': the feed lookup key, and the one partial index — the anchor for the
//     predicate normalization.
//   - unique(user_id,date) on daily_logs: the one-entry-per-day rule, an inline
//     UNIQUE constraint whose index each engine names itself.
func assertParityAnchorsResolve(t *testing.T, schema dialectSchema, indexes dialectIndexes, dialect string) {
	t.Helper()

	sessionVersion, ok := schema["users"]["auth_session_version"]
	if !ok || !sessionVersion.HasDefault || sessionVersion.Default != "1" || sessionVersion.Nullable {
		t.Errorf("%s users.auth_session_version read as %+v, want DEFAULT 1 NOT NULL — the default/nullability reader lost a load-bearing value, so their comparison is vacuous", dialect, sessionVersion)
	}
	ageGroup, ok := schema["users"]["age_group"]
	if !ok || !ageGroup.HasDefault || ageGroup.Default != "" {
		t.Errorf("%s users.age_group read as %+v, want DEFAULT '' — an empty-string default must stay distinct from no default", dialect, ageGroup)
	}

	anchors := []struct {
		table string
		key   string
		want  indexParity
	}{
		{table: "users", key: "idx_users_email_normalized", want: indexParity{Unique: true, Keys: "lower(trim(email))"}},
		{table: "users", key: "idx_users_calendar_feed_selector", want: indexParity{Unique: true, Keys: "calendar_feed_selector", Where: "calendar_feed_selector<>''"}},
		{table: "daily_logs", key: "unique(user_id,date)", want: indexParity{Unique: true, Keys: "user_id,date"}},
	}
	for _, anchor := range anchors {
		got, ok := indexes[anchor.table][anchor.key]
		if !ok || got.Unique != anchor.want.Unique || got.Keys != anchor.want.Keys || got.Where != anchor.want.Where {
			t.Errorf("%s index %s.%s read as %+v (present=%v), want unique=%v keys=%q where=%q — the index reader or its normalization lost a load-bearing index, so the index comparison is vacuous", dialect, anchor.table, anchor.key, got, ok, anchor.want.Unique, anchor.want.Keys, anchor.want.Where)
		}
	}
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

// readMigratedSchemaForParity reads the live columns through GORM's migrator
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
			family := columnTypeFamily(rawType)
			rawDefault, hasDefault := columnType.DefaultValue()
			nullable, _ := columnType.Nullable()
			primaryKey, _ := columnType.PrimaryKey()
			columns[columnName] = columnParity{
				Family:     family,
				RawType:    rawType,
				HasDefault: hasDefault,
				Default:    normalizeParityDefault(rawDefault, family),
				RawDefault: rawDefault,
				Nullable:   nullable && !primaryKey,
				PrimaryKey: primaryKey,
			}
		}
		schema[tableName] = columns
	}
	return schema
}

// normalizeParityDefault reduces a driver-reported DEFAULT to the value the two
// engines must agree on. It is built from what ColumnTypes() returns on each
// engine — SQLite echoes the declared expression ('none', 0, CURRENT_TIMESTAMP),
// the Postgres driver reports the value with its quotes and cast already
// stripped (none, false, now()) — so the normalization only has to undo
// spelling: one layer of SQL string quoting, a trailing ::type cast, boolean
// 0/1 vs false/true, and now() vs CURRENT_TIMESTAMP. A string literal keeps its
// case: DEFAULT 'C' and DEFAULT 'c' are different defaults.
func normalizeParityDefault(rawDefault string, family string) string {
	value := strings.TrimSpace(rawDefault)
	if cast := strings.LastIndex(value, "::"); cast > 0 && strings.HasSuffix(value[:cast], "'") {
		value = value[:cast]
	}
	if len(value) >= 2 && strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
		return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
	}

	switch lowered := strings.ToLower(value); {
	case family == "boolean" && (lowered == "0" || lowered == "false"):
		return "false"
	case family == "boolean" && (lowered == "1" || lowered == "true"):
		return "true"
	case family == "temporal" && (lowered == "current_timestamp" || lowered == "now()"):
		return "current_timestamp"
	}
	return value
}

// parityIndexRow is one index as a catalog query reports it. Origin follows
// SQLite's pragma_index_list vocabulary on both engines: "c" for an index a
// statement created, "u" for one backing a UNIQUE constraint, "pk" for one
// backing the primary key.
type parityIndexRow struct {
	TableName  string
	IndexName  string
	Origin     string
	Definition *string
}

// readSQLiteIndexesForParity reads SQLite's indexes from the catalog rather
// than GORM's Migrator().GetIndexes, which fails outright on a table carrying
// an expression index (it scans the expression's NULL column name into a
// string) — so users and symptom_types would be unreadable. A created index is
// read from the CREATE statement SQLite stores verbatim; a constraint-backed
// one has no statement, so its key list comes from pragma_index_info.
func readSQLiteIndexesForParity(t *testing.T, database *gorm.DB) dialectIndexes {
	t.Helper()

	var rows []parityIndexRow
	requireNoErr(t, database.Raw(`
		SELECT m.name AS table_name, il.name AS index_name, il.origin AS origin, i.sql AS definition
		FROM sqlite_master m
		JOIN pragma_index_list(m.name) il
		LEFT JOIN sqlite_master i ON i.type = 'index' AND i.name = il.name
		WHERE m.type = 'table'`).Scan(&rows).Error, "list sqlite indexes")

	indexes := make(dialectIndexes)
	for _, row := range rows {
		switch row.Origin {
		case "pk":
			continue
		case "u":
			var keys []string
			requireNoErr(t, database.Raw(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, row.IndexName).Scan(&keys).Error, "key columns of sqlite index "+row.IndexName)
			addParityIndex(indexes, row.TableName, indexParity{Unique: true, Keys: normalizeParityIndexExpression(strings.Join(keys, ",")), Raw: row.IndexName + " (" + strings.Join(keys, ", ") + ")"}, "")
		default:
			addParityIndexFromDefinition(t, indexes, row, string(DriverSQLite))
		}
	}
	return indexes
}

// readPostgresIndexesForParity reads Postgres's indexes from pg_index rather
// than GORM's Migrator().GetIndexes, which drops an expression key from the
// column list (lower(name) vanishes) and omits the partial predicate, so it
// would compare a different index than the one the migration created.
// pg_get_indexdef gives the full definition for every index, constraint-backed
// or not.
func readPostgresIndexesForParity(t *testing.T, database *gorm.DB) dialectIndexes {
	t.Helper()

	var rows []parityIndexRow
	requireNoErr(t, database.Raw(`
		SELECT c.relname AS table_name, i.relname AS index_name,
			CASE con.contype WHEN 'p' THEN 'pk' WHEN 'u' THEN 'u' ELSE 'c' END AS origin,
			pg_get_indexdef(i.oid) AS definition
		FROM pg_index x
		JOIN pg_class i ON i.oid = x.indexrelid
		JOIN pg_class c ON c.oid = x.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_constraint con ON con.conindid = i.oid AND con.contype IN ('p', 'u')
		WHERE n.nspname = current_schema()`).Scan(&rows).Error, "list postgres indexes")

	indexes := make(dialectIndexes)
	for _, row := range rows {
		if row.Origin == "pk" {
			continue
		}
		addParityIndexFromDefinition(t, indexes, row, string(DriverPostgres))
	}
	return indexes
}

// addParityIndexFromDefinition parses one CREATE INDEX statement into an
// indexParity and files it under its key.
func addParityIndexFromDefinition(t *testing.T, indexes dialectIndexes, row parityIndexRow, dialect string) {
	t.Helper()

	if row.Definition == nil {
		t.Errorf("%s index %s.%s (origin %s) has no definition to compare — the catalog reader cannot see it", dialect, row.TableName, row.IndexName, row.Origin)
		return
	}
	parsed, ok := parseParityIndexDefinition(*row.Definition)
	if !ok {
		t.Errorf("%s index %s.%s has a definition the parity parser cannot read: %q", dialect, row.TableName, row.IndexName, *row.Definition)
		return
	}
	name := ""
	if row.Origin == "c" {
		name = strings.ToLower(row.IndexName)
	}
	addParityIndex(indexes, row.TableName, parsed, name)
}

// addParityIndex files an index under its name, or under "unique(<keys>)" when
// it backs a UNIQUE constraint and so carries an engine-chosen name.
func addParityIndex(indexes dialectIndexes, table string, index indexParity, name string) {
	table = strings.ToLower(table)
	if _, exempt := schemaParityTableExemptions[table]; exempt {
		return
	}
	if indexes[table] == nil {
		indexes[table] = make(map[string]indexParity)
	}
	key := name
	if key == "" {
		key = "unique(" + index.Keys + ")"
	}
	indexes[table][key] = index
}

// parseParityIndexDefinition reads uniqueness, the key list and the partial
// predicate out of a CREATE [UNIQUE] INDEX statement in either engine's
// spelling — SQLite's as written in the migration, Postgres's as
// pg_get_indexdef regenerates it (schema-qualified table, USING btree).
func parseParityIndexDefinition(definition string) (indexParity, bool) {
	collapsed := strings.Join(strings.Fields(definition), " ")
	upper := strings.ToUpper(collapsed)
	on := strings.Index(upper, " ON ")
	if !strings.HasPrefix(upper, "CREATE ") || on < 0 {
		return indexParity{}, false
	}
	rest := collapsed[on+len(" ON "):]
	open := strings.Index(rest, "(")
	if open < 0 {
		return indexParity{}, false
	}
	closing := matchingParityParen(rest, open)
	if closing < 0 {
		return indexParity{}, false
	}

	parsed := indexParity{
		Unique: strings.HasPrefix(upper, "CREATE UNIQUE "),
		Keys:   normalizeParityIndexExpression(rest[open+1 : closing]),
		Raw:    collapsed,
	}
	tail := strings.TrimSpace(rest[closing+1:])
	if strings.HasPrefix(strings.ToUpper(tail), "WHERE ") {
		parsed.Where = normalizeParityIndexExpression(tail[len("WHERE "):])
	}
	return parsed, true
}

// matchingParityParen returns the index of the parenthesis closing the one at
// open, skipping parentheses inside string literals, or -1.
func matchingParityParen(text string, open int) int {
	depth := 0
	quoted := false
	for i := open; i < len(text); i++ {
		switch {
		case text[i] == '\'':
			quoted = !quoted
		case quoted:
		case text[i] == '(':
			depth++
		case text[i] == ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// normalizeParityIndexExpression reduces an index key list or predicate to a
// spelling both engines share. Outside string literals it drops whitespace and
// identifier quotes, lowercases, and removes ::type casts Postgres adds; it
// then rewrites TRIM(BOTH FROM x) to trim(x) and != to <>, and strips
// parentheses enclosing the whole expression. String literals keep their case.
func normalizeParityIndexExpression(expression string) string {
	var builder strings.Builder
	quoted := false
	for i := 0; i < len(expression); i++ {
		char := expression[i]
		switch {
		case char == '\'':
			quoted = !quoted
			builder.WriteByte(char)
		case quoted:
			builder.WriteByte(char)
		case char == ':' && i+1 < len(expression) && expression[i+1] == ':':
			i++
			for i+1 < len(expression) && (isParityIdentifierByte(expression[i+1]) || expression[i+1] == ' ') {
				i++
			}
		case char == '"' || char == ' ' || char == '\t' || char == '\n' || char == '\r':
		default:
			builder.WriteString(strings.ToLower(string(char)))
		}
	}

	normalized := builder.String()
	normalized = strings.ReplaceAll(normalized, "trim(bothfrom", "trim(")
	normalized = strings.ReplaceAll(normalized, "!=", "<>")
	for strings.HasPrefix(normalized, "(") && matchingParityParen(normalized, 0) == len(normalized)-1 {
		normalized = normalized[1 : len(normalized)-1]
	}
	return normalized
}

func isParityIdentifierByte(char byte) bool {
	return char == '_' || (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9')
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
// have, the column names and then each shared column's type family, default,
// nullability and key membership. Tables only one engine has are already named
// by assertParitySchemaTablesMatch, so this pass stays on the intersection
// rather than reporting them twice.
func assertParitySchemaColumnsMatch(t *testing.T, sqliteSchema dialectSchema, postgresSchema dialectSchema) {
	t.Helper()

	defaultExemptionsUsed := make(map[string]bool, len(schemaParityDefaultExemptions))
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

		// The per-column pass runs even when the names already diverged:
		// skipping the table there would let one missing column hide every
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
			if sqliteColumn.Family != postgresColumn.Family {
				t.Errorf("column %s.%s sits in different type families across dialects: sqlite %s (%s), postgres %s (%s) — the two migration trees declare it as different kinds of value; align the declaration in the tree that is wrong, or record the column in schemaParityColumnExemptions with the reason", table, column, sqliteColumn.Family, sqliteColumn.RawType, postgresColumn.Family, postgresColumn.RawType)
			}
			if sqliteColumn.PrimaryKey != postgresColumn.PrimaryKey {
				t.Errorf("column %s.%s is part of the primary key on one engine only: sqlite %v, postgres %v — align the key declaration in the tree that is wrong", table, column, sqliteColumn.PrimaryKey, postgresColumn.PrimaryKey)
			}
			if sqliteColumn.Nullable != postgresColumn.Nullable {
				t.Errorf("column %s.%s differs in nullability across dialects: sqlite nullable=%v, postgres nullable=%v — one tree accepts a NULL the other refuses; align the NOT NULL in the tree that is wrong", table, column, sqliteColumn.Nullable, postgresColumn.Nullable)
			}

			defaultsMatch := sqliteColumn.HasDefault == postgresColumn.HasDefault && sqliteColumn.Default == postgresColumn.Default
			if reason, exempt := schemaParityDefaultExemptions[table+"."+column]; exempt {
				defaultExemptionsUsed[table+"."+column] = true
				if defaultsMatch {
					t.Errorf("column %s.%s carries a default exemption (%s) but its defaults now match (%q on both) — drop the stale entry from schemaParityDefaultExemptions", table, column, reason, sqliteColumn.Default)
				} else {
					t.Logf("waiving default divergence on %s.%s (sqlite %q, postgres %q): %s", table, column, sqliteColumn.RawDefault, postgresColumn.RawDefault, reason)
				}
				continue
			}
			if !defaultsMatch {
				t.Errorf("column %s.%s has different defaults across dialects: sqlite %q (has default %v, raw %q), postgres %q (has default %v, raw %q) — an insert that omits the column stores different values per engine; align the DEFAULT in the tree that is wrong, or record the column in schemaParityDefaultExemptions with the reason", table, column, sqliteColumn.Default, sqliteColumn.HasDefault, sqliteColumn.RawDefault, postgresColumn.Default, postgresColumn.HasDefault, postgresColumn.RawDefault)
			}
		}
	}

	for _, entry := range sortedParityKeys(schemaParityDefaultExemptions) {
		if !defaultExemptionsUsed[entry] {
			t.Errorf("schemaParityDefaultExemptions names %s, which is not a column both engines have — drop or correct the stale entry", entry)
		}
	}
}

// assertParityIndexesMatch compares, within every table, the secondary indexes
// both engines hold: which exist, and for each shared one its uniqueness, key
// list and predicate. A missing UNIQUE index is the divergence that matters
// most — one engine then accepts the duplicate the other refuses.
func assertParityIndexesMatch(t *testing.T, sqliteIndexes dialectIndexes, postgresIndexes dialectIndexes) {
	t.Helper()

	tables := make(map[string]bool)
	for table := range sqliteIndexes {
		tables[table] = true
	}
	for table := range postgresIndexes {
		tables[table] = true
	}
	for _, table := range sortedParityKeys(tables) {
		sqliteTable := sqliteIndexes[table]
		postgresTable := postgresIndexes[table]

		sqliteOnly := parityIndexesMissingFrom(sqliteTable, postgresTable)
		postgresOnly := parityIndexesMissingFrom(postgresTable, sqliteTable)
		if len(sqliteOnly) > 0 || len(postgresOnly) > 0 {
			t.Errorf("table %s holds different indexes across dialects: sqlite-only %v, postgres-only %v — add the missing index to the migration tree that lacks it", table, sqliteOnly, postgresOnly)
		}

		for _, key := range sortedParityKeys(sqliteTable) {
			sqliteIndex := sqliteTable[key]
			postgresIndex, shared := postgresTable[key]
			if !shared {
				continue
			}
			if sqliteIndex.Unique != postgresIndex.Unique || sqliteIndex.Keys != postgresIndex.Keys || sqliteIndex.Where != postgresIndex.Where {
				t.Errorf("index %s.%s differs across dialects: sqlite unique=%v keys=%q where=%q (%s), postgres unique=%v keys=%q where=%q (%s) — align the CREATE INDEX in the tree that is wrong", table, key, sqliteIndex.Unique, sqliteIndex.Keys, sqliteIndex.Where, sqliteIndex.Raw, postgresIndex.Unique, postgresIndex.Keys, postgresIndex.Where, postgresIndex.Raw)
			}
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

// parityIndexesMissingFrom returns the index keys of have that want lacks,
// sorted so the failure message is stable across runs.
func parityIndexesMissingFrom(have map[string]indexParity, want map[string]indexParity) []string {
	missing := make([]string, 0)
	for key := range have {
		if _, present := want[key]; !present {
			missing = append(missing, key)
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
// several divergences in one table report in the same sequence on every run.
func sortedParityColumnNames(columns map[string]columnParity) []string {
	names := make([]string, 0, len(columns))
	for name := range columns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedParityKeys[V any](entries map[string]V) []string {
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
