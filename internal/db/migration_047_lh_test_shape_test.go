package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigration047AddsTheLHTestColumnsWithSafeDefaults rewinds migration 047 on
// a populated database and re-applies it: an existing owner must come out with
// the LH test toggle off, and the day-log column must be NOT NULL with the
// 'none' default so every pre-existing day reads as "no result".
func TestMigration047AddsTheLHTestColumnsWithSafeDefaults(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-migration-047.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)
	repo := NewUserRepository(database)
	user := createUserForTimezoneTest(t, repo, "migration-047@example.com")

	if err := database.Exec(`ALTER TABLE users DROP COLUMN track_lh_test`).Error; err != nil {
		t.Fatalf("drop users.track_lh_test to rewind the schema: %v", err)
	}
	if err := database.Exec(`ALTER TABLE daily_logs DROP COLUMN lh_test`).Error; err != nil {
		t.Fatalf("drop daily_logs.lh_test to rewind the schema: %v", err)
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "047").Error; err != nil {
		t.Fatalf("delete the migration 047 record: %v", err)
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
		t.Fatalf("expected migration 047 to apply to a populated database: %v", err)
	}
	reopenedSQLDB, err := reopened.DB()
	if err != nil {
		t.Fatalf("get reopened sql db handle: %v", err)
	}
	t.Cleanup(func() { _ = reopenedSQLDB.Close() })
	assertAllEmbeddedMigrationsApplied(t, reopened)

	upgraded, err := NewUserRepository(reopened).FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("reload the upgraded owner: %v", err)
	}
	if upgraded.TrackLHTest {
		t.Fatal("the upgrade switched the LH test toggle on for an existing owner")
	}
	if upgraded.Email == "" {
		t.Fatal("the seeded owner lost data across the upgrade, so the toggle assertion could be reading an empty row")
	}

	for table, column := range map[string]string{"daily_logs": "lh_test", "users": "track_lh_test"} {
		columnTypes, err := reopened.Migrator().ColumnTypes(table)
		if err != nil {
			t.Fatalf("read %s column types: %v", table, err)
		}
		seen := false
		for _, columnType := range columnTypes {
			if strings.ToLower(strings.TrimSpace(columnType.Name())) != column {
				continue
			}
			seen = true
			if nullable, reported := columnType.Nullable(); reported && nullable {
				t.Errorf("%s.%s is nullable: an upgraded row could carry a value the readers do not model", table, column)
			}
			value, hasDefault := columnType.DefaultValue()
			if !hasDefault {
				t.Errorf("%s.%s has no default after the upgrade", table, column)
			}
			if table == "daily_logs" && strings.Trim(strings.TrimSpace(value), "'\"") != "none" {
				t.Errorf("%s.%s default is %q, want 'none'", table, column, value)
			}
		}
		if !seen {
			t.Fatalf("%s.%s does not exist after migrations", table, column)
		}
	}
}
