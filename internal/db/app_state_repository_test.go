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
	database, err := OpenSQLite(filepath.Join(t.TempDir(), "app-state.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
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

// TestAppStateRepositoryRejectsBlankKey covers the guard: an empty/whitespace key
// is not a valid marker. Get treats it as missing; Set refuses it.
func TestAppStateRepositoryRejectsBlankKey(t *testing.T) {
	database, err := OpenSQLite(filepath.Join(t.TempDir(), "app-state-blank.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
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
	database, err := OpenSQLite(filepath.Join(t.TempDir(), "app-state-closed.db"))
	if err != nil {
		t.Fatalf("OpenSQLite() unexpected error: %v", err)
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
