package db

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// dropTableTokenPattern finds the DROP TABLE token pair anywhere in a migration.
// It is deliberately independent of the runner's own anchored detection: the
// sweep below decides for itself which migrations destroy a table and then
// requires the runner to agree about them.
var dropTableTokenPattern = regexp.MustCompile(`(?i)\bDROP\s+TABLE\b`)

// replaySentinelDay is the day row every case here seeds: one value in each
// daily_logs column that a migration AFTER 003 added, so a rebuild that copies
// only the nine columns of migration 001 is visible as data and not only as
// schema.
type replaySentinelDay struct {
	Date            string
	Mood            int
	SexActivity     string
	BBT             float64
	CervicalMucus   string
	CycleStart      bool
	IsUncertain     bool
	CycleFactorKeys string
	PregnancyTest   string
}

func replaySentinel() replaySentinelDay {
	return replaySentinelDay{
		Date:            "2026-03-05",
		Mood:            4,
		SexActivity:     "protected",
		BBT:             36.55,
		CervicalMucus:   "creamy",
		CycleStart:      true,
		IsUncertain:     true,
		CycleFactorKeys: `["stress"]`,
		PregnancyTest:   "positive",
	}
}

// TestNoMissingSchemaMigrationsRowCostsAColumnOrARow deletes each
// schema_migrations row in turn from a fully migrated database, reopens it, and
// requires that the reopen cost neither a column nor a stored value.
//
// The runner re-applies a migration whose ledger row is absent — a restore from
// a backup taken before the row was written, or an operator pruning the table —
// and until this guard the only thing that made a replay safe was
// shouldSkipStatement, which recognizes `ALTER TABLE ... ADD COLUMN` and
// nothing else. A migration that reconciles a table by rebuilding it copies the
// columns ITS OWN version knew about and drops the rest: replaying migration
// 003 on a current database rebuilt daily_logs from the nine columns of
// migration 001 and silently discarded mood, sex_activity, bbt, cervical_mucus,
// cycle_start, is_uncertain, cycle_factor_keys and pregnancy_test — eight
// health columns and every value in them — on a database that was already
// fully migrated.
//
// A case passes on either outcome, because both are non-destructive and which
// one applies is the runner's business: the boot completes, or the boot is
// refused by name. What it may never do is change the schema or the data.
//
// Deliberately narrower than "the schema is unchanged", and the limit is here
// so no reader concludes the broader claim: this compares TABLES, their COLUMN
// names, and the seeded day's stored values. INDEXES are out of scope, and one
// divergence in that axis is known and measured — migrations 001 and 024 both
// end with `CREATE INDEX IF NOT EXISTS idx_daily_logs_date`, which migration 025
// later drops, so replaying either recreates a redundant index. That costs
// write amplification, never a column or a row, and closing it needs a
// different mechanism from this one.
func TestNoMissingSchemaMigrationsRowCostsAColumnOrARow(t *testing.T) {
	migrations, err := loadEmbeddedMigrations(DriverSQLite)
	requireNoErr(t, err, "load embedded migrations")
	if len(migrations) == 0 {
		t.Fatal("loaded no embedded migrations — the sweep would assert nothing")
	}

	bootsCompleted := 0
	bootsRefused := 0

	for _, migration := range migrations {
		expectRefusal := dropTableTokenPattern.MatchString(migration.SQL)

		t.Run("without the row for "+migration.Version, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "replay-"+migration.Version+".db")
			seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

			before := readReplaySchemaAndDays(t, databasePath)
			deleteSchemaMigrationsRows(t, databasePath, migration.Version)

			bootErr := reopenMigratedDatabaseForReplayTest(t, databasePath)
			after := readReplaySchemaAndDays(t, databasePath)

			if bootErr == nil {
				bootsCompleted++
				if expectRefusal {
					t.Errorf(
						"migration %s drops a table, so re-applying it against the current schema must be refused, but the boot completed",
						migration.Name,
					)
				}
			} else {
				bootsRefused++
				if !expectRefusal {
					t.Fatalf(
						"boot failed after removing the schema_migrations row for %s, which drops no table and must simply re-apply: %v",
						migration.Name, bootErr,
					)
				}
				if !strings.Contains(bootErr.Error(), migration.Name) {
					t.Errorf(
						"the refusal must name the migration it stopped so an operator can act on it; got %v",
						bootErr,
					)
				}
			}

			assertReplayStateUnchanged(t, migration, before, after)
		})
	}

	// Anchor both paths: a harness whose reopen never re-applies anything, or
	// one whose reopen always fails, would leave one of these at zero and make
	// every case above agree with itself.
	if bootsCompleted == 0 {
		t.Error("no case completed its boot — the sweep never exercised a re-applied migration")
	}
	if bootsRefused == 0 {
		t.Error("no case was refused — the sweep never exercised the destructive-replay guard")
	}
}

// TestMigration003ReplayOnACurrentSchemaIsRefusedWithTheHealthColumnsIntact is
// the destructive-replay case spelled out: the audit's repro deleted only the
// schema_migrations row for 003, reopened a current database, and watched the
// live mood column disappear. Here the reopen is refused by name and the column
// and its value are still there.
func TestMigration003ReplayOnACurrentSchemaIsRefusedWithTheHealthColumnsIntact(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "replay-003.db")
	seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)
	deleteSchemaMigrationsRows(t, databasePath, "003")

	bootErr := reopenMigratedDatabaseForReplayTest(t, databasePath)
	if bootErr == nil {
		t.Fatal("expected the boot to refuse re-applying migration 003 against a schema that has moved past it")
	}
	// The refusal names the highest version the ledger records, which is the
	// newest migration in the tree. Read it rather than spelling it out: a
	// literal here turns red the day a migration is added, for a reason that
	// has nothing to do with what this guard protects.
	for _, expected := range []string{"003_daily_logs_schema_reconcile.sql", newestEmbeddedMigrationVersion(t)} {
		if !strings.Contains(bootErr.Error(), expected) {
			t.Errorf("the refusal must name %q so an operator knows what stopped and why; got %v", expected, bootErr)
		}
	}

	assertSentinelDayIntact(t, databasePath)
}

// newestEmbeddedMigrationVersion is the version of the last migration in the
// SQLite tree — the one a fully migrated ledger records highest, and therefore
// the one the replay refusal names.
func newestEmbeddedMigrationVersion(t *testing.T) string {
	t.Helper()

	migrations, err := loadEmbeddedMigrations(DriverSQLite)
	if err != nil {
		t.Fatalf("load the embedded SQLite migrations: %v", err)
	}
	if len(migrations) == 0 {
		t.Fatal("the embedded SQLite migration set is empty, so the refusal below would be asserted against nothing")
	}
	newest := migrations[len(migrations)-1].Version
	if newest == "" {
		// strings.Contains(anything, "") is true, so an empty version would
		// make the caller's assertion pass without reading the refusal at all.
		t.Fatal("the newest embedded migration carries no version, which would make the refusal assertion vacuous")
	}
	return newest
}

// TestAWipedSchemaMigrationsLedgerKeepsEveryDailyLogsColumn covers the case the
// version comparison cannot see. When the whole ledger is gone — a data-only
// restore, a dropped bookkeeping table — no LATER migration is recorded either,
// so nothing marks 003 as a replay: the set simply runs again from 001. The
// second half of the guard measures the effect instead of the ledger, inside
// the migration's own transaction, so the rebuild is rolled back with every
// column and value still in place.
func TestAWipedSchemaMigrationsLedgerKeepsEveryDailyLogsColumn(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "replay-wiped-ledger.db")
	seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)
	deleteSchemaMigrationsRows(t, databasePath)

	bootErr := reopenMigratedDatabaseForReplayTest(t, databasePath)
	if bootErr == nil {
		t.Fatal("expected the boot to refuse rebuilding daily_logs from a wiped schema_migrations ledger")
	}
	for _, expected := range []string{"003_daily_logs_schema_reconcile.sql", "daily_logs", "mood"} {
		if !strings.Contains(bootErr.Error(), expected) {
			t.Errorf("the refusal must name %q so an operator knows what it protected; got %v", expected, bootErr)
		}
	}

	assertSentinelDayIntact(t, databasePath)
}

// TestARebuildNarrowingATableIsRefusedInEitherSQLiteIdiom runs synthetic
// migrations through the real applyMigration, because the shape that matters
// most is the one the embedded tree does NOT contain.
//
// SQLite has two ways to rebuild a table. Migrations 003 and 024 use the first:
// create the replacement beside the original, copy, DROP the original, rename
// the replacement onto its name. The second renames first — `ALTER TABLE t
// RENAME TO t_old`, create t afresh, copy back, drop t_old — and it narrows t
// exactly as much, while dropping only a name that did not exist when the
// migration started. A guard that measured the tables a migration textually
// drops saw nothing there: t_old is not in the schema to snapshot, and t is
// never dropped at all. With a ledger lost entirely, which is the case the
// effect half exists for, such a migration would have replayed and narrowed
// the table in silence.
//
// Each idiom is run twice, and the widening case is the anchor: a rebuild that
// preserves every column must still apply, or the guard would be passing by
// refusing everything.
func TestARebuildNarrowingATableIsRefusedInEitherSQLiteIdiom(t *testing.T) {
	const narrowReplacement = `CREATE TABLE %s (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  date DATE NOT NULL
);
INSERT INTO %s (id, user_id, date) SELECT id, user_id, date FROM %s;`

	const wideReplacement = `CREATE TABLE %s (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  date DATE NOT NULL,
  is_period BOOLEAN NOT NULL DEFAULT 0,
  flow TEXT NOT NULL DEFAULT 'none',
  symptom_ids TEXT,
  notes TEXT,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  mood INTEGER NOT NULL DEFAULT 0,
  sex_activity TEXT NOT NULL DEFAULT 'none',
  bbt REAL,
  cervical_mucus TEXT NOT NULL DEFAULT 'none',
  cycle_start BOOLEAN NOT NULL DEFAULT 0,
  is_uncertain BOOLEAN NOT NULL DEFAULT 0,
  cycle_factor_keys TEXT NOT NULL DEFAULT '[]',
  pregnancy_test TEXT NOT NULL DEFAULT 'none'
);
INSERT INTO %s SELECT * FROM %s;`

	// dropFirst is the idiom of migrations 003 and 024; renameFirst is the one
	// the embedded tree does not use and the guard used to miss.
	dropFirst := func(replacement string) string {
		return fmt.Sprintf(replacement, "daily_logs_new", "daily_logs_new", "daily_logs") + `
DROP TABLE daily_logs;
ALTER TABLE daily_logs_new RENAME TO daily_logs;`
	}
	renameFirst := func(replacement string) string {
		return `ALTER TABLE daily_logs RENAME TO daily_logs_old;
` + fmt.Sprintf(replacement, "daily_logs", "daily_logs", "daily_logs_old") + `
DROP TABLE daily_logs_old;`
	}

	for _, testCase := range []struct {
		name          string
		sql           string
		expectRefusal bool
	}{
		{name: "drop-first rebuild that narrows", sql: dropFirst(narrowReplacement), expectRefusal: true},
		{name: "rename-first rebuild that narrows", sql: renameFirst(narrowReplacement), expectRefusal: true},
		{name: "drop-first rebuild that preserves every column", sql: dropFirst(wideReplacement)},
		{name: "rename-first rebuild that preserves every column", sql: renameFirst(wideReplacement)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "rebuild.db")
			seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

			err := applyFixtureMigration(t, databasePath, "900_fixture_rebuild.sql", testCase.sql)

			if !testCase.expectRefusal {
				requireNoErr(t, err, "a rebuild that preserves every column must apply")
				assertSentinelDayIntact(t, databasePath)
				return
			}

			if err == nil {
				t.Fatal("expected the narrowing rebuild to be refused")
			}
			for _, expected := range []string{"900_fixture_rebuild.sql", "daily_logs", "mood"} {
				if !strings.Contains(err.Error(), expected) {
					t.Errorf("the refusal must name %q; got %v", expected, err)
				}
			}
			assertSentinelDayIntact(t, databasePath)
		})
	}
}

// TestRemovingATableNeedsTheMigrationToSayItMeantTo covers the other side of
// the same check: a table that does not come back is a defect when it is the
// middle of a rebuild and the whole point when a migration retires a table for
// good, and only the migration can tell the two apart.
//
// The marker is what says so. It is namespaced, it is not prose, and it names
// the one table it authorizes, so it cannot be written by accident — and a bare
// `DROP TABLE` deliberately does not count, since that is the middle statement
// of every rebuild in the tree.
func TestRemovingATableNeedsTheMigrationToSayItMeantTo(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		sql           string
		expectRefusal bool
	}{
		{
			name:          "an unmarked removal is refused",
			sql:           "DROP TABLE register_pickup_tokens;",
			expectRefusal: true,
		},
		{
			name: "a marker naming a different table does not authorize this one",
			sql: `-- ovumcy:removes-table oidc_logout_states
DROP TABLE register_pickup_tokens;`,
			expectRefusal: true,
		},
		{
			name: "a marker inside prose is not a marker",
			sql: `-- This migration does not use ovumcy:removes-table register_pickup_tokens yet.
DROP TABLE register_pickup_tokens;`,
			expectRefusal: true,
		},
		{
			name: "a marked removal applies",
			sql: `-- 900 retires register_pickup_tokens, whose flow was withdrawn.
-- ovumcy:removes-table register_pickup_tokens
DROP TABLE register_pickup_tokens;`,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "removal.db")
			seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

			err := applyFixtureMigration(t, databasePath, "900_fixture_removal.sql", testCase.sql)

			reader := openSQLiteFileForReplayTest(t, databasePath)
			defer closeReplayDatabase(t, reader)

			if !testCase.expectRefusal {
				requireNoErr(t, err, "a marked removal must apply")
				if reader.Migrator().HasTable("register_pickup_tokens") {
					t.Error("the marked removal was accepted but the table is still there")
				}
				return
			}

			if err == nil {
				t.Fatal("expected the unmarked removal to be refused")
			}
			for _, expected := range []string{"register_pickup_tokens", removedTableMarker} {
				if !strings.Contains(err.Error(), expected) {
					t.Errorf("the refusal must name %q so the author can express the intent; got %v", expected, err)
				}
			}
			if !reader.Migrator().HasTable("register_pickup_tokens") {
				t.Error("the refusal must roll the migration back, but the table is gone")
			}
		})
	}
}

// TestAColumnDisappearanceTheMigrationExplainsStillApplies keeps the column
// half expressible. Now that the snapshot covers every table rather than the
// ones a migration drops, a column that goes missing on purpose would be
// refused along with the accidental ones unless the visible forms of that
// intent are read as the authorization they are.
//
// There are two such forms and both engines support both. A DROP COLUMN
// retires the column. A RENAME COLUMN does not remove anything at all: the
// values are still there under the new name, so refusing it would have the
// guard reporting a loss that did not happen — the mirror of the defect it
// exists to catch — and advising a backup restore for a routine operation.
func TestAColumnDisappearanceTheMigrationExplainsStillApplies(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		sql         string
		goneColumn  string
		addedColumn string
	}{
		{
			name: "an explicit drop",
			sql: `-- 900 retires the shown_period_tip flag.
ALTER TABLE users DROP COLUMN shown_period_tip;`,
			goneColumn: "shown_period_tip",
		},
		{
			name: "a rename, which loses nothing",
			sql: `-- 900 renames usage_goal to usage_intent.
ALTER TABLE users RENAME COLUMN usage_goal TO usage_intent;`,
			goneColumn:  "usage_goal",
			addedColumn: "usage_intent",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			databasePath := filepath.Join(t.TempDir(), "column-intent.db")
			seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

			requireNoErr(t, applyFixtureMigration(
				t, databasePath, "900_fixture_column_intent.sql", testCase.sql,
			), "a column disappearance the migration explains must apply")

			reader := openSQLiteFileForReplayTest(t, databasePath)
			defer closeReplayDatabase(t, reader)

			if reader.Migrator().HasColumn("users", testCase.goneColumn) {
				t.Errorf("the migration was accepted but users.%s is still there", testCase.goneColumn)
			}
			if testCase.addedColumn != "" && !reader.Migrator().HasColumn("users", testCase.addedColumn) {
				t.Errorf("the rename was accepted but users.%s is not there", testCase.addedColumn)
			}
			if !reader.Migrator().HasColumn("users", "email") {
				t.Error("the migration took a column it did not name")
			}
		})
	}
}

// TestARenameDoesNotExcuseAColumnLostBesideIt pins how far the rename hatch
// reaches: it authorizes the one name the statement renames and nothing else.
//
// The fixture renames daily_logs.notes and, in the same migration, rebuilds the
// table from a replacement that omits every other late column — the shape of a
// replay against a newer schema, wearing one legitimate rename. The refusal
// must name what was really lost and must not name the renamed column, or the
// hatch would be reading one explained disappearance as consent for the rest of
// the table.
func TestARenameDoesNotExcuseAColumnLostBesideIt(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "rename-and-lose.db")
	seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

	err := applyFixtureMigration(t, databasePath, "900_fixture_rename_and_lose.sql", `ALTER TABLE daily_logs RENAME COLUMN notes TO note_text;
ALTER TABLE daily_logs RENAME TO daily_logs_old;
CREATE TABLE daily_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  date DATE NOT NULL,
  note_text TEXT
);
INSERT INTO daily_logs (id, user_id, date, note_text)
SELECT id, user_id, date, note_text FROM daily_logs_old;
DROP TABLE daily_logs_old;`)

	if err == nil {
		t.Fatal("expected a rebuild that loses columns beside a rename to be refused")
	}
	if !strings.Contains(err.Error(), "mood") {
		t.Errorf("the refusal must name the column that was actually lost; got %v", err)
	}
	if strings.Contains(err.Error(), "notes") {
		t.Errorf("the refusal must not name the renamed column, whose values are under the new name; got %v", err)
	}

	assertSentinelDayIntact(t, databasePath)
}

// applyFixtureMigration runs one synthetic migration through the real
// applyMigration against a fully migrated database, so a fixture exercises the
// guard exactly as an embedded migration would. The version is above every
// embedded one, so the version-comparison half never fires and what a case
// measures is the effect half alone.
func applyFixtureMigration(t *testing.T, databasePath string, name string, sqlText string) error {
	t.Helper()

	database := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, database)

	return applyMigration(database, embeddedMigration{Version: "900", Order: 900, Name: name, SQL: sqlText})
}

// TestThePostgresMigrationTreeContainsNoTableDrop keeps the sweep's coverage
// claim true of both supported engines.
//
// The sweep above runs on SQLite because that is where the rebuild pattern
// lives: SQLite cannot drop a constraint or a NOT NULL in place, so migrations
// 003 and 024 reconcile daily_logs by rebuilding it, while their Postgres
// counterparts are a `SELECT 1;` and an `ALTER TABLE`. The guard itself is in
// the runner and therefore dialect-neutral, but the sweep that proves it is
// not — so a rebuild landing in the Postgres tree later would be a destructive
// replay no test here watches. This asserts the premise instead of assuming
// it, and needs no container to do so.
func TestThePostgresMigrationTreeContainsNoTableDrop(t *testing.T) {
	migrations, err := loadEmbeddedMigrations(DriverPostgres)
	requireNoErr(t, err, "load embedded postgres migrations")
	if len(migrations) == 0 {
		t.Fatal("loaded no embedded postgres migrations — this would assert nothing")
	}

	for _, migration := range migrations {
		if dropTableTokenPattern.MatchString(migration.SQL) {
			t.Errorf(
				"postgres migration %s drops a table: the destructive-replay sweep runs on SQLite only, so this rebuild would replay unwatched on the other supported engine — extend the sweep to Postgres before landing it",
				migration.Name,
			)
		}
	}
}

// TestParseDropTableStatementSeesPastLeadingComments pins the detection the
// guard rests on: the prose header splitSQLStatements leaves attached to a
// chunk is skipped, comment text is never read as SQL, and a statement that
// drops something other than a table stays unrecognized.
func TestParseDropTableStatementSeesPastLeadingComments(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		statement     string
		expectedTable string
		expectedMatch bool
	}{
		{
			name:          "bare drop",
			statement:     "DROP TABLE daily_logs",
			expectedTable: "daily_logs",
			expectedMatch: true,
		},
		{
			name:          "drop behind the chunk's prose header",
			statement:     "-- 3) Swap old and new tables only after a successful copy.\nDROP TABLE daily_logs",
			expectedTable: "daily_logs",
			expectedMatch: true,
		},
		{
			name:          "if exists form",
			statement:     "DROP TABLE IF EXISTS daily_logs_new",
			expectedTable: "daily_logs_new",
			expectedMatch: true,
		},
		{
			name:          "quoted identifier",
			statement:     `DROP TABLE "daily_logs"`,
			expectedTable: "daily_logs",
			expectedMatch: true,
		},
		{
			name:          "a comment mentioning a drop is not a drop",
			statement:     "-- Rollback: DROP TABLE daily_logs\nCREATE INDEX IF NOT EXISTS idx_daily_logs_user_id ON daily_logs(user_id)",
			expectedMatch: false,
		},
		{
			name:          "dropping an index is not dropping a table",
			statement:     "DROP INDEX IF EXISTS idx_daily_logs_date",
			expectedMatch: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			table, isDropTable := parseDropTableStatement(testCase.statement)
			if isDropTable != testCase.expectedMatch {
				t.Fatalf("parseDropTableStatement(%q) matched=%v, want %v", testCase.statement, isDropTable, testCase.expectedMatch)
			}
			if table != testCase.expectedTable {
				t.Errorf("parseDropTableStatement(%q) table=%q, want %q", testCase.statement, table, testCase.expectedTable)
			}
		})
	}
}

// replayState is what a case compares before and after the reopen: every
// table's column names, and the stored values of the seeded day.
type replayState struct {
	Columns map[string][]string
	Days    []map[string]string
}

// seedFullyMigratedDatabaseWithSentinelDay boots a clean database through
// OpenDatabase — so its schema is the one the real migrations produce — and
// writes one owner and one day carrying a value in every column added after
// migration 003. The connection is closed before returning, so the caller owns
// the file.
func seedFullyMigratedDatabaseWithSentinelDay(t *testing.T, databasePath string) {
	t.Helper()

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	requireNoErr(t, err, "open sqlite for seeding")
	defer closeReplayDatabase(t, database)

	requireNoErr(t, database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
		"replay-owner@example.com", "replay-hash", "owner",
	).Error, "seed owner")

	sentinel := replaySentinel()
	requireNoErr(t, database.Exec(
		`INSERT INTO daily_logs (
			user_id, date, is_period, flow, symptom_ids, notes, created_at, updated_at,
			mood, sex_activity, bbt, cervical_mucus, cycle_start, is_uncertain,
			cycle_factor_keys, pregnancy_test
		) VALUES (
			(SELECT id FROM users WHERE email = ?), ?, 1, 'light', '[1,2]', 'replay-sentinel',
			CURRENT_TIMESTAMP, CURRENT_TIMESTAMP, ?, ?, ?, ?, ?, ?, ?, ?
		)`,
		"replay-owner@example.com",
		sentinel.Date,
		sentinel.Mood,
		sentinel.SexActivity,
		sentinel.BBT,
		sentinel.CervicalMucus,
		sentinel.CycleStart,
		sentinel.IsUncertain,
		sentinel.CycleFactorKeys,
		sentinel.PregnancyTest,
	).Error, "seed sentinel day")
}

// deleteSchemaMigrationsRows removes the named ledger rows, or the whole ledger
// when no version is given. It opens the file directly rather than through
// OpenDatabase, so preparing a case can never itself run a migration.
func deleteSchemaMigrationsRows(t *testing.T, databasePath string, versions ...string) {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	if len(versions) == 0 {
		requireNoErr(t, reader.Exec(`DELETE FROM schema_migrations`).Error, "wipe schema_migrations")
		return
	}
	for _, version := range versions {
		result := reader.Exec(`DELETE FROM schema_migrations WHERE version = ?`, version)
		requireNoErr(t, result.Error, "delete schema_migrations row "+version)
		if result.RowsAffected != 1 {
			t.Fatalf("expected to delete exactly one schema_migrations row for version %s, deleted %d", version, result.RowsAffected)
		}
	}
}

// reopenMigratedDatabaseForReplayTest reopens the database through the real
// boot path and returns whatever the migration runner decided, closing the
// handle when the boot succeeded.
func reopenMigratedDatabaseForReplayTest(t *testing.T, databasePath string) error {
	t.Helper()

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		return err
	}
	closeReplayDatabase(t, database)
	return nil
}

// readReplaySchemaAndDays reads the comparison state through a plain connection
// that applies no migration, so the reader can never repair what it measures.
func readReplaySchemaAndDays(t *testing.T, databasePath string) replayState {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	tables, err := reader.Migrator().GetTables()
	requireNoErr(t, err, "list tables")

	columns := make(map[string][]string, len(tables))
	for _, table := range tables {
		names, err := tableColumnNames(reader, table)
		requireNoErr(t, err, "read columns of "+table)
		sort.Strings(names)
		columns[strings.ToLower(table)] = names
	}

	return replayState{Columns: columns, Days: readDailyLogRowsForReplayTest(t, reader)}
}

// readDailyLogRowsForReplayTest renders every daily_logs row as column ->
// printed value. Reading whole rows rather than named fields is what makes a
// lost column visible: a scan into a struct would leave the field at its zero
// value and report nothing.
func readDailyLogRowsForReplayTest(t *testing.T, reader *gorm.DB) []map[string]string {
	t.Helper()

	rawRows := make([]map[string]any, 0)
	requireNoErr(t, reader.Table("daily_logs").Order("id").Find(&rawRows).Error, "read daily_logs rows")

	rows := make([]map[string]string, 0, len(rawRows))
	for _, rawRow := range rawRows {
		row := make(map[string]string, len(rawRow))
		for column, value := range rawRow {
			row[strings.ToLower(column)] = fmt.Sprintf("%v", value)
		}
		rows = append(rows, row)
	}
	return rows
}

// assertReplayStateUnchanged is the sweep's whole verdict: whatever the runner
// decided, the reopen may not have cost a table, a column or a stored value.
func assertReplayStateUnchanged(t *testing.T, migration embeddedMigration, before replayState, after replayState) {
	t.Helper()

	for table, columnsBefore := range before.Columns {
		columnsAfter, stillThere := after.Columns[table]
		if !stillThere {
			t.Fatalf("reopening without the schema_migrations row for %s removed table %s", migration.Name, table)
		}
		if strings.Join(columnsBefore, ",") != strings.Join(columnsAfter, ",") {
			t.Fatalf(
				"reopening without the schema_migrations row for %s changed the columns of %s\n before: %s\n  after: %s",
				migration.Name, table, strings.Join(columnsBefore, ","), strings.Join(columnsAfter, ","),
			)
		}
	}

	if len(before.Days) != len(after.Days) {
		t.Fatalf(
			"reopening without the schema_migrations row for %s changed the daily_logs row count from %d to %d",
			migration.Name, len(before.Days), len(after.Days),
		)
	}
	for index, dayBefore := range before.Days {
		dayAfter := after.Days[index]
		for column, valueBefore := range dayBefore {
			valueAfter, stillThere := dayAfter[column]
			if !stillThere {
				t.Fatalf("reopening without the schema_migrations row for %s lost daily_logs.%s", migration.Name, column)
			}
			if valueBefore != valueAfter {
				t.Fatalf(
					"reopening without the schema_migrations row for %s rewrote daily_logs.%s from %q to %q",
					migration.Name, column, valueBefore, valueAfter,
				)
			}
		}
	}
}

// assertSentinelDayIntact names the columns a migration-003 replay destroys and
// checks each one still holds what was written, so a failure reads as the data
// loss it is rather than as a schema diff.
func assertSentinelDayIntact(t *testing.T, databasePath string) {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	rows := readDailyLogRowsForReplayTest(t, reader)
	if len(rows) != 1 {
		t.Fatalf("expected the seeded day to survive, found %d daily_logs rows", len(rows))
	}

	sentinel := replaySentinel()
	for column, expected := range map[string]string{
		"mood":           fmt.Sprintf("%d", sentinel.Mood),
		"sex_activity":   sentinel.SexActivity,
		"bbt":            fmt.Sprintf("%v", sentinel.BBT),
		"cervical_mucus": sentinel.CervicalMucus,
		// SQLite stores a boolean as an integer, and the reader prints back
		// what is stored rather than what the seed passed in.
		"cycle_start":       "1",
		"is_uncertain":      "1",
		"cycle_factor_keys": sentinel.CycleFactorKeys,
		"pregnancy_test":    sentinel.PregnancyTest,
	} {
		value, stillThere := rows[0][column]
		if !stillThere {
			t.Errorf("daily_logs.%s is gone — the replay dropped a health column and its values", column)
			continue
		}
		if value != expected {
			t.Errorf("daily_logs.%s = %q, want %q", column, value, expected)
		}
	}
}

// openSQLiteFileForReplayTest opens the database file directly, bypassing
// OpenDatabase and therefore the migration runner.
func openSQLiteFileForReplayTest(t *testing.T, databasePath string) *gorm.DB {
	t.Helper()

	reader, err := gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	requireNoErr(t, err, "open sqlite file directly")
	return reader
}

func closeReplayDatabase(t *testing.T, database *gorm.DB) {
	t.Helper()

	sqlDB, err := database.DB()
	requireNoErr(t, err, "get sql db handle")
	requireNoErr(t, sqlDB.Close(), "close sql db handle")
}

// TestABinaryOlderThanTheLedgerRefusesToStart is the downgrade case. Before the
// guard it was entirely silent: a ledger recording migrations this binary does
// not carry meant the schema was written by a newer release, and the older one
// applied nothing, said nothing, and served requests against conventions it
// does not know — writing rows the next upgrade has to reconcile. The synthetic
// row stands in for that newer release, since the test cannot run a future
// binary.
func TestABinaryOlderThanTheLedgerRefusesToStart(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "downgrade.db")
	seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)
	insertSchemaMigrationsRow(t, databasePath, "994", "994_from_a_newer_release.sql")
	insertSchemaMigrationsRow(t, databasePath, "995", "995_from_a_newer_release.sql")

	bootErr := reopenMigratedDatabaseForReplayTest(t, databasePath)
	if bootErr == nil {
		t.Fatal("expected the boot to refuse a schema written by a newer binary")
	}
	for _, expected := range []string{"994", "995", newestEmbeddedMigrationVersion(t), "Downgrade Caveats"} {
		if !strings.Contains(bootErr.Error(), expected) {
			t.Errorf("the refusal must name %q so an operator knows what stopped and why; got %v", expected, bootErr)
		}
	}

	assertSentinelDayIntact(t, databasePath)

	// The anchor, and the reason this is a version comparison and not a
	// blanket refusal of any unrecognized ledger row: with the future rows gone
	// the same database boots. A row numbered BELOW the newest embedded one is
	// a migration the file set dropped, which the runner has always tolerated,
	// so it must not be read as a downgrade either.
	deleteSchemaMigrationsRows(t, databasePath, "994", "995")
	insertSchemaMigrationsRow(t, databasePath, "000", "000_from_a_dropped_file.sql")
	if err := reopenMigratedDatabaseForReplayTest(t, databasePath); err != nil {
		t.Fatalf("a ledger row below the newest embedded migration is not a downgrade and must still boot: %v", err)
	}
}

// TestRefuseASchemaWrittenByANewerBinaryClassifiesEachLedgerShape judges the
// guard on inputs the test owns, one per rule, so its verdict cannot ride on
// whichever migrations happen to be in the tree today: only a version numbered
// above every embedded one is a downgrade, an unparseable row is not orderable
// and is ignored, and an equal or lower one is an ordinary ledger.
func TestRefuseASchemaWrittenByANewerBinaryClassifiesEachLedgerShape(t *testing.T) {
	migrations := []embeddedMigration{
		{Version: "001", Order: 1, Name: "001_initial.sql"},
		{Version: "002", Order: 2, Name: "002_second.sql"},
	}

	testCases := []struct {
		name        string
		applied     []string
		wantRefusal bool
	}{
		{name: "current ledger", applied: []string{"001", "002"}, wantRefusal: false},
		{name: "partially applied ledger", applied: []string{"001"}, wantRefusal: false},
		{name: "dropped migration file", applied: []string{"001", "002", "000"}, wantRefusal: false},
		{name: "unorderable row", applied: []string{"001", "002", "not-a-number"}, wantRefusal: false},
		{name: "written by a newer binary", applied: []string{"001", "002", "003"}, wantRefusal: true},
	}

	for _, testCase := range testCases {

		t.Run(testCase.name, func(t *testing.T) {
			applied := make(map[string]struct{}, len(testCase.applied))
			for _, version := range testCase.applied {
				applied[version] = struct{}{}
			}

			err := refuseASchemaWrittenByANewerBinary(migrations, applied)
			if testCase.wantRefusal && err == nil {
				t.Fatal("expected a refusal")
			}
			if !testCase.wantRefusal && err != nil {
				t.Fatalf("expected no refusal, got %v", err)
			}
		})
	}
}

func insertSchemaMigrationsRow(t *testing.T, databasePath string, version string, name string) {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	requireNoErr(t, reader.Exec(
		`INSERT INTO schema_migrations(version, name) VALUES (?, ?)`, version, name,
	).Error, "insert schema_migrations row "+version)
}
