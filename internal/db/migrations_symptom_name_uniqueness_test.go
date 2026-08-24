package db

import (
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
