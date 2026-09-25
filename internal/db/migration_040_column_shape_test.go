package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestMigration040AddsAnUnbackfilledNullableColumn applies migration 040 to a
// POPULATED users table — the state every upgrading instance is in — and pins
// what the existing row gets: NULL, with the rest of the row untouched. Modeled
// on TestMigration039AddsTwoUnbackfilledNullableColumns. Cited by SECURITY.md's
// Calendar Feed Subscription rows.
func TestMigration040AddsAnUnbackfilledNullableColumn(t *testing.T) {
	const column = "calendar_feed_last_polled_on"
	databasePath := filepath.Join(t.TempDir(), "ovumcy-migration-040.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)
	repo := NewUserRepository(database)

	user := createUserForTimezoneTest(t, repo, "migration-040@example.com")
	if err := database.Exec(
		`UPDATE users SET calendar_feed_selector = ? WHERE id = ?`,
		"SELECTOR16CHARSXX", user.ID,
	).Error; err != nil {
		t.Fatalf("seed the pre-upgrade row: %v", err)
	}

	if err := database.Exec(`ALTER TABLE users DROP COLUMN ` + column).Error; err != nil {
		t.Fatalf("drop %s to rewind the schema: %v", column, err)
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "040").Error; err != nil {
		t.Fatalf("delete the migration 040 record: %v", err)
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
		t.Fatalf("expected migration 040 to apply to a populated users table: %v", err)
	}
	reopenedSQLDB, err := reopened.DB()
	if err != nil {
		t.Fatalf("get reopened sql db handle: %v", err)
	}
	t.Cleanup(func() { _ = reopenedSQLDB.Close() })
	assertAllEmbeddedMigrationsApplied(t, reopened)

	upgraded, err := NewUserRepository(reopened).FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("reload the upgraded row: %v", err)
	}
	if upgraded.CalendarFeedLastPolledOn != nil {
		t.Fatalf("the upgrade wrote a poll date (%s) onto a row that was never polled since this column existed", upgraded.CalendarFeedLastPolledOn)
	}
	if upgraded.CalendarFeedSelector == "" {
		t.Fatal("the seeded row lost data across the upgrade, so the NULL assertion above could be reporting an empty row")
	}
}

// TestMigration040ColumnIsNullableUndefaultedAndUnindexed reads the migrated
// schema back through the driver rather than trusting the DDL text.
func TestMigration040ColumnIsNullableUndefaultedAndUnindexed(t *testing.T) {
	const column = "calendar_feed_last_polled_on"
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "ovumcy-040-shape.db"))

	columnTypes, err := database.Migrator().ColumnTypes("users")
	if err != nil {
		t.Fatalf("read users column types: %v", err)
	}
	seen := false
	for _, columnType := range columnTypes {
		if strings.ToLower(strings.TrimSpace(columnType.Name())) != column {
			continue
		}
		seen = true

		nullable, reported := columnType.Nullable()
		if !reported {
			t.Errorf("the driver reported no nullability for users.%s, so that half of the contract went unmeasured", column)
		} else if !nullable {
			t.Errorf("users.%s is NOT NULL: an upgraded row could then never say that it was never polled", column)
		}
		if value, hasDefault := columnType.DefaultValue(); hasDefault && strings.TrimSpace(value) != "" {
			t.Errorf("users.%s carries the default %q: a default is a backfill by the back door", column, value)
		}
	}
	if !seen {
		t.Fatalf("users.%s does not exist after migrations: this guard would otherwise pass over an absent column", column)
	}

	var definitions []string
	if err := database.Raw(
		`SELECT COALESCE(sql, '') FROM sqlite_master WHERE type = 'index' AND tbl_name = 'users'`,
	).Scan(&definitions).Error; err != nil {
		t.Fatalf("read users index definitions: %v", err)
	}
	if len(definitions) == 0 {
		t.Fatal("the users table reported no indexes at all, so this guard would pass over any index the migration might add")
	}
	for _, definition := range definitions {
		if strings.Contains(strings.ToLower(definition), column) {
			t.Errorf("users.%s is indexed by %q: it is read one row at a time by owner id, never queried across owners", column, definition)
		}
	}
}
