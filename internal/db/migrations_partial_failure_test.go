package db

import (
	"path/filepath"
	"strings"
	"testing"
)

// A migration whose SECOND statement of three is rejected by the engine is the
// state applyMigration's per-migration transaction exists for, and no case drove
// it before this file. The neighbouring failure tests stop somewhere else: the
// inspection cases (migrations_inspection_failure_test.go) fail an injected
// CATALOG READ, the destructive-replay cases
// (migrations_destructive_replay_test.go) are refused by the runner's own
// post-condition after every statement has already run, and
// TestOpenSQLiteReleasesTheConnectionWhenMigrationsFail aborts on the ledger
// SELECT before applyMigration is ever reached. None of them is a statement the
// engine refuses in the middle of the list.
//
// The ledger row is the half of that state which decides what the NEXT boot
// does, and no case looked at it at all. It is written after the last statement
// and inside the same transaction, so both halves stand or fall together: a row
// recorded for a migration whose statements did not all run would make every
// later boot skip that migration for good, and the instance would keep serving
// on a schema missing whatever the migration was for — silently, since the
// runner reports success for a version it believes is applied.
//
// Deliberately narrower than "the runner is atomic", and the limit is stated
// here so no reader concludes the broader claim: this drives one synthetic
// migration through the real applyMigration on SQLite, where DDL is
// transactional. Postgres is not measured — the transaction is the runner's,
// not the dialect's, but only SQLite is exercised here.

const (
	// partialFailureFixtureVersion is the version applyFixtureMigration stamps
	// on every synthetic migration, above every embedded one.
	partialFailureFixtureVersion = "900"
	partialFailureFixtureName    = "900_partial_failure.sql"

	// partialFailureFirstTable is created by statement 1, before the failure;
	// partialFailureLastTable by statement 3, after it. The first proves the
	// transaction rolled back what had already been executed, the second that
	// the runner stopped at the failing statement instead of running past it.
	partialFailureFirstTable = "partial_failure_first"
	partialFailureLastTable  = "partial_failure_last"

	// partialFailureMiddleStatement parses and cannot run: an INSERT into a
	// table that does not exist. A statement the engine rejects at execution
	// time is what a migration typo, a renamed column or a constraint the data
	// does not satisfy all look like from the runner's side.
	partialFailureMiddleStatement = "INSERT INTO partial_failure_no_such_table (id) VALUES (1)"
	partialFailureWorkingMiddle   = "CREATE TABLE partial_failure_middle (id INTEGER)"
)

// partialFailureFixtureSQL is the three-statement migration, with middle as its
// second statement.
func partialFailureFixtureSQL(middle string) string {
	return "CREATE TABLE " + partialFailureFirstTable + " (id INTEGER);\n" +
		middle + ";\n" +
		"CREATE TABLE " + partialFailureLastTable + " (id INTEGER);"
}

// TestAMigrationThatFailsPartWayRecordsNeitherItsSchemaChangeNorItsLedgerRow
// runs that migration twice. The failing case is the subject; the case whose
// middle statement works is the anchor, and it is what keeps every assertion
// below able to fail: it proves the fixture's tables do get created and its
// ledger row does get written when the migration completes, so their absence in
// the failing case is the rollback and not the fixture doing nothing.
func TestAMigrationThatFailsPartWayRecordsNeitherItsSchemaChangeNorItsLedgerRow(t *testing.T) {
	t.Run("the middle statement is rejected by the engine", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "partial-failure.db")
		seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

		err := applyFixtureMigration(t, databasePath, partialFailureFixtureName, partialFailureFixtureSQL(partialFailureMiddleStatement))

		// Reported with t.Error rather than t.Fatal: a runner that swallowed the
		// statement error is exactly the shape whose effect on the schema and on
		// the ledger the assertions below measure, and aborting here would hide
		// both.
		if err == nil {
			t.Error("a migration whose statement the engine rejects must fail, got no error")
		} else {
			for _, expected := range []string{partialFailureFixtureName, partialFailureMiddleStatement, "no such table"} {
				if !strings.Contains(err.Error(), expected) {
					t.Errorf("the failure must name %q so an operator can find the statement that broke; got %v", expected, err)
				}
			}
		}

		assertFixtureTablePresence(t, databasePath, partialFailureFirstTable, false,
			"statement 1 ran before the failure and its table is still there: the migration was not rolled back")
		assertFixtureTablePresence(t, databasePath, partialFailureLastTable, false,
			"statement 3 is after the failing one and its table is there: the runner ran past a statement the engine rejected")
		assertLedgerRowCount(t, databasePath, partialFailureFixtureVersion, 0,
			"the migration is recorded as applied although it failed part-way: every later boot will skip it and the schema change is lost for good")
	})

	t.Run("anchor: the same migration with a middle statement that works", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "partial-failure-anchor.db")
		seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

		err := applyFixtureMigration(t, databasePath, partialFailureFixtureName, partialFailureFixtureSQL(partialFailureWorkingMiddle))
		requireNoErr(t, err, "a migration whose statements all succeed must apply")

		assertFixtureTablePresence(t, databasePath, partialFailureFirstTable, true,
			"statement 1 left nothing behind even though the migration succeeded — the failing case above would then be asserting nothing")
		assertFixtureTablePresence(t, databasePath, partialFailureLastTable, true,
			"statement 3 left nothing behind even though the migration succeeded — the failing case above would then be asserting nothing")
		assertLedgerRowCount(t, databasePath, partialFailureFixtureVersion, 1,
			"a migration that applied cleanly was not recorded — the failing case above would then be asserting nothing")
	})
}

// assertFixtureTablePresence reads the schema back through a connection that
// applies no migration, so the reader can never repair what it measures.
func assertFixtureTablePresence(t *testing.T, databasePath string, tableName string, want bool, complaint string) {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	if got := reader.Migrator().HasTable(tableName); got != want {
		t.Errorf("table %s present=%v, want %v: %s", tableName, got, want, complaint)
	}
}

// assertLedgerRowCount counts the schema_migrations rows for one version, again
// through a connection that runs no migration of its own.
func assertLedgerRowCount(t *testing.T, databasePath string, version string, want int64, complaint string) {
	t.Helper()

	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	var count int64
	requireNoErr(t, reader.Raw(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count).Error, "count schema_migrations rows")
	if count != want {
		t.Errorf("schema_migrations holds %d row(s) for version %s, want %d: %s", count, version, want, complaint)
	}
}
