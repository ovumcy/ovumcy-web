package db

import (
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"
)

// A migration whose statements are rejected part-way through the list is the
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
// Three things have to be true afterwards, and the fixture is built so that each
// one is separately observable:
//
//   - the SCHEMA the migration had already changed is back (its first statement
//     creates a table);
//   - the DATA it had already rewritten is back (its second statement overwrites
//     a health field of the seeded sentinel day, which is why this file seeds
//     one at all — a rollback that reverts a fixture's own two tables while
//     leaving a rewritten health row behind is the failure worth catching, and
//     assertSentinelDayIntactOn is the sibling helper that names it);
//   - the LEDGER is untouched — no row for this migration, and no row lost or
//     gained anywhere else in it. The second half is not decoration: a rollback
//     that reverted everything above while dropping the rows of the migrations
//     that ARE applied leaves a state worse than the one under test, since the
//     next boot re-applies the whole set against a schema that already has it.
//
// The ledger row is what decides that next boot. It is written after the last
// statement and inside the same transaction, so a row recorded for a migration
// whose statements did not all run would make every later boot skip it for
// good, and the instance would keep serving on a schema missing whatever the
// migration was for — silently, since the runner reports success for a version
// it believes is applied.
//
// Postgres is deliberately not measured here, and the reason is not "SQLite is
// what this file happens to open". The transaction is the RUNNER's — one
// dialect-neutral `database.Transaction` in applyMigration — and both supported
// engines have transactional DDL, so the SQLite arm measures the runner itself.
// A Postgres arm would cost a container per case on every run of this package
// and would measure the same Go statement through an engine that ADDITIONALLY
// refuses every command issued after a failed one inside a transaction block
// (SQLSTATE 25P02, until the block is rolled back): a green there could not
// distinguish the runner's rollback from the engine's own refusal, which makes
// it the weaker of the two arms on exactly the axis under test. Add the arm if
// the runner ever stops wrapping a migration in one transaction, or if an
// engine without transactional DDL is supported.

const (
	// partialFailureFixtureName is the ONE place the fixture's version is
	// written down. partialFailureFixtureMigration reads it back out of this
	// name with the runner's own migrationFilePattern, and the assertions query
	// the ledger for the version off that same value, so the version stamped
	// into schema_migrations and the version counted afterwards cannot drift
	// apart. Spelling the number a second time would let the failing case count
	// rows for a version nobody wrote and pass on a zero it earned by asking the
	// wrong question.
	//
	// It has to be a version the embedded set does not contain, or the seeded
	// database already holds a ledger row for it and the case would read that
	// row as this fixture's own. partialFailureFixtureMigration CHECKS that
	// rather than trusting this sentence.
	partialFailureFixtureName = "900_partial_failure.sql"

	// The three tables bracket the failure: the first statement's table proves
	// the rollback undid what had already executed, the middle one belongs to
	// the statement that separates the two cases, and the last statement's table
	// proves the runner stopped at the failing statement instead of running past
	// it.
	partialFailureFirstTable  = "partial_failure_first"
	partialFailureMiddleTable = "partial_failure_middle"
	partialFailureLastTable   = "partial_failure_last"

	// partialFailureRewrittenMood is what the fixture's data statement writes
	// over every daily_logs row. It differs from the sentinel's own mood, so the
	// write is visible either way round: absent after the failed migration,
	// present after the one that completes.
	partialFailureRewrittenMood = "0"

	// partialFailureMissingTable is the only part of the failing statement the
	// assertions match on. Requiring the runner's whole statement echo would
	// couple this test to that formatting — a change that truncates a long
	// statement, normalises whitespace or quotes differently would redden it
	// with no behaviour regressed — while the table name is what an operator
	// searches the migration for.
	partialFailureMissingTable    = "partial_failure_no_such_table"
	partialFailureBreakingMiddle  = "INSERT INTO " + partialFailureMissingTable + " (id) VALUES (1)"
	partialFailureCompletedMiddle = "CREATE TABLE " + partialFailureMiddleTable + " (id INTEGER)"
)

// partialFailureFixtureSQL is the four-statement migration: create a table,
// rewrite a health field, then middle — the statement whose success or failure
// separates the two cases — and finally create a second table.
func partialFailureFixtureSQL(middle string) string {
	return "CREATE TABLE " + partialFailureFirstTable + " (id INTEGER);\n" +
		"UPDATE daily_logs SET mood = " + partialFailureRewrittenMood + ";\n" +
		middle + ";\n" +
		"CREATE TABLE " + partialFailureLastTable + " (id INTEGER);"
}

// TestAMigrationThatFailsPartWayRecordsNeitherItsSchemaChangeNorItsLedgerRow
// runs that migration twice. The failing case is the subject; the case whose
// middle statement works is the anchor, and it is what keeps every assertion
// below able to fail: it proves the fixture's tables do get created, its health
// write does land, and its ledger row does get written when the migration
// completes — so their absence in the failing case is the rollback and not the
// fixture doing nothing.
func TestAMigrationThatFailsPartWayRecordsNeitherItsSchemaChangeNorItsLedgerRow(t *testing.T) {
	t.Run("the third statement of four is rejected by the engine", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "partial-failure.db")
		seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

		fixture := partialFailureFixtureMigration(t, partialFailureFixtureSQL(partialFailureBreakingMiddle))
		err := applyPartialFailureFixture(t, databasePath, fixture)

		// Reported with t.Error rather than t.Fatal: a runner that swallowed the
		// statement error is exactly the shape whose effect on the schema, the
		// health row and the ledger the assertions below measure, and aborting
		// here would hide all three.
		if err == nil {
			t.Error("a migration whose statement the engine rejects must fail, got no error")
		} else {
			for _, expected := range []string{partialFailureFixtureName, partialFailureMissingTable, "no such table"} {
				if !strings.Contains(err.Error(), expected) {
					t.Errorf("the failure must name %q so an operator can find the statement that broke; got %v", expected, err)
				}
			}
		}

		reader := openSQLiteFileForReplayTest(t, databasePath)
		defer closeReplayDatabase(t, reader)

		assertFixtureTablePresence(t, reader, partialFailureFirstTable, false,
			"statement 1 ran before the failure and its table is still there: the migration was not rolled back")
		assertFixtureTablePresence(t, reader, partialFailureLastTable, false,
			"the last statement is after the failing one and its table is there: the runner ran past a statement the engine rejected")
		assertLedgerRowCount(t, reader, fixture.Version, 0,
			"the migration is recorded as applied although it failed part-way: every later boot will skip it and the schema change is lost for good")
		assertLedgerHoldsOnlyTheEmbeddedSet(t, reader, 0,
			"the rolled-back migration cost the ledger rows of migrations that ARE applied: the next boot re-applies the whole set against a schema that already has it")

		// The health row the failed migration had already rewritten, read back
		// through the sibling helper that names every column a bad replay costs.
		assertSentinelDayIntactOn(t, reader)
	})

	t.Run("anchor: the same migration with a middle statement that works", func(t *testing.T) {
		databasePath := filepath.Join(t.TempDir(), "partial-failure-anchor.db")
		seedFullyMigratedDatabaseWithSentinelDay(t, databasePath)

		fixture := partialFailureFixtureMigration(t, partialFailureFixtureSQL(partialFailureCompletedMiddle))
		requireNoErr(t, applyPartialFailureFixture(t, databasePath, fixture), "a migration whose statements all succeed must apply")

		reader := openSQLiteFileForReplayTest(t, databasePath)
		defer closeReplayDatabase(t, reader)

		assertFixtureTablePresence(t, reader, partialFailureFirstTable, true,
			"statement 1 left nothing behind even though the migration succeeded — the failing case above would then be asserting nothing")
		// The statement in the position under test. Without this the anchor
		// proves every statement of the fixture lands EXCEPT the one the two
		// cases differ in, and a runner that silently skipped it — an off-by-one
		// in the loop, a splitter dropping a chunk — would stay green here.
		assertFixtureTablePresence(t, reader, partialFailureMiddleTable, true,
			"the third statement of four left nothing behind even though the migration succeeded: the runner skipped the very statement the failing case relies on reaching")
		assertFixtureTablePresence(t, reader, partialFailureLastTable, true,
			"the last statement left nothing behind even though the migration succeeded — the failing case above would then be asserting nothing")
		assertLedgerRowCount(t, reader, fixture.Version, 1,
			"a migration that applied cleanly was not recorded — the failing case above would then be asserting nothing")
		assertLedgerHoldsOnlyTheEmbeddedSet(t, reader, 1,
			"the ledger does not hold the embedded set plus this migration, so its row count says nothing about what a failed migration costs")

		rows := readDailyLogRowsForReplayTest(t, reader)
		if len(rows) != 1 {
			t.Fatalf("expected the seeded day to survive a migration that applied, found %d daily_logs rows", len(rows))
		}
		if mood := rows[0]["mood"]; mood != partialFailureRewrittenMood {
			t.Errorf(
				"daily_logs.mood = %q after the migration applied, want %q: the fixture's health write never lands, so its absence in the failing case above is not the rollback",
				mood, partialFailureRewrittenMood,
			)
		}
	})
}

// partialFailureFixtureMigration builds the fixture the runner will see, reading
// its version and order out of the file name with migrationFilePattern — the
// runner's own detection, so the fixture is versioned exactly as an embedded
// migration would be and the caller has one value to both run and query.
//
// It also refuses a version the embedded set already uses. The database every
// case here starts from is fully migrated, so a collision would have the ledger
// assertions counting a row the seed wrote as if the fixture had written it: the
// failing case would report a migration recorded that never was, and the total
// would be off beside it. That was a sentence in a comment until it was this.
func partialFailureFixtureMigration(t *testing.T, sqlText string) embeddedMigration {
	t.Helper()

	matches := migrationFilePattern.FindStringSubmatch(partialFailureFixtureName)
	if len(matches) != 2 {
		t.Fatalf("the fixture name %q is not one the runner would load as a migration", partialFailureFixtureName)
	}
	version := matches[1]
	order, err := strconv.Atoi(version)
	requireNoErr(t, err, "read the fixture's order out of its name")

	for _, embedded := range embeddedSQLiteMigrations(t) {
		if embedded.Version == version {
			t.Fatalf(
				"the fixture's version %s is now taken by embedded migration %s: the seeded database already holds a ledger row for it, so this case would read that row as the fixture's own and report a failure that never happened — renumber %s above the embedded set",
				version, embedded.Name, partialFailureFixtureName,
			)
		}
	}

	return embeddedMigration{Version: version, Order: order, Name: partialFailureFixtureName, SQL: sqlText}
}

// applyPartialFailureFixture runs the fixture through the real applyMigration on
// a connection of its own, which it closes again, so the assertions read the
// file through a connection that never carried the migration.
func applyPartialFailureFixture(t *testing.T, databasePath string, fixture embeddedMigration) error {
	t.Helper()

	database := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, database)

	return applyMigration(database, fixture)
}

// assertFixtureTablePresence reports whether one of the fixture's tables is in
// the schema. The reader is passed in: it applies no migration, so it can never
// repair what it measures, and one per case is enough.
func assertFixtureTablePresence(t *testing.T, reader *gorm.DB, tableName string, want bool, complaint string) {
	t.Helper()

	if got := reader.Migrator().HasTable(tableName); got != want {
		t.Errorf("table %s present=%v, want %v: %s", tableName, got, want, complaint)
	}
}

// assertLedgerRowCount counts the schema_migrations rows for one version.
func assertLedgerRowCount(t *testing.T, reader *gorm.DB, version string, want int64, complaint string) {
	t.Helper()

	var count int64
	requireNoErr(t, reader.Raw(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&count).Error, "count schema_migrations rows")
	if count != want {
		t.Errorf("schema_migrations holds %d row(s) for version %s, want %d: %s", count, version, want, complaint)
	}
}

// assertLedgerHoldsOnlyTheEmbeddedSet pins the rest of the ledger, which the
// per-version count above cannot see. The expected total is DERIVED from the
// embedded migration set rather than written down: the database was seeded by
// booting the real opener, so it holds one row per embedded migration, plus
// extra for whatever the case under test is expected to have added.
func assertLedgerHoldsOnlyTheEmbeddedSet(t *testing.T, reader *gorm.DB, extra int64, complaint string) {
	t.Helper()

	var total int64
	requireNoErr(t, reader.Raw(`SELECT COUNT(*) FROM schema_migrations`).Scan(&total).Error, "count schema_migrations rows")
	if want := int64(len(embeddedSQLiteMigrations(t))) + extra; total != want {
		t.Errorf("schema_migrations holds %d row(s) in total, want %d: %s", total, want, complaint)
	}
}

// embeddedSQLiteMigrationSet reads and parses the embedded SQLite tree once per
// process. Deriving both the collision check and the expected ledger total from
// the live set is the point — a written-down count would agree with itself — but
// the set does not change while a test binary runs, and this file asks for it
// three times per case.
var embeddedSQLiteMigrationSet = sync.OnceValues(func() ([]embeddedMigration, error) {
	return loadEmbeddedMigrations(DriverSQLite)
})

func embeddedSQLiteMigrations(t *testing.T) []embeddedMigration {
	t.Helper()

	migrations, err := embeddedSQLiteMigrationSet()
	requireNoErr(t, err, "load the embedded sqlite migration set")
	if len(migrations) == 0 {
		t.Fatal("loaded no embedded migrations — every expectation derived from the set would assert nothing")
	}
	return migrations
}
