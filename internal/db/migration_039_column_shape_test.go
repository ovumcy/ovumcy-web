package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// migration039Columns are the two marks migration 039 adds, and the shape all
// three assertions below are about: nullable, no default, no index.
//
// Each property is load-bearing rather than stylistic. NULLABLE and NO DEFAULT
// are how "nothing is recorded" stays distinguishable from a recorded value on
// every row that existed before the upgrade — a default would backfill by the
// back door, which is precisely what the migration's prose forbids. NO INDEX
// because both are read one row at a time by owner id, and an index on a column
// that says when an owner's data last left the instance is a structure worth
// nothing to this workload.
var migration039Columns = []string{"webhook_last_delivered_at", "calendar_feed_key_epoch"}

// TestMigration039AddsTwoUnbackfilledNullableColumns applies migration 039 to a
// POPULATED users table — the state every upgrading instance is in — and pins
// what the existing row gets: NULL, in both columns, with the rest of the row
// untouched.
func TestMigration039AddsTwoUnbackfilledNullableColumns(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "ovumcy-migration-039.db")
	database := openSQLiteForMigrationBootstrapTest(t, databasePath)
	repo := NewUserRepository(database)

	user := createUserForTimezoneTest(t, repo, "migration-039@example.com")
	// A populated row that also carries the watermarks, so a backfill from them —
	// the one temptation the migration's prose is written against — would have
	// something to draw on and would show up here as a non-NULL mark.
	if err := database.Exec(
		`UPDATE users SET webhook_period_last_sent_cycle_start = ?, webhook_ovulation_last_sent_cycle_start = ?, calendar_feed_selector = ? WHERE id = ?`,
		"2026-03-01", "2026-03-15", "SELECTOR16CHARSXX", user.ID,
	).Error; err != nil {
		t.Fatalf("seed the pre-upgrade row: %v", err)
	}

	// Rewind to the pre-039 schema and re-boot, so the migration runs against a
	// table that already holds data rather than against a fresh install.
	for _, column := range migration039Columns {
		if err := database.Exec(`ALTER TABLE users DROP COLUMN ` + column).Error; err != nil {
			t.Fatalf("drop %s to rewind the schema: %v", column, err)
		}
	}
	if err := database.Exec(`DELETE FROM schema_migrations WHERE version = ?`, "039").Error; err != nil {
		t.Fatalf("delete the migration 039 record: %v", err)
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
		t.Fatalf("expected migration 039 to apply to a populated users table: %v", err)
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
	if upgraded.WebhookLastDeliveredAt != nil {
		t.Fatalf("the upgrade wrote a delivery time (%s) onto a row that never recorded one: the watermarks are claim anchors and backfilling from them makes that lie permanent", upgraded.WebhookLastDeliveredAt)
	}
	if upgraded.CalendarFeedKeyEpoch != "" {
		t.Fatalf("the upgrade stamped a key epoch (%q) onto a token minted before it was recorded, asserting the one thing the column exists to deny", upgraded.CalendarFeedKeyEpoch)
	}
	if upgraded.WebhookPeriodLastSentCycleStart == nil || upgraded.CalendarFeedSelector == "" {
		t.Fatal("the seeded row lost data across the upgrade, so the NULL assertions above could be reporting an empty row")
	}
}

// TestMigration039ColumnsAreNullableUndefaultedAndUnindexed reads the migrated
// schema back through the driver rather than trusting the DDL text, so a value
// arriving from a rebuild or a later migration would still be caught.
func TestMigration039ColumnsAreNullableUndefaultedAndUnindexed(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "ovumcy-039-shape.db"))

	columnTypes, err := database.Migrator().ColumnTypes("users")
	if err != nil {
		t.Fatalf("read users column types: %v", err)
	}
	seen := map[string]bool{}
	for _, columnType := range columnTypes {
		name := strings.ToLower(strings.TrimSpace(columnType.Name()))
		for _, wanted := range migration039Columns {
			if name != wanted {
				continue
			}
			seen[wanted] = true

			nullable, reported := columnType.Nullable()
			if !reported {
				t.Errorf("the driver reported no nullability for users.%s, so that half of the contract went unmeasured", wanted)
			} else if !nullable {
				t.Errorf("users.%s is NOT NULL: an upgraded row could then never say that nothing is recorded", wanted)
			}
			if value, hasDefault := columnType.DefaultValue(); hasDefault && strings.TrimSpace(value) != "" {
				t.Errorf("users.%s carries the default %q: a default is a backfill by the back door, which migration 039 exists to refuse", wanted, value)
			}
		}
	}
	for _, wanted := range migration039Columns {
		if !seen[wanted] {
			t.Fatalf("users.%s does not exist after migrations: this guard would otherwise pass over an absent column", wanted)
		}
	}

	// Read the index definitions straight out of the catalogue: the driver's own
	// GetIndexes cannot scan this schema's expression index (it returns a NULL
	// column name), and the DDL text answers the question directly anyway.
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
		for _, wanted := range migration039Columns {
			if strings.Contains(strings.ToLower(definition), wanted) {
				t.Errorf("users.%s is indexed by %q: neither mark is queried across owners, and an index on when an owner's data last left is a structure this workload never asked for", wanted, definition)
			}
		}
	}
}
