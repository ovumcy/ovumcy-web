package db

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestAppStateRepositoryRoundTripAndUpsert covers the full contract the reminder
// scheduler relies on: a missing key reads as (", false), a Set then Get returns
// the stored value, and a second Set for the SAME key overwrites in place (the
// ON CONFLICT (key) update) rather than erroring on the primary-key collision or
// appending a second row.
func TestAppStateRepositoryRoundTripAndUpsert(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "app-state.db")})
	if err != nil {
		t.Fatalf("OpenDatabase() unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewAppStateRepository(database)
	ctx := context.Background()
	key := models.AppStateKeyLastReminderRunDate

	if _, ok, err := repo.Get(ctx, key); err != nil || ok {
		t.Fatalf("expected missing key to read as (_, false, nil), got ok=%v err=%v", ok, err)
	}

	if err := repo.Set(ctx, key, "2026-07-05"); err != nil {
		t.Fatalf("Set() first write unexpected error: %v", err)
	}
	value, ok, err := repo.Get(ctx, key)
	if err != nil || !ok || value != "2026-07-05" {
		t.Fatalf("expected first value 2026-07-05, got value=%q ok=%v err=%v", value, ok, err)
	}

	if err := repo.Set(ctx, key, "2026-07-06"); err != nil {
		t.Fatalf("Set() upsert unexpected error: %v", err)
	}
	value, ok, err = repo.Get(ctx, key)
	if err != nil || !ok || value != "2026-07-06" {
		t.Fatalf("expected upserted value 2026-07-06, got value=%q ok=%v err=%v", value, ok, err)
	}

	// The upsert must not have appended a second row: exactly one row for the key.
	var count int64
	if err := database.Model(&models.AppState{}).Where("key = ?", key).Count(&count).Error; err != nil {
		t.Fatalf("count app_state rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one app_state row after upsert, got %d", count)
	}
}

// TestAppStateRepositoryDeleteRemovesTheKeyAndToleratesAMissingOne covers the
// contract the calendar-feed restore fence relies on: a marker that is there is
// gone afterwards, and deleting one that was never written is not an error. The
// fence erases its unanchored stamp on every boot that records a token, without
// reading first, so an absent key raising an error would turn every ordinary
// first boot into a failed start.
func TestAppStateRepositoryDeleteRemovesTheKeyAndToleratesAMissingOne(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "app-state-delete.db")})
	if err != nil {
		t.Fatalf("OpenDatabase() unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewAppStateRepository(database)
	ctx := context.Background()
	key := models.AppStateKeyCalendarFeedFenceUnanchored

	if err := repo.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() of a key that was never written must be a no-op, got %v", err)
	}

	if err := repo.Set(ctx, key, "booted without a usable fence"); err != nil {
		t.Fatalf("Set() unexpected error: %v", err)
	}
	if _, ok, err := repo.Get(ctx, key); err != nil || !ok {
		t.Fatalf("expected the marker to be readable before the delete, got ok=%v err=%v", ok, err)
	}

	if err := repo.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if value, ok, err := repo.Get(ctx, key); err != nil || ok || value != "" {
		t.Fatalf("expected the marker to be gone, got value=%q ok=%v err=%v", value, ok, err)
	}

	// A second delete of the same key is the boot after the one that erased it.
	if err := repo.Delete(ctx, key); err != nil {
		t.Fatalf("Delete() must stay idempotent, got %v", err)
	}

	// A blank key deletes nothing rather than every row, which is the one way
	// this method could quietly destroy the markers other subsystems own.
	if err := repo.Set(ctx, models.AppStateKeyLastReminderRunDate, "2026-07-05"); err != nil {
		t.Fatalf("Set() unexpected error: %v", err)
	}
	if err := repo.Delete(ctx, "   "); err != nil {
		t.Fatalf("Delete() with a blank key must be a no-op, got %v", err)
	}
	if _, ok, err := repo.Get(ctx, models.AppStateKeyLastReminderRunDate); err != nil || !ok {
		t.Fatalf("a blank key must match no row at all, got ok=%v err=%v", ok, err)
	}
}

// TestAppStateRepositoryRejectsBlankKey covers the guard: an empty/whitespace key
// is not a valid marker. Get treats it as missing; Set refuses it.
func TestAppStateRepositoryRejectsBlankKey(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "app-state-blank.db")})
	if err != nil {
		t.Fatalf("OpenDatabase() unexpected error: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewAppStateRepository(database)
	ctx := context.Background()

	if _, ok, err := repo.Get(ctx, "   "); err != nil || ok {
		t.Fatalf("expected blank key Get to read as (_, false, nil), got ok=%v err=%v", ok, err)
	}
	if err := repo.Set(ctx, "  ", "value"); err == nil {
		t.Fatal("expected Set with a blank key to return an error")
	}
}

// TestAppStateRepositoryGetSurfacesNonNotFoundError covers the error branch of
// Get: a query failure that is NOT gorm.ErrRecordNotFound (here a closed
// connection) must propagate, not be swallowed as "missing". The scheduler
// relies on this so a real DB failure fails its catch-up safe rather than
// silently reading "never ran".
func TestAppStateRepositoryGetSurfacesNonNotFoundError(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "app-state-closed.db")})
	if err != nil {
		t.Fatalf("OpenDatabase() unexpected error: %v", err)
	}
	repo := NewAppStateRepository(database)

	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB() unexpected error: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}

	value, ok, err := repo.Get(context.Background(), models.AppStateKeyLastReminderRunDate)
	if err == nil {
		t.Fatal("expected Get on a closed database to surface an error")
	}
	if ok || value != "" {
		t.Fatalf("expected a failed Get to return the zero value, got value=%q ok=%v", value, ok)
	}
}
