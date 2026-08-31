package db

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/ovumcy/ovumcy-web/internal/models"
	embeddedmigrations "github.com/ovumcy/ovumcy-web/migrations"
	"gorm.io/gorm"
)

// addColumnTokenPattern finds the ADD COLUMN token pair anywhere in a chunk. It
// is deliberately independent of the runner's own anchored detection: the
// structural guard below decides for itself that a statement is an ADD COLUMN
// and then requires the runner to agree.
var addColumnTokenPattern = regexp.MustCompile(`(?i)\bADD\s+COLUMN\b`)

func TestOpenSQLiteAppliesEmbeddedMigrationsOnCleanDatabase(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-clean.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	assertUsersSchemaReconciled(t, database)
	assertSymptomTypesSchemaReconciled(t, database)
	assertDailyLogsSchemaReconciled(t, database)
	assertOIDCLogoutStateSchemaReconciled(t, database)
	assertAppStateSchema(t, database)
	assertNormalizedEmailIndexExists(t, database)
	assertCalendarFeedSelectorUniqueIndexExists(t, database)
	assertAllEmbeddedMigrationsApplied(t, database)
}

func TestOpenSQLiteUpgradesLegacyInitSchema(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-legacy.db")
	seedLegacyInitSchema(t, databasePath)

	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	assertUsersSchemaReconciled(t, database)
	assertSymptomTypesSchemaReconciled(t, database)
	assertDailyLogsSchemaReconciled(t, database)
	assertOIDCLogoutStateSchemaReconciled(t, database)
	assertNormalizedEmailIndexExists(t, database)
	assertCalendarFeedSelectorUniqueIndexExists(t, database)
	assertAllEmbeddedMigrationsApplied(t, database)

	assertMigratedLegacyUserDefaults(t, database)
	assertMigratedLegacyDailyLogDefaults(t, database)
	assertMigratedLegacyDailyLogDateCanonicalized(t, database)
}

// TestMigration019CanonicalizesNonUTCDateFields locks the on-disk
// rewrite contract of migration 019: legacy rows whose stored date or
// last_period_start carries a non-UTC offset (because they were written
// before the DailyLog BeforeSave hook landed) must be rewritten to
// canonical UTC-midnight TEXT form. The migration is idempotent — a row
// already in canonical form is left at the same value.
func TestMigration019CanonicalizesNonUTCDateFields(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-019.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, last_period_start, created_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		"non-canonical@example.com",
		"test-hash",
		"owner",
		"2026-02-15 00:00:00-05:00",
	).Error; err != nil {
		t.Fatalf("insert non-canonical user: %v", err)
	}

	var nonCanonicalUser struct {
		ID uint `gorm:"column:id"`
	}
	if err := database.Raw(
		`SELECT id FROM users WHERE email = ?`, "non-canonical@example.com",
	).Scan(&nonCanonicalUser).Error; err != nil {
		t.Fatalf("load non-canonical user id: %v", err)
	}

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		nonCanonicalUser.ID,
		"2026-02-20 00:00:00+09:00",
		true,
		"medium",
		"[]",
		"non-canonical-log",
	).Error; err != nil {
		t.Fatalf("insert non-canonical daily log: %v", err)
	}

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		nonCanonicalUser.ID,
		"2026-02-21 00:00:00+00:00",
		false,
		"none",
		"[]",
		"already-canonical-log",
	).Error; err != nil {
		t.Fatalf("insert already-canonical daily log: %v", err)
	}

	if err := database.Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, "019",
	).Error; err != nil {
		t.Fatalf("delete migration 019 record: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	reopened := openSQLiteForMigrationBootstrapTest(t, databasePath)

	assertStoredDateEqualsUTCMidnight(t, reopened,
		`SELECT last_period_start FROM users WHERE email = ?`,
		"non-canonical@example.com",
		time.Date(2026, time.February, 15, 0, 0, 0, 0, time.UTC),
	)

	assertStoredDateEqualsUTCMidnight(t, reopened,
		`SELECT date FROM daily_logs WHERE notes = ?`,
		"non-canonical-log",
		time.Date(2026, time.February, 20, 0, 0, 0, 0, time.UTC),
	)

	assertStoredDateEqualsUTCMidnight(t, reopened,
		`SELECT date FROM daily_logs WHERE notes = ?`,
		"already-canonical-log",
		time.Date(2026, time.February, 21, 0, 0, 0, 0, time.UTC),
	)
}

// TestMigration033PurgesUnattributedOIDCLogoutStates locks the data contract of
// migration 033. An oidc_logout_states row written before migration 031 carries
// a NULL user_id, so no "WHERE user_id = ?" delete can ever match it: its
// id_token_hint outlived the erasure of the account it was minted for, until its
// own TTL expired up to 7 days later. The migration removes exactly those rows
// and leaves every attributed row untouched.
//
// The surviving attributed row is the positive anchor — a migration that emptied
// the whole table would satisfy the "NULL rows are gone" half on its own. The
// re-run also proves runner idempotency: the DELETE lands on a database where it
// has already been applied once.
func TestMigration033PurgesUnattributedOIDCLogoutStates(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-033.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at)
		 VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
		"logout-state-owner@example.com",
		"test-hash",
		"owner",
	).Error; err != nil {
		t.Fatalf("insert logout-state owner: %v", err)
	}

	var owner struct {
		ID uint `gorm:"column:id"`
	}
	if err := database.Raw(
		`SELECT id FROM users WHERE email = ?`, "logout-state-owner@example.com",
	).Scan(&owner).Error; err != nil {
		t.Fatalf("load logout-state owner id: %v", err)
	}
	if owner.ID == 0 {
		t.Fatal("expected non-zero logout-state owner id")
	}

	// Raw SQL on purpose: models.OIDCLogoutState.UserID is a plain uint, so a
	// GORM insert writes 0 where a genuine pre-031 row carries NULL.
	expiresAt := time.Now().Add(time.Hour).UTC()
	for _, row := range []struct {
		sessionID string
		userID    any
	}{
		{sessionID: "pre-031-session", userID: nil},
		{sessionID: "attributed-session", userID: owner.ID},
	} {
		if err := database.Exec(
			`INSERT INTO oidc_logout_states
			   (session_id, user_id, end_session_endpoint, id_token_hint, post_logout_redirect_url, expires_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
			row.sessionID,
			row.userID,
			"https://idp.example.com/logout",
			"id-token-hint",
			"https://app.example.com/",
			expiresAt,
		).Error; err != nil {
			t.Fatalf("insert %s logout state: %v", row.sessionID, err)
		}
	}

	if seeded := countOIDCLogoutStatesWithNullUser(t, database); seeded != 1 {
		t.Fatalf("expected exactly one seeded NULL-user_id logout state, got %d", seeded)
	}

	if err := database.Exec(
		`DELETE FROM schema_migrations WHERE version = ?`, "033",
	).Error; err != nil {
		t.Fatalf("delete migration 033 record: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	reopened := openSQLiteForMigrationBootstrapTest(t, databasePath)

	if remaining := countOIDCLogoutStatesWithNullUser(t, reopened); remaining != 0 {
		t.Fatalf("expected migration 033 to purge every NULL-user_id logout state, %d left", remaining)
	}
	if attributed := countOIDCLogoutStatesBySessionID(t, reopened, "attributed-session"); attributed != 1 {
		t.Fatalf("expected the attributed logout state to survive migration 033, found %d", attributed)
	}
}

// TestEmbeddedSQLiteMigrationAddColumnStatementsAreRecognizedByTheRunner is the
// structural guard behind SQLite migration idempotency. SQLite has no
// `ADD COLUMN IF NOT EXISTS`, so re-running a migration is safe only because the
// runner skips an ADD COLUMN whose column already exists. That skip is driven by
// one anchored pattern over the statement chunks splitSQLStatements produces,
// and those chunks carry the file's prose header, so a detection that cannot see
// past a leading comment silently stops protecting the first ADD COLUMN of every
// migration that opens with one.
//
// Walking the whole embedded set turns that into a structural regression rather
// than a per-file fix: a future migration whose ADD COLUMN sits behind prose is
// caught here instead of at an operator's boot.
func TestEmbeddedSQLiteMigrationAddColumnStatementsAreRecognizedByTheRunner(t *testing.T) {
	migrations, err := loadEmbeddedMigrations(DriverSQLite)
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}

	inspected := 0
	for _, migration := range migrations {
		for index, statement := range splitSQLStatements(migration.SQL) {
			if !statementCarriesAddColumnCode(statement) {
				continue
			}
			inspected++

			tableName, columnName, recognized := parseAddColumnStatement(statement)
			if !recognized {
				t.Errorf(
					"%s statement %d carries an ADD COLUMN the runner does not recognize, so the already-exists skip never applies to it and a re-run fails on a duplicate column: %q",
					migration.Name, index, firstStatementLineForTest(statement),
				)
				continue
			}
			if tableName == "" || columnName == "" {
				t.Errorf(
					"%s statement %d resolved to an empty table/column pair (table=%q column=%q), so the skip would probe the wrong schema object: %q",
					migration.Name, index, tableName, columnName, firstStatementLineForTest(statement),
				)
			}
		}
	}

	// Positive anchor: a walk that matches nothing passes just as well when the
	// detection is dead, so require the set to actually contain ADD COLUMNs.
	if inspected == 0 {
		t.Fatal("expected the SQLite migration set to contain ADD COLUMN statements, found none — this guard would have passed vacuously")
	}
}

// statementCarriesAddColumnCode reports whether a statement chunk contains real
// ADD COLUMN code rather than prose about one. Whole `--` comment lines are
// dropped first, so a header sentence describing the runner's skip does not
// count as a statement.
func statementCarriesAddColumnCode(statement string) bool {
	codeLines := make([]string, 0)
	for _, line := range strings.Split(statement, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		codeLines = append(codeLines, line)
	}
	return addColumnTokenPattern.MatchString(strings.Join(codeLines, "\n"))
}

// firstStatementLineForTest returns the first line of a chunk that is neither
// blank nor a comment, so a failure names the offending SQL instead of dumping a
// forty-line prose header.
func firstStatementLineForTest(statement string) string {
	for _, line := range strings.Split(statement, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return trimmed
	}
	return strings.TrimSpace(statement)
}

// TestMigrationReappliesWhenItsSchemaMigrationsRecordIsMissing covers the boot
// consequence of the guard above. The trigger is narrow but real: a database
// that carries the column while its schema_migrations row is absent — a restore
// from a backup taken before the row was written, or an operator pruning the
// table — makes the runner replay the migration. It must complete instead of
// failing the whole boot on `duplicate column name`.
//
// 032 opens with a prose header, so its ADD COLUMN reaches the detection behind
// a comment. 031's ALTER is bare and is the positive anchor: it exercises the
// same delete-record-and-reopen harness through the path that always matched, so
// a green 032 cannot be credited to a harness that never re-applies anything.
func TestMigrationReappliesWhenItsSchemaMigrationsRecordIsMissing(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		version string
		table   string
		column  string
	}{
		{name: "comment-prefixed ALTER (032)", version: "032", table: "users", column: "calendar_feed_verifier_mac"},
		{name: "bare ALTER (031)", version: "031", table: "oidc_logout_states", column: "user_id"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "ovumcy-reapply-"+testCase.version+".db")
			database := openSQLiteForMigrationBootstrapTest(t, databasePath)

			if !database.Migrator().HasColumn(testCase.table, testCase.column) {
				t.Fatalf("expected %s.%s to exist after a clean boot", testCase.table, testCase.column)
			}

			if err := database.Exec(
				`DELETE FROM schema_migrations WHERE version = ?`, testCase.version,
			).Error; err != nil {
				t.Fatalf("delete migration %s record: %v", testCase.version, err)
			}

			sqlDB, err := database.DB()
			if err != nil {
				t.Fatalf("get sql db handle: %v", err)
			}
			if err := sqlDB.Close(); err != nil {
				t.Fatalf("close sql db: %v", err)
			}

			reopened, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
			if err != nil {
				t.Fatalf(
					"expected migration %s to re-apply cleanly with %s.%s already present, but boot failed — the runner did not recognize its ADD COLUMN as skippable: %v",
					testCase.version, testCase.table, testCase.column, err,
				)
			}
			reopenedSQLDB, err := reopened.DB()
			if err != nil {
				t.Fatalf("get reopened sql db handle: %v", err)
			}
			t.Cleanup(func() { _ = reopenedSQLDB.Close() })

			if !reopened.Migrator().HasColumn(testCase.table, testCase.column) {
				t.Fatalf("expected %s.%s to survive the re-applied migration %s", testCase.table, testCase.column, testCase.version)
			}
			assertAllEmbeddedMigrationsApplied(t, reopened)
		})
	}
}

// TestParseAddColumnStatementSeesPastLeadingComments pins the detection
// semantics directly: leading prose is skipped, comment text is never parsed as
// SQL, and anything that is not an ADD COLUMN stays unrecognized (and therefore
// executes untouched).
func TestParseAddColumnStatementSeesPastLeadingComments(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statement  string
		wantTable  string
		wantColumn string
		wantOK     bool
	}{
		{
			name:       "bare ALTER",
			statement:  "ALTER TABLE users ADD COLUMN week_starts_on TEXT NOT NULL DEFAULT 'sunday'",
			wantTable:  "users",
			wantColumn: "week_starts_on",
			wantOK:     true,
		},
		{
			name:       "ALTER behind a prose header",
			statement:  "-- Week-start display preference.\n--\n-- Mentions ADD COLUMN in prose.\n\nALTER TABLE users ADD COLUMN week_starts_on TEXT",
			wantTable:  "users",
			wantColumn: "week_starts_on",
			wantOK:     true,
		},
		{
			name:       "quoted identifiers normalize",
			statement:  "-- header\n\nALTER TABLE \"users\" ADD COLUMN \"week_starts_on\" TEXT",
			wantTable:  "users",
			wantColumn: "week_starts_on",
			wantOK:     true,
		},
		{
			name:      "comment naming a statement is not one",
			statement: "-- ALTER TABLE users ADD COLUMN never_created TEXT\n-- second line of prose\n",
			wantOK:    false,
		},
		{
			name:      "comment-only chunk without a trailing newline",
			statement: "-- a split inside prose leaves a comment fragment behind",
			wantOK:    false,
		},
		{
			name:      "non-ALTER statement stays unrecognized",
			statement: "-- header\n\nCREATE UNIQUE INDEX IF NOT EXISTS idx_users_calendar_feed_selector ON users (calendar_feed_selector)",
			wantOK:    false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			tableName, columnName, ok := parseAddColumnStatement(testCase.statement)
			if ok != testCase.wantOK {
				t.Fatalf("parseAddColumnStatement() recognized=%t, want %t", ok, testCase.wantOK)
			}
			if tableName != testCase.wantTable || columnName != testCase.wantColumn {
				t.Fatalf("parseAddColumnStatement() = (%q, %q), want (%q, %q)", tableName, columnName, testCase.wantTable, testCase.wantColumn)
			}
		})
	}
}

// TestEmbeddedMigrationSetsMatchAcrossDialects keeps the two dialect trees
// aligned. A migration added to one engine and forgotten in the other is
// otherwise invisible on a machine without the Postgres container: the SQLite
// bootstrap stays green, and the divergence only surfaces where the missing
// version is needed.
//
// It compares LABELS ONLY — version and file name — and nothing here looks at
// what a file does. Two same-named migrations that write opposite effects, or
// one that writes nothing at all where its twin rewrites every row, pass this
// test by construction. The effect axis is covered by two other guards, and the
// broader claim may not be read off this one:
// TestEveryEffectFreeMigrationFileIsDeclaredDeliberate below refuses a file that
// silently does nothing, and TestMigratedSchemasMatchAcrossDialects
// (schema_parity_test.go) compares the schema the two trees actually produce.
func TestEmbeddedMigrationSetsMatchAcrossDialects(t *testing.T) {
	sqliteSet := embeddedMigrationIdentitiesForTest(t, DriverSQLite)
	postgresSet := embeddedMigrationIdentitiesForTest(t, DriverPostgres)

	if len(sqliteSet) == 0 {
		t.Fatal("expected the SQLite migration set to be non-empty")
	}
	if !reflect.DeepEqual(sqliteSet, postgresSet) {
		t.Fatalf("migration sets diverge across engines: sqlite=%v postgres=%v", sqliteSet, postgresSet)
	}
}

func embeddedMigrationIdentitiesForTest(t *testing.T, driver Driver) []string {
	t.Helper()

	migrations, err := loadEmbeddedMigrations(driver)
	if err != nil {
		t.Fatalf("load embedded %s migrations: %v", driver, err)
	}

	identities := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		identities = append(identities, migration.Version+"/"+migration.Name)
	}
	return identities
}

// deliberateNoOpMigrationFiles declares, per file, every embedded migration the
// runner executes without changing anything — keyed "<dialect>/<version>", each
// with the reason that file is allowed to do nothing.
//
// A version exists in both trees under the same name, so a file that does
// nothing is indistinguishable from its twin by name alone; that is exactly the
// hole TestEmbeddedMigrationSetsMatchAcrossDialects leaves open. Silence is what
// this list removes: doing nothing on one engine is often right, but it is never
// something a reader should have to infer from the SQL.
//
// The declaration lives here and NOT as a marker line inside the .sql, because an
// applied migration is immutable — a marker would mean editing a file that has
// already run on every existing installation. What the file itself carries is
// prose; what the guard reads is this list, and every entry paraphrases the
// reason its file already states.
//
// An entry is a hole in "both trees do the same work", so it may only be added
// with the reason the asymmetry is legitimate. A stale entry fails just as an
// undeclared file does: once the file grows a statement with an effect, the
// exemption has to go with it.
var deliberateNoOpMigrationFiles = map[string]string{
	// 003 rebuilds the SQLite daily_logs table to make symptom_ids nullable and
	// to drop the table-level CHECK on flow. The Postgres tree creates
	// daily_logs in that shape to begin with, so there is nothing to reconcile
	// on this engine.
	"postgres/003": "the reconciled daily_logs shape — nullable symptom_ids, no CHECK on flow — is what the Postgres tree already creates, so only SQLite has a table to rebuild",

	// 019 rewrites every stored date to canonical UTC-midnight TEXT. That shape
	// exists only on SQLite, where glebarez stores DATE columns as TEXT and a
	// legacy row may still carry the request locale's offset. This is the pair
	// the identity comparison was green about while one side rewrote every row
	// and the other ran `SELECT 1;`.
	"postgres/019": "Postgres DATE columns hold a calendar day without offset metadata, so the glebarez TEXT-DATE canonicalization has no legacy shape to rewrite here",

	// 035 restates the users.auto_period_fill DEFAULT. SQLite has no ALTER
	// COLUMN, and users is the parent every child foreign key points at, so
	// restating it there would mean a copy/swap of the account table inside the
	// runner's transaction — a real risk to the account rows for a literal
	// nothing reads. The file states this at length; the entry records it.
	"sqlite/035": "SQLite has no ALTER COLUMN and rebuilding users would cascade every child row, while no insert path omits the column — models.DefaultAutoPeriodFill decides what a new account gets, asserted on both engines by TestNewAccountsCarryAutoPeriodFillOff",
}

// bareSelectStatementPattern matches a statement whose first keyword is SELECT.
// Anchored, so an INSERT ... SELECT or a CREATE TABLE ... AS SELECT — both of
// which write — never match on the SELECT they carry further in.
var bareSelectStatementPattern = regexp.MustCompile(`(?is)^SELECT\b`)

// selectIntoStatementPattern spots the one SELECT form that is not a bare read:
// Postgres `SELECT ... INTO <table>` creates a table. Matching INTO anywhere in
// the statement is deliberately over-eager — misreading a writing statement as
// effect-free is the failure that matters here, and the opposite direction only
// costs an entry in the list above.
var selectIntoStatementPattern = regexp.MustCompile(`(?is)\bINTO\b`)

// TestEveryEffectFreeMigrationFileIsDeclaredDeliberate refuses a migration file
// that runs no statement with an effect unless the file is declared in
// deliberateNoOpMigrationFiles with its reason.
//
// This is the effect half the label comparison cannot reach. Two files under one
// version and one name may hold opposite amounts of work — migration 019
// rewrites every stored date on SQLite and is a bare `SELECT 1;` on Postgres —
// and the identity comparison is green about both. Emptiness is the cheapest
// form of divergence to produce by accident (a version added to one tree and
// stubbed out in the other to keep the sets aligned) and the cheapest to state
// deliberately, so it is the axis this guard pins.
//
// It reads the embedded files through the runner's own splitter and comment
// stripper, so what it judges is what the runner would actually execute — not a
// second opinion about the text.
func TestEveryEffectFreeMigrationFileIsDeclaredDeliberate(t *testing.T) {
	assertMigrationEffectClassifierAnswersBothWays(t)

	effectFreeFiles := make(map[string]string)
	totalFiles := 0

	for _, driver := range []Driver{DriverSQLite, DriverPostgres} {
		migrations, err := loadEmbeddedMigrations(driver)
		if err != nil {
			t.Fatalf("load embedded %s migrations: %v", driver, err)
		}
		for _, migration := range migrations {
			totalFiles++
			if migrationRunsNothingWithAnEffect(migration) {
				effectFreeFiles[string(driver)+"/"+migration.Version] = migration.Name
			}
		}
	}

	// Loader sanity, not a classifier anchor: the classifier is anchored on
	// fixtures above, before any of the live trees is read.
	if totalFiles == 0 {
		t.Fatal("read no embedded migration files at all — the loader is broken, not the migrations")
	}
	if len(effectFreeFiles) == totalFiles {
		t.Fatalf("all %d embedded migration files were classified as effect-free — no migration tree can be that, so the reading of the trees is wrong before any of it is judged", totalFiles)
	}

	for _, fileKey := range sortedMigrationKeys(effectFreeFiles) {
		if strings.TrimSpace(deliberateNoOpMigrationFiles[fileKey]) != "" {
			continue
		}
		t.Errorf("migration %s (%s) runs no statement with an effect and is not declared as deliberate: a file that silently does nothing is a divergence from its same-named twin in the other dialect tree until someone writes down why it is not — give the tree that lacks the work its statement, or record the file in deliberateNoOpMigrationFiles with the reason", fileKey, effectFreeFiles[fileKey])
	}

	for _, fileKey := range sortedMigrationKeys(deliberateNoOpMigrationFiles) {
		if _, stillEffectFree := effectFreeFiles[fileKey]; stillEffectFree {
			continue
		}
		t.Errorf("deliberateNoOpMigrationFiles declares %s a deliberate no-op, but that file is no longer one — either the file gained a statement with an effect, or its version was dropped from the tree; remove the entry, so the list keeps naming only live exemptions", fileKey)
	}
}

// assertMigrationEffectClassifierAnswersBothWays anchors
// migrationRunsNothingWithAnEffect on two migrations this test owns — one that
// does nothing, one that adds a column — and refuses to go on unless it answers
// each correctly. The fixtures double as the classifier's definition: this is
// what "runs nothing with an effect" is being taken to mean.
//
// It deliberately reads NEITHER the live trees NOR deliberateNoOpMigrationFiles.
// An earlier version anchored on the live data — "some embedded file must have
// come back effect-free" — gated on the exemption list being non-empty, and the
// stale-exemption loop is precisely what empties that list, one entry at a time,
// as each declared file gains real work. In that end state a classifier
// refactored into always answering "has an effect" would leave effectFreeFiles
// empty, the exemption list empty, and both reporting loops iterating over
// nothing: a green test measuring nothing, with the next `SELECT 1;` twin
// shipping undeclared. A guard's own self-check may not depend on the data it
// judges, so the anchor is fixtures.
func assertMigrationEffectClassifierAnswersBothWays(t *testing.T) {
	t.Helper()

	for _, fixture := range []struct {
		Name        string
		Migration   embeddedMigration
		RunsNothing bool
	}{
		{
			Name: "a file whose only statement is a bare read runs nothing",
			Migration: embeddedMigration{
				Version: "900",
				Order:   900,
				Name:    "900_recorded_for_version_parity.sql",
				// No semicolon in the prose: the runner splits on every `;`,
				// including one inside the leading comment block, so a prose
				// semicolon leaves a fragment behind that is neither comment
				// nor statement. The fixture is written the way a migration
				// file has to be written.
				SQL: "-- Recorded for version parity. This engine has no work to do.\n\nSELECT 1;\n",
			},
			RunsNothing: true,
		},
		{
			Name: "a file that adds a column does not",
			Migration: embeddedMigration{
				Version: "901",
				Order:   901,
				Name:    "901_add_a_column.sql",
				SQL:     "-- Add the column the day editor reads.\n\nALTER TABLE daily_logs ADD COLUMN pregnancy_test TEXT NOT NULL DEFAULT 'none';\n",
			},
			RunsNothing: false,
		},
	} {
		classified := migrationRunsNothingWithAnEffect(fixture.Migration)
		if classified == fixture.RunsNothing {
			continue
		}
		t.Fatalf("classifier anchor %q: migrationRunsNothingWithAnEffect(%s) = %t, want %t — the classifier no longer answers both ways, so every judgement this test makes below it is unmeasured", fixture.Name, fixture.Migration.Name, classified, fixture.RunsNothing)
	}
}

// migrationRunsNothingWithAnEffect reports whether a migration file executes no
// statement that can change schema or data.
//
// The split and the comment strip are the runner's own (splitSQLStatements,
// stripLeadingSQLComments), so a chunk that is nothing but a prose header — what
// a file's trailing comment block becomes after the split — is correctly seen as
// no statement rather than as an unrecognized one. A file with no statement left
// at all is effect-free too: pure prose runs nothing.
//
// Effect-free means every remaining statement is a bare read. Anything the
// classifier does not recognize as a bare read counts as having an effect, so an
// unfamiliar statement can only ever make a file look busier than it is.
func migrationRunsNothingWithAnEffect(migration embeddedMigration) bool {
	for _, statement := range splitSQLStatements(migration.SQL) {
		body := strings.TrimSpace(stripLeadingSQLComments(statement))
		if body == "" {
			continue
		}
		if !bareSelectStatementPattern.MatchString(body) {
			return false
		}
		if selectIntoStatementPattern.MatchString(body) {
			return false
		}
	}
	return true
}

// sortedMigrationKeys returns a keyed migration map's keys in a stable order, so
// several undeclared or stale files report in the same sequence on every run.
func sortedMigrationKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// TestNoEmbeddedMigrationCutsItsOwnProseInHalf holds the one convention every
// migration file in this tree depends on and nothing enforced.
//
// splitSQLStatements splits the raw SQL on `;` WITHOUT stripping comments, so a
// semicolon inside comment text ends the chunk there. What happens next depends
// on what follows it ON THAT LINE, and only one of the two outcomes is a defect:
//
//   - nothing follows it. The chunk that ends is comment-only, both engines
//     accept a comment-only statement, and the next chunk starts on the next
//     line — which is either more comment or the real SQL. Three shipped files
//     already do this at the end of a `-- Rollback: …;` line and are correct.
//   - text follows it. The `--` that opened the comment went with the previous
//     chunk, so the remainder of the sentence starts the next one as bare SQL,
//     and the engine answers `near "…": syntax error` naming words the author
//     wrote as English, on a migration that read fine in review.
//
// The second is one ordinary compound sentence away in every file, now that
// every file opens with a long prose header, and until this sweep the rule
// lived only in the closing paragraph of the files that happened to repeat it.
//
// The fix belongs in the runner or in the file, and this guard deliberately
// takes the file's side: making the splitter comment-aware changes how every
// migration that has already run on every installation is parsed, which is a
// larger blast radius than the convention it would replace. It is also why the
// guard is written to the narrow rule rather than to "no semicolon in prose" —
// applied migrations are immutable, so a sweep that condemned the three
// harmless ones could never be made to pass.
func TestNoEmbeddedMigrationCutsItsOwnProseInHalf(t *testing.T) {
	assertCommentSemicolonScannerAnswersBothWays(t)

	filesRead := 0
	for _, driver := range []Driver{DriverSQLite, DriverPostgres} {
		migrations, err := loadEmbeddedMigrations(driver)
		if err != nil {
			t.Fatalf("load embedded %s migrations: %v", driver, err)
		}
		for _, migration := range migrations {
			filesRead++
			lines := commentSemicolonLines(migration.SQL)
			if len(lines) == 0 {
				continue
			}
			t.Errorf("migration %s/%s cuts a comment line in half on line(s) %v: the runner splits statements on every `;` without stripping comments, so the words after that semicolon start the next chunk as bare SQL and the engine reports a syntax error naming prose — rewrite the sentence without the semicolon, or end the line there", driver, migration.Name, lines)
		}
	}
	if filesRead == 0 {
		t.Fatal("read no embedded migration files at all — the loader is broken, not the migrations")
	}
}

// assertCommentSemicolonScannerAnswersBothWays anchors the scanner on fixtures
// this test owns, before either live tree is read. The set it judges is
// expected to be clean forever, so an anchor taken from the live files would
// stop measuring anything the moment the sweep started passing — which is
// immediately.
func assertCommentSemicolonScannerAnswersBothWays(t *testing.T) {
	t.Helper()

	for _, fixture := range []struct {
		Name    string
		SQL     string
		Flagged []int
	}{
		{
			Name:    "a compound sentence in a prose header is found",
			SQL:     "-- Adds the column; and explains why.\n\nSELECT 1;\n",
			Flagged: []int{1},
		},
		{
			Name:    "ordinary prose is left alone",
			SQL:     "-- Adds the column and explains why.\n\nSELECT 1;\n",
			Flagged: []int{},
		},
		{
			Name:    "a semicolon that ends a comment line is left alone",
			SQL:     "-- Rollback: CREATE INDEX idx ON daily_logs(date);\n\nDROP INDEX idx;\n",
			Flagged: []int{},
		},
		{
			Name:    "a compound sentence in a trailing note is found",
			SQL:     "SELECT 1; -- and a note; with more after it\n",
			Flagged: []int{1},
		},
		{
			Name:    "two dashes inside a string literal are not a comment",
			SQL:     "UPDATE users SET email = '--a;b' WHERE id = 1;\n",
			Flagged: []int{},
		},
	} {
		got := commentSemicolonLines(fixture.SQL)
		if len(got) == len(fixture.Flagged) && (len(got) == 0 || got[0] == fixture.Flagged[0]) {
			continue
		}
		t.Fatalf("scanner anchor %q: commentSemicolonLines() = %v, want %v — the scanner no longer answers both ways, so the sweep below measures nothing", fixture.Name, got, fixture.Flagged)
	}
}

// commentSemicolonLines returns the 1-based lines on which comment text carries
// a `;` with more text after it on the same line — the form that turns the rest
// of the sentence into the next chunk's opening SQL. A semicolon that ends the
// line is not reported: it only closes the chunk early, and what closes is
// comment-only.
//
// A `--` inside a single-quoted literal starts no comment, which is what keeps
// a data-fixing migration from being reported for the contents of a string it
// writes.
func commentSemicolonLines(sqlText string) []int {
	flagged := make([]int, 0)
	for offset, line := range strings.Split(sqlText, "\n") {
		inLiteral := false
		for index := range len(line) {
			if line[index] == '\'' {
				inLiteral = !inLiteral
				continue
			}
			if inLiteral || line[index] != '-' {
				continue
			}
			if index+1 >= len(line) || line[index+1] != '-' {
				continue
			}
			comment := line[index:]
			if cut := strings.Index(comment, ";"); cut >= 0 && strings.TrimSpace(comment[cut+1:]) != "" {
				flagged = append(flagged, offset+1)
			}
			break
		}
	}
	return flagged
}

func countOIDCLogoutStatesWithNullUser(t *testing.T, database *gorm.DB) int64 {
	t.Helper()

	var count int64
	if err := database.Raw(
		`SELECT COUNT(*) FROM oidc_logout_states WHERE user_id IS NULL`,
	).Row().Scan(&count); err != nil {
		t.Fatalf("count NULL-user_id logout states: %v", err)
	}
	return count
}

func countOIDCLogoutStatesBySessionID(t *testing.T, database *gorm.DB, sessionID string) int64 {
	t.Helper()

	var count int64
	if err := database.Raw(
		`SELECT COUNT(*) FROM oidc_logout_states WHERE session_id = ?`, sessionID,
	).Row().Scan(&count); err != nil {
		t.Fatalf("count logout states for session %s: %v", sessionID, err)
	}
	return count
}

// assertStoredDateEqualsUTCMidnight reads the column the query selects
// and asserts the value, parsed by the glebarez driver into time.Time,
// equals the expected UTC-midnight instant. This is format-agnostic
// (the driver reformats DATE columns on read, so byte-equal comparisons
// are unreliable) but instant-strict — a non-UTC offset row resolves to
// a different instant than the canonical UTC-midnight target.
func assertStoredDateEqualsUTCMidnight(t *testing.T, database *gorm.DB, query string, arg any, expected time.Time) {
	t.Helper()

	var stored time.Time
	if err := database.Raw(query, arg).Row().Scan(&stored); err != nil {
		t.Fatalf("scan stored date for %v: %v", arg, err)
	}
	if !stored.Equal(expected) {
		t.Fatalf("expected stored date %s for %v, got %s", expected.Format(time.RFC3339), arg, stored.Format(time.RFC3339))
	}
	if stored.Hour() != 0 || stored.Minute() != 0 || stored.Second() != 0 {
		t.Fatalf("expected midnight time-of-day for %v, got %s", arg, stored.Format(time.RFC3339Nano))
	}
}

func assertMigratedLegacyUserDefaults(t *testing.T, database *gorm.DB) {
	t.Helper()

	var migratedUser struct {
		Email                string `gorm:"column:email"`
		DisplayName          string `gorm:"column:display_name"`
		LocalAuthEnabled     bool   `gorm:"column:local_auth_enabled"`
		AuthSessionVersion   int    `gorm:"column:auth_session_version"`
		OnboardingCompleted  bool   `gorm:"column:onboarding_completed"`
		CycleLength          int    `gorm:"column:cycle_length"`
		PeriodLength         int    `gorm:"column:period_length"`
		LutealPhase          int    `gorm:"column:luteal_phase"`
		AutoPeriodFill       bool   `gorm:"column:auto_period_fill"`
		IrregularCycle       bool   `gorm:"column:irregular_cycle"`
		TrackBBT             bool   `gorm:"column:track_bbt"`
		TemperatureUnit      string `gorm:"column:temperature_unit"`
		TrackCervicalMucus   bool   `gorm:"column:track_cervical_mucus"`
		HideSexChip          bool   `gorm:"column:hide_sex_chip"`
		HideCycleFactors     bool   `gorm:"column:hide_cycle_factors"`
		HideNotesField       bool   `gorm:"column:hide_notes_field"`
		ShowHistoricalPhases bool   `gorm:"column:show_historical_phases"`
		WebhookEnabled       bool   `gorm:"column:webhook_enabled"`
		WebhookNotifyPeriod  bool   `gorm:"column:webhook_notify_period"`
		WebhookNotifyOvul    bool   `gorm:"column:webhook_notify_ovulation"`
		ReminderLeadDays     int    `gorm:"column:reminder_lead_days"`
	}
	if err := database.
		Table("users").
		Select(
			"email",
			"display_name",
			"local_auth_enabled",
			"auth_session_version",
			"onboarding_completed",
			"cycle_length",
			"period_length",
			"luteal_phase",
			"auto_period_fill",
			"irregular_cycle",
			"track_bbt",
			"temperature_unit",
			"track_cervical_mucus",
			"hide_sex_chip",
			"hide_cycle_factors",
			"hide_notes_field",
			"show_historical_phases",
			"webhook_enabled",
			"webhook_notify_period",
			"webhook_notify_ovulation",
			"reminder_lead_days",
		).
		Where("email = ?", "legacy@example.com").
		First(&migratedUser).Error; err != nil {
		t.Fatalf("load migrated legacy user: %v", err)
	}

	assertStringDefault(t, "display_name", migratedUser.DisplayName, "")
	assertBoolDefault(t, "local_auth_enabled", migratedUser.LocalAuthEnabled, true)
	assertIntDefault(t, "auth_session_version", migratedUser.AuthSessionVersion, 1)
	assertBoolDefault(t, "onboarding_completed", migratedUser.OnboardingCompleted, false)
	assertIntDefault(t, "cycle_length", migratedUser.CycleLength, 28)
	assertIntDefault(t, "period_length", migratedUser.PeriodLength, 5)
	assertIntDefault(t, "luteal_phase", migratedUser.LutealPhase, 14)
	assertBoolDefault(t, "auto_period_fill", migratedUser.AutoPeriodFill, true)
	assertBoolDefault(t, "irregular_cycle", migratedUser.IrregularCycle, false)
	assertBoolDefault(t, "track_bbt", migratedUser.TrackBBT, false)
	assertStringDefault(t, "temperature_unit", migratedUser.TemperatureUnit, "c")
	assertBoolDefault(t, "track_cervical_mucus", migratedUser.TrackCervicalMucus, false)
	assertBoolDefault(t, "hide_sex_chip", migratedUser.HideSexChip, false)
	assertBoolDefault(t, "hide_cycle_factors", migratedUser.HideCycleFactors, false)
	assertBoolDefault(t, "hide_notes_field", migratedUser.HideNotesField, false)
	assertBoolDefault(t, "show_historical_phases", migratedUser.ShowHistoricalPhases, false)
	// Webhook notification columns (migration 027) backfill NOT NULL defaults
	// onto the legacy row: delivery off, both per-kind opt-ins on, lead window 3.
	assertBoolDefault(t, "webhook_enabled", migratedUser.WebhookEnabled, false)
	assertBoolDefault(t, "webhook_notify_period", migratedUser.WebhookNotifyPeriod, true)
	assertBoolDefault(t, "webhook_notify_ovulation", migratedUser.WebhookNotifyOvul, true)
	assertIntDefault(t, "reminder_lead_days", migratedUser.ReminderLeadDays, 3)
}

func assertStringDefault(t *testing.T, field string, got string, want string) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s default to be %q, got %q", field, want, got)
	}
}

func assertIntDefault(t *testing.T, field string, got int, want int) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s default to be %d, got %d", field, want, got)
	}
}

func assertBoolDefault(t *testing.T, field string, got bool, want bool) {
	t.Helper()

	if got != want {
		t.Fatalf("expected %s default to be %t, got %t", field, want, got)
	}
}

func assertMigratedLegacyDailyLogDefaults(t *testing.T, database *gorm.DB) {
	t.Helper()

	var migratedLog struct {
		CycleStart      bool     `gorm:"column:cycle_start"`
		IsUncertain     bool     `gorm:"column:is_uncertain"`
		Flow            string   `gorm:"column:flow"`
		Mood            int      `gorm:"column:mood"`
		SexActivity     string   `gorm:"column:sex_activity"`
		BBT             *float64 `gorm:"column:bbt"`
		CervicalMucus   string   `gorm:"column:cervical_mucus"`
		PregnancyTest   string   `gorm:"column:pregnancy_test"`
		CycleFactorKeys string   `gorm:"column:cycle_factor_keys"`
		SymptomIDs      *string  `gorm:"column:symptom_ids"`
		Notes           string   `gorm:"column:notes"`
	}
	if err := database.
		Table("daily_logs").
		Select("cycle_start", "is_uncertain", "flow", "mood", "sex_activity", "bbt", "cervical_mucus", "pregnancy_test", "cycle_factor_keys", "symptom_ids", "notes").
		Where("notes = ?", "legacy-log").
		First(&migratedLog).Error; err != nil {
		t.Fatalf("load migrated legacy daily log: %v", err)
	}

	if migratedLog.CycleStart {
		t.Fatal("expected migrated cycle_start default to be false")
	}
	if migratedLog.IsUncertain {
		t.Fatal("expected migrated is_uncertain default to be false")
	}
	if migratedLog.Flow != "light" {
		t.Fatalf("expected migrated flow=light, got %q", migratedLog.Flow)
	}
	if migratedLog.Mood != 0 {
		t.Fatalf("expected migrated mood default to be 0, got %d", migratedLog.Mood)
	}
	if migratedLog.SexActivity != models.SexActivityNone {
		t.Fatalf("expected migrated sex_activity default to be %q, got %q", models.SexActivityNone, migratedLog.SexActivity)
	}
	// Migration 024 rewrites the legacy `bbt = 0` sentinel to NULL, so a legacy
	// row that never recorded a temperature must migrate to NULL (not measured).
	if migratedLog.BBT != nil {
		t.Fatalf("expected migrated bbt sentinel 0 to become NULL, got %v", *migratedLog.BBT)
	}
	if migratedLog.CervicalMucus != models.CervicalMucusNone {
		t.Fatalf("expected migrated cervical_mucus default to be %q, got %q", models.CervicalMucusNone, migratedLog.CervicalMucus)
	}
	if migratedLog.PregnancyTest != models.PregnancyTestNone {
		t.Fatalf("expected migrated pregnancy_test default to be %q, got %q", models.PregnancyTestNone, migratedLog.PregnancyTest)
	}
	if migratedLog.CycleFactorKeys != "[]" {
		t.Fatalf("expected migrated cycle_factor_keys default to be [], got %q", migratedLog.CycleFactorKeys)
	}
	if migratedLog.SymptomIDs == nil || strings.TrimSpace(*migratedLog.SymptomIDs) != "[1,2]" {
		t.Fatalf("expected migrated symptom_ids to remain [1,2], got %v", migratedLog.SymptomIDs)
	}
}

func assertMigratedLegacyDailyLogDateCanonicalized(t *testing.T, database *gorm.DB) {
	t.Helper()

	expected := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC)
	assertStoredDateEqualsUTCMidnight(t, database, `SELECT date FROM daily_logs WHERE notes = ?`, "legacy-log", expected)
}

func assertOIDCLogoutStateSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	if !database.Migrator().HasTable("oidc_logout_states") {
		t.Fatal("expected oidc_logout_states table to exist after migrations")
	}
	if !database.Migrator().HasColumn("oidc_logout_states", "session_id") {
		t.Fatal("expected oidc_logout_states.session_id column to exist after migrations")
	}
	if !database.Migrator().HasColumn("oidc_logout_states", "expires_at") {
		t.Fatal("expected oidc_logout_states.expires_at column to exist after migrations")
	}
	if !database.Migrator().HasColumn("oidc_logout_states", "user_id") {
		t.Fatal("expected oidc_logout_states.user_id column to exist after migrations (migration 031)")
	}
}

// assertAppStateSchema pins the app_state key/value table created by migration
// 028 (issue #125): the table and its key/value/updated_at columns must exist
// after migrations on this engine.
func assertAppStateSchema(t *testing.T, database *gorm.DB) {
	t.Helper()

	if !database.Migrator().HasTable("app_state") {
		t.Fatal("expected app_state table to exist after migrations")
	}
	for _, column := range []string{"key", "value", "updated_at"} {
		if !database.Migrator().HasColumn("app_state", column) {
			t.Fatalf("expected app_state.%s column to exist after migrations", column)
		}
	}
}

func TestOpenSQLiteMigrationBootstrapIsIdempotent(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-idempotent.db")

	firstOpen, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("first open sqlite: %v", err)
	}
	firstRecords := loadMigrationRecords(t, firstOpen)

	firstSQLDB, err := firstOpen.DB()
	if err != nil {
		t.Fatalf("first open sql db: %v", err)
	}
	if err := firstSQLDB.Close(); err != nil {
		t.Fatalf("close first sql db: %v", err)
	}

	secondOpen := openSQLiteForMigrationBootstrapTest(t, databasePath)
	secondRecords := loadMigrationRecords(t, secondOpen)

	if !reflect.DeepEqual(firstRecords, secondRecords) {
		t.Fatalf("expected migration records to remain unchanged between boots, before=%v after=%v", firstRecords, secondRecords)
	}
}

// TestOpenSQLiteReleasesTheConnectionWhenMigrationsFail locks the ownership
// contract of the open path: the caller owns the connection pool only when a
// handle comes back. The web binary and every operator subcommand close the
// handle they were given and have nothing to close when the open reports an
// error, so a failure that happens AFTER the connection is up has to release the
// pool where it was opened — otherwise the sql.DB, and the SQLite file it holds
// open, survive for the rest of the process. That lifetime is the whole run of a
// subcommand that keeps working after a failed open.
//
// Two observables, because the platforms disagree about what a live handle
// prevents:
//   - removing the database file: on Windows an open handle makes this fail with
//     a sharing violation — which is how the leak first surfaced, as a failed
//     boot test failing a second time while its temp directory was cleaned up —
//     while on Linux an open file unlinks without complaint, so this half proves
//     nothing there;
//   - the WAL sidecars: SQLite deletes `<db>-wal` and `<db>-shm` when the LAST
//     connection to a database closes, so a surviving sidecar is a surviving
//     pool on every platform, and this half carries the test on the Linux CI
//     runner.
//
// The successful open at the end is the positive anchor: it runs both assertions
// against a database that certainly existed and was certainly opened, and passes
// only because the test closed it — so neither observable can report success
// merely for want of a file.
func TestOpenSQLiteReleasesTheConnectionWhenMigrationsFail(t *testing.T) {
	failedPath := filepath.Join(t.TempDir(), "ovumcy-migration-failure.db")
	seedShadowedSchemaMigrationsTable(t, failedPath)

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: failedPath})
	if err == nil {
		if sqlDB, handleErr := database.DB(); handleErr == nil {
			_ = sqlDB.Close()
		}
		t.Fatal("expected a shadowed schema_migrations table to fail the migration runner")
	}
	if !strings.Contains(err.Error(), "apply embedded migrations") {
		t.Fatalf("expected the open to fail while applying migrations, got %v", err)
	}
	if database != nil {
		t.Fatal("expected no handle back from a failed open")
	}

	assertSQLiteConnectionReleased(t, failedPath)

	// Positive anchor: the same two assertions against a successful open, closed
	// by the test itself.
	succeededPath := filepath.Join(t.TempDir(), "ovumcy-migration-success.db")
	succeeded, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: succeededPath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := succeeded.DB()
	if err != nil {
		t.Fatalf("get sql db handle: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	assertSQLiteConnectionReleased(t, succeededPath)
}

// seedShadowedSchemaMigrationsTable creates the database file with a
// schema_migrations table that shadows the runner's own: the name is already
// taken, so the runner's CREATE TABLE IF NOT EXISTS is a no-op and the very next
// statement — the SELECT over the applied versions — fails on the missing
// column. The migration runner therefore fails deterministically on a database
// that opens perfectly, with no seam in production code and without depending on
// the content of any particular migration. The seeding connection is closed
// before returning, so the only handle the file can still be holding is the one
// under test.
func seedShadowedSchemaMigrationsTable(t *testing.T, databasePath string) {
	t.Helper()

	database, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite for seeding: %v", err)
	}
	if err := database.Exec(`CREATE TABLE schema_migrations (id INTEGER PRIMARY KEY)`).Error; err != nil {
		t.Fatalf("seed shadowed schema_migrations table: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open seeding sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close seeding sql db: %v", err)
	}
}

// assertSQLiteConnectionReleased proves nothing still holds the database at
// databasePath open, and consumes the file in the process (it removes it).
func assertSQLiteConnectionReleased(t *testing.T, databasePath string) {
	t.Helper()

	if err := os.Remove(databasePath); err != nil {
		t.Fatalf("expected the database file to be removable once the connection is closed, got %v", err)
	}

	for _, sidecar := range []string{databasePath + "-wal", databasePath + "-shm"} {
		_, err := os.Stat(sidecar)
		if err == nil {
			t.Fatalf("expected %s to be gone: SQLite removes it when the last connection closes, so a surviving sidecar means the pool is still open", filepath.Base(sidecar))
		}
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("stat %s: %v", filepath.Base(sidecar), err)
		}
	}
}

func openSQLiteForMigrationBootstrapTest(t *testing.T, databasePath string) *gorm.DB {
	t.Helper()

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return database
}

func seedLegacyInitSchema(t *testing.T, databasePath string) {
	t.Helper()

	dsn := fmt.Sprintf("%s?_foreign_keys=on&_busy_timeout=5000", databasePath)
	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy sqlite: %v", err)
	}

	initSQL, err := fs.ReadFile(embeddedmigrations.Files, "001_init.sql")
	if err != nil {
		t.Fatalf("read 001 migration: %v", err)
	}
	if err := database.Exec(string(initSQL)).Error; err != nil {
		t.Fatalf("apply 001 migration: %v", err)
	}

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
		"legacy@example.com",
		"legacy-hash",
		"owner",
	).Error; err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}

	var legacyUser struct {
		ID uint `gorm:"column:id"`
	}
	if err := database.Raw(`SELECT id FROM users WHERE email = ?`, "legacy@example.com").Scan(&legacyUser).Error; err != nil {
		t.Fatalf("load legacy user id: %v", err)
	}
	if legacyUser.ID == 0 {
		t.Fatal("expected non-zero legacy user id")
	}

	if err := database.Exec(
		`INSERT INTO daily_logs (user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		legacyUser.ID,
		"2026-01-10",
		true,
		"light",
		"[1,2]",
		"legacy-log",
	).Error; err != nil {
		t.Fatalf("insert legacy daily log: %v", err)
	}

	if database.Migrator().HasTable("schema_migrations") {
		t.Fatal("expected legacy schema to not have schema_migrations table")
	}

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open legacy sql db: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy sql db: %v", err)
	}
}

func assertUsersSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	expectedColumns := []string{
		"display_name",
		"onboarding_completed",
		"cycle_length",
		"period_length",
		"luteal_phase",
		"auto_period_fill",
		"auth_session_version",
		"local_auth_enabled",
		"irregular_cycle",
		"track_bbt",
		"temperature_unit",
		"track_cervical_mucus",
		"hide_sex_chip",
		"hide_cycle_factors",
		"hide_notes_field",
		"show_historical_phases",
		"last_period_start",
		"totp_secret",
		"totp_enabled",
		"totp_last_used_step",
		"timezone",
		// Webhook notification settings (migration 027, issue #124).
		"webhook_enabled",
		"webhook_url",
		"webhook_notify_period",
		"webhook_notify_ovulation",
		"webhook_period_last_sent_cycle_start",
		"webhook_ovulation_last_sent_cycle_start",
		"reminder_lead_days",
		// Webhook revocation epoch (migration 038): the monotonic per-owner
		// counter the pre-delivery watermark claim pins, so a notify pass that
		// snapshotted a configuration the owner has since revoked cannot send.
		"webhook_config_version",
		// Calendar (.ics) feed subscription token (migration 029) plus the keyed
		// verifier authenticator that superseded bcrypt on the verify path
		// (migration 032; the bcrypt column stays for pre-032 rows and rollback).
		"calendar_feed_selector",
		"calendar_feed_verifier_hash",
		"calendar_feed_verifier_mac",
	}

	for _, column := range expectedColumns {
		if !database.Migrator().HasColumn("users", column) {
			t.Fatalf("expected users.%s column to exist after migrations", column)
		}
	}
}

func assertSymptomTypesSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	if !database.Migrator().HasColumn("symptom_types", "archived_at") {
		t.Fatal("expected symptom_types.archived_at column to exist after migrations")
	}
}

// dailyLogsTrackingContract is the per-day tracking shape both migration trees
// have to produce: every column the day editor and the export path read, and
// whether the schema may leave it NULL.
//
// It is one list, read by both engines through assertDailyLogsTrackingContract,
// because the two per-dialect assertions it replaces had already drifted apart:
// the SQLite side asserted pregnancy_test, symptom_ids nullability and
// cycle_factor_keys NOT NULL, and the Postgres side asserted none of the three
// — so the Postgres tree could have shipped without pregnancy_test, or with
// symptom_ids made NOT NULL, and both bootstraps stayed green. A shared list is
// also what carries the next column added here onto both engines at once.
//
// Nullability is asserted per engine rather than compared between them:
// TestMigratedSchemasMatchAcrossDialects deliberately leaves that axis out,
// because the drivers report a PRIMARY KEY column's nullability differently.
// None of the columns below is a key, and both drivers report all of them
// identically (measured 2026-08-21 on the migrated schema of each engine).
var dailyLogsTrackingContract = []struct {
	Column  string
	NotNull bool
}{
	{Column: "mood", NotNull: true},
	{Column: "sex_activity", NotNull: true},
	// bbt is nullable on purpose: NULL means "not measured", the sentinel-free
	// replacement for the stored 0 that migration 024 retired.
	{Column: "bbt", NotNull: false},
	{Column: "cervical_mucus", NotNull: true},
	{Column: "pregnancy_test", NotNull: true},
	// symptom_ids stays nullable — migration 003 rebuilds the SQLite table for
	// exactly that, and the insert probe in assertPostgresDailyLogsSchemaReconciled
	// writes NULL into it.
	{Column: "symptom_ids", NotNull: false},
	{Column: "cycle_factor_keys", NotNull: true},
	{Column: "cycle_start", NotNull: true},
	{Column: "is_uncertain", NotNull: true},
}

// assertDailyLogsTrackingContract checks the shared contract against one live
// migrated schema, through GORM's migrator rather than a dialect-specific
// PRAGMA or information_schema query, so one body serves both engines and a
// difference it reports cannot be an artifact of two different queries.
//
// The driver's own nullability answer is required, not assumed: ColumnTypes
// returns whether it knows, and a driver that does not know would otherwise
// leave every column silently reading as nullable.
func assertDailyLogsTrackingContract(t *testing.T, database *gorm.DB, dialect Driver) {
	t.Helper()

	columnTypes, err := database.Migrator().ColumnTypes("daily_logs")
	if err != nil {
		t.Fatalf("read %s daily_logs column types: %v", dialect, err)
	}

	nullable := make(map[string]bool, len(columnTypes))
	nullabilityReported := make(map[string]bool, len(columnTypes))
	for _, columnType := range columnTypes {
		columnName := strings.ToLower(strings.TrimSpace(columnType.Name()))
		isNullable, reported := columnType.Nullable()
		nullable[columnName] = isNullable
		nullabilityReported[columnName] = reported
	}

	for _, expected := range dailyLogsTrackingContract {
		if _, exists := nullable[expected.Column]; !exists {
			t.Errorf("expected %s daily_logs.%s column to exist after migrations", dialect, expected.Column)
			continue
		}
		if !nullabilityReported[expected.Column] {
			t.Errorf("the %s driver reported no nullability for daily_logs.%s, so that half of the contract went unmeasured", dialect, expected.Column)
			continue
		}
		if nullable[expected.Column] != !expected.NotNull {
			t.Errorf("expected %s daily_logs.%s to be %s after migrations, got %s",
				dialect, expected.Column,
				describeNullability(!expected.NotNull), describeNullability(nullable[expected.Column]))
		}
	}
}

func describeNullability(isNullable bool) string {
	if isNullable {
		return "nullable"
	}
	return "not null"
}

// assertDailyLogsSchemaReconciled checks the SQLite tree's daily_logs against
// the shared dailyLogsTrackingContract — the same list the Postgres bootstrap
// runs — and then the one thing only this engine has to show: that migration
// 003's rebuild really dropped the table-level CHECK on flow, which is readable
// off sqlite_master and nowhere else.
func assertDailyLogsSchemaReconciled(t *testing.T, database *gorm.DB) {
	t.Helper()

	assertDailyLogsTrackingContract(t, database, DriverSQLite)

	tableDefinition := loadSQLiteObjectSQL(t, database, "table", "daily_logs")
	normalized := strings.ToLower(strings.Join(strings.Fields(tableDefinition), ""))
	if strings.Contains(normalized, "check(flowin(") {
		t.Fatalf("expected daily_logs flow CHECK constraint to be removed, got %q", tableDefinition)
	}
}

func assertNormalizedEmailIndexExists(t *testing.T, database *gorm.DB) {
	t.Helper()

	indexSQL := loadSQLiteObjectSQL(t, database, "index", "idx_users_email_normalized")
	definition := strings.ToLower(strings.Join(strings.Fields(indexSQL), ""))
	if definition == "" {
		t.Fatal("expected normalized email index definition to exist")
	}
	if !strings.Contains(definition, "lower(trim(email))") {
		t.Fatalf("expected normalized email index to use lower(trim(email)), got %q", indexSQL)
	}
}

// assertCalendarFeedSelectorUniqueIndexExists locks migration 029's PARTIAL
// UNIQUE index on users.calendar_feed_selector: the by-selector feed lookup
// relies on it to resolve exactly one row, and cross-owner uniqueness of an armed
// selector is a correctness invariant of the token scheme. The index must be
// partial (predicate on non-empty selector) so every feed-off owner can share the
// empty-string zero value without colliding — a plain unique index would reject
// the second feed-off insert. SQLite-only (reads sqlite_master); the Postgres
// bootstrap proves column presence via the shared assertUsersSchemaReconciled,
// and its clean-boot insert path would fail if the partial predicate were missing.
func assertCalendarFeedSelectorUniqueIndexExists(t *testing.T, database *gorm.DB) {
	t.Helper()

	indexSQL := loadSQLiteObjectSQL(t, database, "index", "idx_users_calendar_feed_selector")
	definition := strings.ToLower(strings.Join(strings.Fields(indexSQL), ""))
	if definition == "" {
		t.Fatal("expected calendar feed selector index definition to exist")
	}
	if !strings.Contains(definition, "uniqueindex") {
		t.Fatalf("expected calendar feed selector index to be UNIQUE, got %q", indexSQL)
	}
	if !strings.Contains(definition, "calendar_feed_selector") {
		t.Fatalf("expected calendar feed selector index to key on calendar_feed_selector, got %q", indexSQL)
	}
	// The partial predicate is what lets multiple feed-off rows coexist. Assert
	// it is present so a future edit cannot silently drop it and reintroduce the
	// empty-string collision.
	if !strings.Contains(definition, "wherecalendar_feed_selector<>''") {
		t.Fatalf("expected calendar feed selector index to be PARTIAL on non-empty selector, got %q", indexSQL)
	}
}

func assertAllEmbeddedMigrationsApplied(t *testing.T, database *gorm.DB) {
	t.Helper()

	expectedVersions := embeddedMigrationVersionsForTest(t)
	actualVersions := make([]string, 0)

	var rows []struct {
		Version string `gorm:"column:version"`
	}
	if err := database.Raw(`SELECT version FROM schema_migrations ORDER BY version ASC`).Scan(&rows).Error; err != nil {
		t.Fatalf("load applied migration versions: %v", err)
	}
	for _, row := range rows {
		actualVersions = append(actualVersions, row.Version)
	}

	if !reflect.DeepEqual(expectedVersions, actualVersions) {
		t.Fatalf("unexpected applied migration versions: expected=%v actual=%v", expectedVersions, actualVersions)
	}
}

type migrationRecord struct {
	Version   string `gorm:"column:version"`
	Name      string `gorm:"column:name"`
	AppliedAt string `gorm:"column:applied_at"`
}

func loadMigrationRecords(t *testing.T, database *gorm.DB) []migrationRecord {
	t.Helper()

	records := make([]migrationRecord, 0)
	if err := database.Raw(
		`SELECT version, name, applied_at FROM schema_migrations ORDER BY version ASC`,
	).Scan(&records).Error; err != nil {
		t.Fatalf("load migration records: %v", err)
	}
	return records
}

func loadSQLiteObjectSQL(t *testing.T, database *gorm.DB, objectType string, objectName string) string {
	t.Helper()

	var row struct {
		SQL string `gorm:"column:sql"`
	}
	if err := database.Raw(
		`SELECT sql FROM sqlite_master WHERE type = ? AND name = ?`,
		objectType,
		objectName,
	).Scan(&row).Error; err != nil {
		t.Fatalf("load sqlite master sql for %s %s: %v", objectType, objectName, err)
	}
	return row.SQL
}

func embeddedMigrationVersionsForTest(t *testing.T) []string {
	return embeddedMigrationVersionsForDriverTest(t, DriverSQLite)
}

func embeddedMigrationVersionsForDriverTest(t *testing.T, driver Driver) []string {
	t.Helper()

	migrations, err := loadEmbeddedMigrations(driver)
	if err != nil {
		t.Fatalf("load embedded migrations: %v", err)
	}

	versions := make([]string, 0, len(migrations))
	for _, migration := range migrations {
		versions = append(versions, migration.Version)
	}
	return versions
}
