package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func createDailyLogTestUser(t *testing.T, database *gorm.DB, email string) uint {
	t.Helper()
	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, 'owner', CURRENT_TIMESTAMP)`,
		email, "test-hash",
	).Error; err != nil {
		t.Fatalf("insert user: %v", err)
	}
	var row struct {
		ID uint `gorm:"column:id"`
	}
	if err := database.Raw(`SELECT id FROM users WHERE email = ?`, email).Scan(&row).Error; err != nil {
		t.Fatalf("load user id: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("expected non-zero user id")
	}
	return row.ID
}

// TestDailyLogRepositoryRangeQueriesAndWhitelist covers the daily-log read
// paths (including the FindByUserAndDayRange column whitelist that carries
// pregnancy_test), range/period filtering and ordering, save, and range delete.
func TestDailyLogRepositoryRangeQueriesAndWhitelist(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "daily.db"))
	userID := createDailyLogTestUser(t, database, "daily-log-repo@example.com")
	repo := NewDailyLogRepository(database)

	day := func(d int) time.Time { return time.Date(2026, time.June, d, 0, 0, 0, 0, time.UTC) }
	mk := func(d int, mutate func(*models.DailyLog)) *models.DailyLog {
		entry := &models.DailyLog{
			UserID:          userID,
			Date:            day(d),
			Flow:            "none",
			SexActivity:     "none",
			CervicalMucus:   "none",
			PregnancyTest:   "none",
			CycleFactorKeys: []string{},
			SymptomIDs:      []uint{},
		}
		if mutate != nil {
			mutate(entry)
		}
		return entry
	}

	if err := repo.Create(context.Background(), mk(1, func(e *models.DailyLog) { e.IsPeriod = true })); err != nil {
		t.Fatalf("create d1: %v", err)
	}
	if err := repo.Create(context.Background(), mk(5, func(e *models.DailyLog) { e.PregnancyTest = "positive" })); err != nil {
		t.Fatalf("create d5: %v", err)
	}
	if err := repo.Create(context.Background(), mk(10, func(e *models.DailyLog) { e.IsPeriod = true; e.CycleStart = true })); err != nil {
		t.Fatalf("create d10: %v", err)
	}

	// FindByUserAndDayRange returns the pregnancy_test value via its column
	// whitelist (regression guard: a missing column silently reads as empty).
	entry, found, err := repo.FindByUserAndDayRange(context.Background(), userID, day(5), day(6))
	if err != nil || !found {
		t.Fatalf("find d5 = (found=%t, err=%v), want found", found, err)
	}
	if entry.PregnancyTest != "positive" {
		t.Fatalf("expected pregnancy_test=positive to round-trip via read whitelist, got %q", entry.PregnancyTest)
	}

	// Empty range reports not-found.
	if _, found, _ := repo.FindByUserAndDayRange(context.Background(), userID, day(20), day(21)); found {
		t.Fatal("expected empty range to report not found")
	}

	// ListByUserDayRange returns the window in DESC order.
	window, err := repo.ListByUserDayRange(context.Background(), userID, day(1), day(11))
	if err != nil {
		t.Fatalf("list day range: %v", err)
	}
	if len(window) != 3 {
		t.Fatalf("expected 3 logs in window, got %d", len(window))
	}
	if !window[0].Date.Equal(day(10)) || !window[2].Date.Equal(day(1)) {
		t.Fatalf("expected DESC date order [10..1], got %s..%s", window[0].Date, window[2].Date)
	}

	// ListByUserRange honors the lower bound only.
	fromD5 := day(5)
	ranged, err := repo.ListByUserRange(context.Background(), userID, &fromD5, nil)
	if err != nil {
		t.Fatalf("list range: %v", err)
	}
	if len(ranged) != 2 {
		t.Fatalf("expected 2 logs from d5 onward, got %d", len(ranged))
	}

	// ListPeriodDays returns only period rows.
	periods, err := repo.ListPeriodDays(context.Background(), userID)
	if err != nil {
		t.Fatalf("list period days: %v", err)
	}
	if len(periods) != 2 {
		t.Fatalf("expected 2 period days (d1, d10), got %d", len(periods))
	}

	// Save persists a mutation on an existing row.
	entry.Notes = "updated note"
	if err := repo.Save(context.Background(), &entry); err != nil {
		t.Fatalf("save: %v", err)
	}
	if updated, _, _ := repo.FindByUserAndDayRange(context.Background(), userID, day(5), day(6)); updated.Notes != "updated note" {
		t.Fatalf("expected Save to persist note, got %q", updated.Notes)
	}

	// DeleteByUserAndDayRange removes the [d1, d5) window (only d1).
	if err := repo.DeleteByUserAndDayRange(context.Background(), userID, day(1), day(5)); err != nil {
		t.Fatalf("delete range: %v", err)
	}
	remaining, err := repo.ListByUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("list by user: %v", err)
	}
	if len(remaining) != 2 {
		t.Fatalf("expected 2 logs after deleting d1, got %d", len(remaining))
	}
}
