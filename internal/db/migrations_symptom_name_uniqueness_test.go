package db

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// symptomNameUniqueIndexName is migration 037's index. It is named here rather
// than inlined so a rename shows up as one edit in this file and in the two
// migration files, and never as a guard that quietly stopped looking.
const symptomNameUniqueIndexName = "idx_symptom_types_user_name_unique"

// TestSymptomNameUniqueIndexIsCreatedAndCoversEveryRow pins the shape of
// migration 037's index on a clean bootstrap.
//
// UNIQUE and per owner is the point: two accounts on one instance each keep
// their own "Cramps". Covering EVERY row rather than only the unarchived ones
// is the deliberate half — ListByUser returns archived rows too, so the service
// has always treated an archived name as taken, and a partial index would be
// weaker than the rule it backs and would let an archive-then-recreate produce
// a pair that can never be restored.
func TestSymptomNameUniqueIndexIsCreatedAndCoversEveryRow(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "symptom-name-unique.db"))

	indexSQL := loadSQLiteObjectSQL(t, database, "index", symptomNameUniqueIndexName)
	definition := strings.ToLower(strings.Join(strings.Fields(indexSQL), ""))
	if definition == "" {
		t.Fatalf("expected index %s to exist after migration", symptomNameUniqueIndexName)
	}
	if !strings.Contains(definition, "uniqueindex") {
		t.Fatalf("expected %s to be UNIQUE, got %q", symptomNameUniqueIndexName, indexSQL)
	}
	if !strings.Contains(definition, "(user_id,lower(name))") {
		t.Fatalf("expected %s to key on (user_id, lower(name)), got %q", symptomNameUniqueIndexName, indexSQL)
	}
	if strings.Contains(definition, "where") {
		t.Fatalf("expected %s to cover every row, including archived ones, got %q", symptomNameUniqueIndexName, indexSQL)
	}

	owner := createDailyLogTestUser(t, database, "symptom-index-owner@example.com")
	other := createDailyLogTestUser(t, database, "symptom-index-other@example.com")

	insert := func(userID uint, name string) error {
		return database.Exec(
			`INSERT INTO symptom_types (user_id, name, icon, color, is_builtin) VALUES (?, ?, 'x', '#FF0000', 0)`,
			userID, name,
		).Error
	}

	if err := insert(owner, "Joint stiffness"); err != nil {
		t.Fatalf("insert first symptom: %v", err)
	}
	if err := insert(other, "Joint stiffness"); err != nil {
		t.Fatalf("expected the index to be per owner, got %v", err)
	}
	if err := insert(owner, "joint stiffness"); err == nil {
		t.Fatal("expected the index to refuse a case-only duplicate for one owner")
	}
}

// TestParseCreateUniqueIndexStatementReadsOnlyWhatItCanDescribe covers the
// decision that precedes the refusal: whether a statement is one whose coverage
// the runner can state exactly.
//
// Every "not recognized" case here is a statement the engine still executes and
// still refuses on a genuine conflict — only the diagnostic degrades. That is
// the deliberate trade: a key list the runner half-understands would build a
// duplicate query around a hole and could refuse a migration the engine would
// have accepted, which is worse than a poor message.
func TestParseCreateUniqueIndexStatementReadsOnlyWhatItCanDescribe(t *testing.T) {
	for _, testCase := range []struct {
		Name       string
		Statement  string
		Recognized bool
		Table      string
		KeyExprs   []string
	}{
		{
			Name:       "the shipped statement, key expressions and all",
			Statement:  "CREATE UNIQUE INDEX IF NOT EXISTS idx_symptom_types_user_name_unique\n    ON symptom_types (user_id, lower(name))",
			Recognized: true,
			Table:      "symptom_types",
			KeyExprs:   []string{"user_id", "lower(name)"},
		},
		{
			Name:       "a prose header does not hide the statement under it",
			Statement:  "-- Per-owner uniqueness.\n--\n-- More prose.\nCREATE UNIQUE INDEX idx_x ON t (a, b)",
			Recognized: true,
			Table:      "t",
			KeyExprs:   []string{"a", "b"},
		},
		{
			Name:       "a comma inside a function call does not split the key list",
			Statement:  "CREATE UNIQUE INDEX idx_x ON t (a, coalesce(b, c))",
			Recognized: true,
			Table:      "t",
			KeyExprs:   []string{"a", "coalesce(b, c)"},
		},
		{
			Name:       "a partial index is left to the engine",
			Statement:  "CREATE UNIQUE INDEX IF NOT EXISTS idx_users_calendar_feed_selector\n    ON users (calendar_feed_selector)\n    WHERE calendar_feed_selector <> ''",
			Recognized: false,
		},
		{
			Name:       "an unclosed key list is left to the engine",
			Statement:  "CREATE UNIQUE INDEX idx_x ON t (a, b",
			Recognized: false,
		},
		{
			Name:       "an empty entry in the middle of the key list is left to the engine",
			Statement:  "CREATE UNIQUE INDEX idx_x ON t (a, , b)",
			Recognized: false,
		},
		{
			Name:       "an empty entry at the end of the key list is left to the engine",
			Statement:  "CREATE UNIQUE INDEX idx_x ON t (a, b,)",
			Recognized: false,
		},
		{
			Name:       "a non-unique index is not this check's business",
			Statement:  "CREATE INDEX idx_x ON t (a)",
			Recognized: false,
		},
	} {
		intent := parseCreateUniqueIndexStatement(testCase.Statement)
		if intent.Recognized != testCase.Recognized {
			t.Errorf("%s: Recognized = %t, want %t", testCase.Name, intent.Recognized, testCase.Recognized)
			continue
		}
		if !testCase.Recognized {
			continue
		}
		if intent.Table != testCase.Table {
			t.Errorf("%s: Table = %q, want %q", testCase.Name, intent.Table, testCase.Table)
		}
		if strings.Join(intent.KeyExprs, "|") != strings.Join(testCase.KeyExprs, "|") {
			t.Errorf("%s: KeyExprs = %v, want %v", testCase.Name, intent.KeyExprs, testCase.KeyExprs)
		}
	}
}

// probeMigration is a migration this test owns, used to drive the refusal
// against statements the embedded set does not contain. Its version is far
// above the tree's so it can never be confused with a real one.
func probeMigration() embeddedMigration {
	return embeddedMigration{Version: "900", Order: 900, Name: "900_probe.sql"}
}

// TestUniqueIndexRefusalOnStatementsTheEmbeddedSetDoesNotContain covers the
// three answers the refusal gives besides "these rows conflict".
func TestUniqueIndexRefusalOnStatementsTheEmbeddedSetDoesNotContain(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "symptom-name-probe.db"))

	// A table this migration is about to create is not there when the check
	// runs, and an index over no rows can conflict with nothing.
	if err := refuseUniqueIndexOverExistingDuplicates(database, probeMigration(), "CREATE UNIQUE INDEX idx_probe ON not_a_table (a)"); err != nil {
		t.Fatalf("a table that does not exist yet must not refuse the migration: %v", err)
	}

	// A key expression the engine rejects is a fault in the check, reported as
	// one, rather than a verdict about the rows.
	err := refuseUniqueIndexOverExistingDuplicates(database, probeMigration(), "CREATE UNIQUE INDEX idx_probe ON symptom_types (no_such_column)")
	if err == nil {
		t.Fatal("a duplicate query the engine cannot run must be reported, not passed over")
	}
	for _, fragment := range []string{"900_probe.sql", "symptom_types", "idx_probe"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Fatalf("the inspection failure must name %q, got: %v", fragment, err)
		}
	}
}

// TestUniqueIndexRefusalNamesAtMostFiveGroupsAndSaysThereAreMore pins the
// report's bound. An operator needs the shape of the problem, not a dump of the
// table, and a truncated list that did not say it was truncated would read as a
// complete one.
func TestUniqueIndexRefusalNamesAtMostFiveGroupsAndSaysThereAreMore(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "symptom-name-groups.db"))
	owner := createDailyLogTestUser(t, database, "symptom-groups-owner@example.com")

	// Six icons, each on two rows: six groups under an index keyed on icon, one
	// more than the report lists. The names stay distinct so the shipped index
	// on (user_id, lower(name)) is untouched.
	for group := range 6 {
		for copyIndex := range 2 {
			if err := database.Exec(
				`INSERT INTO symptom_types (user_id, name, icon, color, is_builtin) VALUES (?, ?, ?, '#FF0000', 0)`,
				owner,
				fmt.Sprintf("Probe %d-%d", group, copyIndex),
				fmt.Sprintf("i%d", group),
			).Error; err != nil {
				t.Fatalf("seed probe row %d-%d: %v", group, copyIndex, err)
			}
		}
	}

	err := refuseUniqueIndexOverExistingDuplicates(database, probeMigration(), "CREATE UNIQUE INDEX idx_probe ON symptom_types (icon)")
	if err == nil {
		t.Fatal("six conflicting groups must refuse the migration")
	}
	message := err.Error()
	if groups := strings.Count(message, " -> "); groups != duplicateGroupReportLimit {
		t.Fatalf("expected %d named groups, got %d in: %s", duplicateGroupReportLimit, groups, message)
	}
	if !strings.Contains(message, "and more") {
		t.Fatalf("a truncated list must say it is truncated, got: %s", message)
	}
}

// TestMigrationRefusesToCreateTheSymptomNameIndexOverExistingDuplicates is the
// migration's own refusal.
//
// A database that already holds duplicate names cannot take the index, and the
// only ways to make room are deleting or merging somebody's symptom rows. This
// is a health tracker: neither is acceptable without the owner, so the
// migration stops, names every conflicting (user_id, lower(name)) group, and
// leaves the database exactly as it found it.
//
// The setup replays the migration against a duplicate-carrying database the way
// an upgrade would meet one: drop the index, forget the ledger row, seed the
// pair the application never wrote, and boot again.
func TestMigrationRefusesToCreateTheSymptomNameIndexOverExistingDuplicates(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "symptom-name-duplicates.db")

	func() {
		database := openSQLiteForMigrationBootstrapTest(t, databasePath)
		owner := createDailyLogTestUser(t, database, "symptom-duplicate-owner@example.com")

		if err := database.Exec(`DROP INDEX IF EXISTS ` + symptomNameUniqueIndexName).Error; err != nil {
			t.Fatalf("drop index for replay: %v", err)
		}
		if err := database.Exec(`DELETE FROM schema_migrations WHERE version = '037'`).Error; err != nil {
			t.Fatalf("forget migration 037: %v", err)
		}
		for _, name := range []string{"Cramps", "cramps"} {
			if err := database.Exec(
				`INSERT INTO symptom_types (user_id, name, icon, color, is_builtin) VALUES (?, ?, 'x', '#FF0000', 0)`,
				owner, name,
			).Error; err != nil {
				t.Fatalf("seed duplicate %q: %v", name, err)
			}
		}
		sqlDB, err := database.DB()
		requireNoErr(t, err, "get sql db handle")
		requireNoErr(t, sqlDB.Close(), "close sql db handle")
	}()

	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err == nil {
		// Release the handle the successful open kept, so the failure below is
		// the only thing this case reports on Windows.
		if sqlDB, handleErr := database.DB(); handleErr == nil {
			_ = sqlDB.Close()
		}
		t.Fatal("expected the migration to refuse a database that already holds duplicate symptom names")
	}

	message := err.Error()
	for _, fragment := range []string{"037_symptom_name_uniqueness.sql", "symptom_types", "cramps", "2"} {
		if !strings.Contains(strings.ToLower(message), strings.ToLower(fragment)) {
			t.Fatalf("refusal must name the conflicting rows, %q is missing from: %s", fragment, message)
		}
	}

	// Nothing was written: both rows are still there and the index is still absent.
	reader := openSQLiteFileForReplayTest(t, databasePath)
	defer closeReplayDatabase(t, reader)

	var rows int64
	if err := reader.Raw(`SELECT COUNT(*) FROM symptom_types WHERE lower(name) = 'cramps'`).Scan(&rows).Error; err != nil {
		t.Fatalf("count duplicate rows after the refusal: %v", err)
	}
	if rows != 2 {
		t.Fatalf("the refusal must leave every row untouched, found %d rows instead of 2", rows)
	}

	var indexSQL struct {
		SQL string `gorm:"column:sql"`
	}
	if err := reader.Raw(`SELECT sql FROM sqlite_master WHERE type = 'index' AND name = ?`, symptomNameUniqueIndexName).Scan(&indexSQL).Error; err != nil {
		t.Fatalf("read index after the refusal: %v", err)
	}
	if strings.TrimSpace(indexSQL.SQL) != "" {
		t.Fatalf("expected no index after the refusal, got %q", indexSQL.SQL)
	}
}
