package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// completeOnboardingOn onboards a fresh owner through the service on
// 2026-09-14 (period length 5) with the given auto-fill setting, as the
// onboarding flow does: the start is saved in step 1, the settings in step 2.
func completeOnboardingOn(t *testing.T, email string, autoFill bool) ([]models.DailyLog, models.User) {
	t.Helper()
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	user := createDayServiceTestUser(t, database, email)
	start := startMoveDay(time.September, 14)
	if err := repositories.Users.UpdateByID(context.Background(), user.ID, map[string]any{
		"auto_period_fill":  autoFill,
		"period_length":     5,
		"last_period_start": start,
	}); err != nil {
		t.Fatalf("save the setup answers: %v", err)
	}
	service := NewOnboardingService(repositories.Users)
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	if _, err := service.CompleteOnboardingForUser(context.Background(), user.ID, now, time.UTC, ""); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}
	stored, logs := withdrawReload(t, repositories, user.ID)
	return logs, stored
}

// TestOnboardingWithFillOffWritesTheStartDayAsALoggedCycleStart: the owner's
// stated start is one real row — a period day carrying the cycle-start mark —
// and nothing else, so the calendar, the export and the observed-cycle count
// agree on it.
func TestOnboardingWithFillOffWritesTheStartDayAsALoggedCycleStart(t *testing.T) {
	logs, stored := completeOnboardingOn(t, "onboarding-start-off@example.com", false)

	if len(logs) != 1 {
		t.Fatalf("fill off wrote %d rows, want exactly the start day: %+v", len(logs), logs)
	}
	row := logs[0]
	if row.Date.UTC().Format("2006-01-02") != "2026-09-14" || !row.IsPeriod || !row.CycleStart {
		t.Fatalf("start row = date %s period %v cycle_start %v, want 2026-09-14 period + cycle start", row.Date.UTC().Format("2006-01-02"), row.IsPeriod, row.CycleStart)
	}
	if row.AutoFilled == nil || *row.AutoFilled {
		t.Fatalf("the owner's own start is not an auto-fill row, auto_filled = %v", row.AutoFilled)
	}
	if stored.LastPeriodStart == nil {
		t.Fatal("the baseline must land with the row")
	}
}

// TestOnboardingWithFillOnMarksTheStartRowWithoutDuplicatingIt: the fill's own
// first row carries the cycle-start mark; the other fill days stay plain
// period days and no day holds two rows.
func TestOnboardingWithFillOnMarksTheStartRowWithoutDuplicatingIt(t *testing.T) {
	logs, _ := completeOnboardingOn(t, "onboarding-start-on@example.com", true)

	if len(logs) != 5 {
		t.Fatalf("fill on wrote %d rows, want the 5 period days: %+v", len(logs), logs)
	}
	seen := map[string]bool{}
	for _, row := range logs {
		day := row.Date.UTC().Format("2006-01-02")
		if seen[day] {
			t.Fatalf("two rows for %s", day)
		}
		seen[day] = true
		if !row.IsPeriod {
			t.Fatalf("%s is not a period day", day)
		}
		if want := day == "2026-09-14"; row.CycleStart != want {
			t.Fatalf("%s cycle_start = %v, want %v", day, row.CycleStart, want)
		}
	}
}
