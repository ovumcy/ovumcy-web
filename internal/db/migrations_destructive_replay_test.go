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
	for _, expected := range []string{"003_daily_logs_schema_reconcile.sql", "034"} {
		if !strings.Contains(bootErr.Error(), expected) {
			t.Errorf("the refusal must name %q so an operator knows what stopped and why; got %v", expected, bootErr)
		}
	}

	assertSentinelDayIntact(t, databasePath)
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
