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
// requireNoErr fails the test immediately when err is non-nil. Extracting the
// repeated `if err != nil { t.Fatalf }` blocks out of the long repository
// integration scenarios keeps their cyclomatic complexity in range without
// splitting a single coherent round-trip across many test functions.
func requireNoErr(t *testing.T, err error, context string) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", context, err)
	}
}

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

	requireNoErr(t, repo.Create(context.Background(), mk(1, func(e *models.DailyLog) { e.IsPeriod = true })), "create d1")
	requireNoErr(t, repo.Create(context.Background(), mk(5, func(e *models.DailyLog) { e.PregnancyTest = "positive" })), "create d5")
	requireNoErr(t, repo.Create(context.Background(), mk(10, func(e *models.DailyLog) { e.IsPeriod = true; e.CycleStart = true })), "create d10")

	// FindByUserAndDayRange returns the pregnancy_test value via its column
	// whitelist (regression guard: a missing column silently reads as empty).
	entry, found, err := repo.FindByUserAndDayRange(context.Background(), userID, day(5), day(6))
	requireNoErr(t, err, "find d5")
	if !found {
		t.Fatal("expected d5 to be found")
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
	requireNoErr(t, err, "list day range")
	if len(window) != 3 {
		t.Fatalf("expected 3 logs in window, got %d", len(window))
	}
	if !window[0].Date.Equal(day(10)) || !window[2].Date.Equal(day(1)) {
		t.Fatalf("expected DESC date order [10..1], got %s..%s", window[0].Date, window[2].Date)
	}

	// ListByUserRange honors the lower bound only.
	fromD5 := day(5)
	ranged, err := repo.ListByUserRange(context.Background(), userID, &fromD5, nil)
	requireNoErr(t, err, "list range")
	if len(ranged) != 2 {
		t.Fatalf("expected 2 logs from d5 onward, got %d", len(ranged))
	}

	// ListPeriodDays returns only period rows.
	periods, err := repo.ListPeriodDays(context.Background(), userID)
	requireNoErr(t, err, "list period days")
	if len(periods) != 2 {
		t.Fatalf("expected 2 period days (d1, d10), got %d", len(periods))
	}

	// Save persists a mutation on an existing row.
	entry.Notes = "updated note"
	requireNoErr(t, repo.Save(context.Background(), &entry), "save")
	if updated, _, _ := repo.FindByUserAndDayRange(context.Background(), userID, day(5), day(6)); updated.Notes != "updated note" {
		t.Fatalf("expected Save to persist note, got %q", updated.Notes)
	}

	// DeleteByUserAndDayRange removes the [d1, d5) window (only d1).
	requireNoErr(t, repo.DeleteByUserAndDayRange(context.Background(), userID, day(1), day(5)), "delete range")
	remaining, err := repo.ListByUser(context.Background(), userID)
	requireNoErr(t, err, "list by user")
	if len(remaining) != 2 {
		t.Fatalf("expected 2 logs after deleting d1, got %d", len(remaining))
	}
}
