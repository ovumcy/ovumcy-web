package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// startMoveTodayBoundNow is the request clock of the tests below: midday of
// 2026-10-07 in UTC, the owner's zone, so the owner's local today is 10-07.
var startMoveTodayBoundNow = time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)

// startMoveTodayBoundOwner creates an owner with auto-fill on and a period length
// of 5, then runs fill(repositories, userID) to write the stored days.
func startMoveTodayBoundOwner(t *testing.T, email string, fill func(*db.Repositories, uint) error) (*db.Repositories, uint) {
	t.Helper()
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	user := createDayServiceTestUser(t, database, email)
	if err := repositories.Users.UpdateByID(context.Background(), user.ID, map[string]any{"auto_period_fill": true, "period_length": 5}); err != nil {
		t.Fatalf("enable auto-fill: %v", err)
	}
	if err := fill(repositories, user.ID); err != nil {
		t.Fatalf("fixture fill: %v", err)
	}
	return repositories, user.ID
}

// startMoveTodayBoundSave saves rawStart through the request's validation, as the
// Settings handler does, on the clock startMoveTodayBoundNow.
func startMoveTodayBoundSave(t *testing.T, repositories *db.Repositories, userID uint, rawStart string) {
	t.Helper()
	settings := NewSettingsService(repositories.Users)
	settings.AttachDayLogReader(NewDayService(repositories.DailyLogs, repositories.Users))
	update, err := settings.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       5,
		AutoPeriodFill:     true,
		LastPeriodStartRaw: rawStart,
		LastPeriodStartSet: true,
		Present:            AllCycleSettingsMembers(),
	}, startMoveTodayBoundNow, time.UTC)
	if err != nil {
		t.Fatalf("ValidateCycleSettings: %v", err)
	}
	if err := settings.SaveCycleSettings(context.Background(), userID, update); err != nil {
		t.Fatalf("SaveCycleSettings: %v", err)
	}
}

// TestSettingsStartMoveToTodayFillsNoDayAfterToday: a start moved to today with
// auto-fill on and a period length of 5 records today and nothing past it — the
// move's fill stops at the owner's local today, on the bound onboarding's fill
// holds, instead of storing four days the owner has not reached.
func TestSettingsStartMoveToTodayFillsNoDayAfterToday(t *testing.T) {
	onboarded := startMoveDay(time.September, 14)
	repositories, userID := startMoveTodayBoundOwner(t, "start-move-today-bound@example.com", func(repositories *db.Repositories, userID uint) error {
		return repositories.Users.CompleteOnboarding(context.Background(), userID, onboarded, onboarded.AddDate(0, 0, 4), true)
	})

	startMoveTodayBoundSave(t, repositories, userID, "2026-10-07")

	logs := startMoveListLogs(t, repositories, userID)
	for _, entry := range logs {
		if entry.Date.UTC().Format("2006-01-02") > "2026-10-07" {
			t.Fatalf("the move stored a day after today: %s (logged %v)", entry.Date.UTC().Format("2006-01-02"), startMoveLoggedDays(logs))
		}
	}
	today, found := startMoveRowOn(logs, "2026-10-07")
	if !found || !today.IsPeriod {
		t.Fatalf("the moved start is not a period day (logged %v)", startMoveLoggedDays(logs))
	}
}

// TestSettingsStartMoveKeepsATodayOnboardingsSingleDay: onboarding completed on
// its own start day (10-07, period length 5, auto-fill on) records that one day,
// its fill stopping at today. One row is no proven cohort, so a later move of
// the start to 10-01 fills 10-01..10-05 and leaves the 10-07 row in place.
func TestSettingsStartMoveKeepsATodayOnboardingsSingleDay(t *testing.T) {
	ctx := context.Background()
	start := startMoveDay(time.October, 7)
	repositories, userID := startMoveTodayBoundOwner(t, "start-move-today-onboarding@example.com", func(repositories *db.Repositories, userID uint) error {
		if err := repositories.Users.SaveOnboardingStep1(ctx, userID, start); err != nil {
			return err
		}
		if err := repositories.Users.SaveOnboardingStep2(ctx, userID, 28, 5, true, false, ""); err != nil {
			return err
		}
		_, err := NewOnboardingService(repositories.Users).CompleteOnboardingForUser(ctx, userID, startMoveTodayBoundNow, time.UTC)
		return err
	})
	if got := startMoveLoggedDays(startMoveListLogs(t, repositories, userID)); len(got) != 1 || got[0] != "2026-10-07" {
		t.Fatalf("fixture: onboarding on its start day logged %v, want [2026-10-07]", got)
	}

	startMoveTodayBoundSave(t, repositories, userID, "2026-10-01")

	got := startMoveLoggedDays(startMoveListLogs(t, repositories, userID))
	want := []string{"2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04", "2026-10-05", "2026-10-07"}
	if len(got) != len(want) {
		t.Fatalf("logged %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("logged %v, want %v", got, want)
		}
	}
}

// TestSettingsStartMoveClearsTheOldFillPastToday: a fill written before it was
// bounded by today stored 10-05..10-09 with one stamp. Today is 10-07 and the
// start is corrected to 10-06. The move fills 10-06 and 10-07 only, so only
// those two survive among the old fill's rows: 10-08 and 10-09 are no day the
// move writes again, and they go with the old start's 10-05.
func TestSettingsStartMoveClearsTheOldFillPastToday(t *testing.T) {
	oldStart := startMoveDay(time.October, 5)
	repositories, userID := startMoveTodayBoundOwner(t, "start-move-old-fill-past-today@example.com", func(repositories *db.Repositories, userID uint) error {
		return repositories.Users.CompleteOnboarding(context.Background(), userID, oldStart, oldStart.AddDate(0, 0, 4), true)
	})

	startMoveTodayBoundSave(t, repositories, userID, "2026-10-06")

	logs := startMoveListLogs(t, repositories, userID)
	got := startMoveLoggedDays(logs)
	want := []string{"2026-10-06", "2026-10-07"}
	if len(got) != len(want) {
		t.Fatalf("logged %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("logged %v, want %v", got, want)
		}
	}
}
