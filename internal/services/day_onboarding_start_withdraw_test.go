package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The production day-log repository withdraws the onboarding start on the day
// write's own transaction; without this the day service would refuse the
// un-mark of an onboarding start day.
var _ lastPeriodStartClearer = (*db.DailyLogRepository)(nil)

// withdrawFixture onboards an owner on 2026-09-14 (period length 5) and returns
// a day service wired the way the composition root wires it: every day write
// runs inside the day-log repository's transaction.
func withdrawFixture(t *testing.T, email string, autoFill bool) (*DayService, *db.Repositories, uint) {
	t.Helper()
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	user := createDayServiceTestUser(t, database, email)
	if err := repositories.Users.UpdateByID(context.Background(), user.ID, map[string]any{"auto_period_fill": autoFill}); err != nil {
		t.Fatalf("set auto-fill: %v", err)
	}
	if err := repositories.Users.CompleteOnboarding(context.Background(), user.ID, startMoveDay(time.September, 14), startMoveDay(time.September, 18), autoFill); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}
	service := NewDayServiceWithTx(repositories.DailyLogs, repositories.Users, func(ctx context.Context, fn func(DayLogRepository) error) error {
		return repositories.DailyLogs.WithinTransaction(ctx, func(tx *db.DailyLogRepository) error {
			return fn(tx)
		})
	})
	return service, repositories, user.ID
}

func withdrawSave(t *testing.T, service *DayService, userID uint, day time.Time, input DayEntryInput) {
	t.Helper()
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), userID, day, input, now, time.UTC); err != nil {
		t.Fatalf("save %s: %v", day.Format("2006-01-02"), err)
	}
}

func withdrawReload(t *testing.T, repositories *db.Repositories, userID uint) (models.User, []models.DailyLog) {
	t.Helper()
	stored, err := repositories.Users.LoadSettingsByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	stored.Role = models.RoleOwner
	logs, err := repositories.DailyLogs.ListByUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	return stored, logs
}

// TestUntickingThePeriodOnTheOnboardingDayWithdrawsTheStart: the owner logged
// the period on the onboarding date, then un-ticked it. That is the explicit
// un-mark: the stored start is cleared in the same write, so neither the
// boundary rule nor the calendar keeps a period day there.
func TestUntickingThePeriodOnTheOnboardingDayWithdrawsTheStart(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "withdraw-start@example.com", false)
	day := startMoveDay(time.September, 14)
	withdrawSave(t, service, userID, day, DayEntryInput{IsPeriod: true, Flow: models.FlowMedium})
	withdrawSave(t, service, userID, day, DayEntryInput{IsPeriod: false, Flow: models.FlowNone, Mood: 3})

	stored, logs := withdrawReload(t, repositories, userID)
	if stored.LastPeriodStart != nil {
		t.Fatalf("last_period_start = %v after the un-tick, want it cleared", stored.LastPeriodStart)
	}
	assertBoundaries(t, logs, BoundaryContextFor(&stored, startMoveDay(time.October, 5)))
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	stats := BuildCycleStatsFromLogs(&stored, logs, now, time.UTC)
	for _, state := range BuildCalendarDayStates(&stored, startMoveDay(time.September, 1), logs, stats, now, time.UTC) {
		if state.DateString == "2026-09-14" && state.IsPeriod {
			t.Fatal("the calendar still paints the un-ticked onboarding day as a period day")
		}
	}
}

// TestUntickingAnotherPeriodDayKeepsTheOnboardingStart: only the start's own
// date withdraws it; un-ticking a later day of the onboarding fill does not.
func TestUntickingAnotherPeriodDayKeepsTheOnboardingStart(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "withdraw-other-day@example.com", true)
	withdrawSave(t, service, userID, startMoveDay(time.September, 16), DayEntryInput{IsPeriod: false, Flow: models.FlowNone, Mood: 3})

	stored, logs := withdrawReload(t, repositories, userID)
	if stored.LastPeriodStart == nil || !dateOnly(*stored.LastPeriodStart).Equal(startMoveDay(time.September, 14)) {
		t.Fatalf("last_period_start = %v, want 2026-09-14 kept", stored.LastPeriodStart)
	}
	if boundaries := boundaryKeys(CycleBoundaries(logs, BoundaryContextFor(&stored, startMoveDay(time.October, 5)))); len(boundaries) == 0 || boundaries[0] != "2026-09-14" {
		t.Fatalf("boundaries = %v, want 2026-09-14 first", boundaries)
	}
}

func withdrawCalendarPaintsPeriod(stored models.User, logs []models.DailyLog, dateString string) bool {
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)
	stats := BuildCycleStatsFromLogs(&stored, logs, now, time.UTC)
	for _, state := range BuildCalendarDayStates(&stored, startMoveDay(time.September, 1), logs, stats, now, time.UTC) {
		if state.DateString == dateString && state.IsPeriod {
			return true
		}
	}
	return false
}

// TestDeletingTheOnboardingStartDayWithdrawsTheStart: deleting the day (DELETE
// /api/v1/days/:date) is an un-mark like un-ticking its period, so the stored
// start goes with the row and the calendar stops painting the day. Deleting
// another day of the fill keeps the start.
func TestDeletingTheOnboardingStartDayWithdrawsTheStart(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "withdraw-delete@example.com", true)
	if err := service.DeleteDayEntry(context.Background(), userID, startMoveDay(time.September, 16), time.UTC); err != nil {
		t.Fatalf("delete 09-16: %v", err)
	}
	if stored, _ := withdrawReload(t, repositories, userID); stored.LastPeriodStart == nil {
		t.Fatal("deleting a later fill day cleared last_period_start, want it kept")
	}

	if err := service.DeleteDayEntry(context.Background(), userID, startMoveDay(time.September, 14), time.UTC); err != nil {
		t.Fatalf("delete 09-14: %v", err)
	}
	stored, logs := withdrawReload(t, repositories, userID)
	if stored.LastPeriodStart != nil {
		t.Fatalf("last_period_start = %v after deleting its day, want it cleared", stored.LastPeriodStart)
	}
	if withdrawCalendarPaintsPeriod(stored, logs, "2026-09-14") {
		t.Fatal("the calendar still paints the deleted onboarding day as a period day")
	}
}

// TestTheDayEditorTicksAStoredStartWithoutARow: with auto-fill off onboarding
// writes no day, yet the calendar paints the start. The editor shows the period
// ticked there, and saving the day with it unticked withdraws the start.
func TestTheDayEditorTicksAStoredStartWithoutARow(t *testing.T) {
	service, repositories, userID := withdrawFixture(t, "withdraw-no-row@example.com", false)
	day := startMoveDay(time.September, 14)
	stored, _ := withdrawReload(t, repositories, userID)
	stored.ID = userID
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

	editor := NewDashboardViewService(&stubDashboardStatsProvider{}, &stubDashboardViewerProvider{}, &stubDashboardDayStateProvider{})
	view, err := editor.BuildDayEditorViewData(context.Background(), &stored, "en", day, now, time.UTC)
	if err != nil {
		t.Fatalf("BuildDayEditorViewData: %v", err)
	}
	if !view.Log.IsPeriod {
		t.Fatal("the day editor shows the period unticked on a stored start the calendar paints")
	}
	if !view.PeriodFromStoredStart {
		t.Fatal("the day editor does not say its period tick came from the stored start")
	}
	other, err := editor.BuildDayEditorViewData(context.Background(), &stored, "en", day.AddDate(0, 0, 1), now, time.UTC)
	if err != nil || other.Log.IsPeriod || other.PeriodFromStoredStart {
		t.Fatalf("the day after the start shows the period ticked (err=%v): only the start's own date is", err)
	}

	// A write of the date that never showed the tick carries no field: a mood
	// alone is not an un-mark.
	withdrawSave(t, service, userID, day, DayEntryInput{IsPeriod: false, Flow: models.FlowNone, Mood: 3})
	if kept, _ := withdrawReload(t, repositories, userID); kept.LastPeriodStart == nil {
		t.Fatal("a mood-only write without the form's stored-start field withdrew the start")
	}
	if err := repositories.DailyLogs.DeleteByUserAndDayRange(context.Background(), userID, day, day.AddDate(0, 0, 1)); err != nil {
		t.Fatalf("reset the start date's row: %v", err)
	}

	withdrawSave(t, service, userID, day, DayEntryInput{IsPeriod: false, Flow: models.FlowNone, Mood: 3, PeriodFromStoredStart: true})
	stored, logs := withdrawReload(t, repositories, userID)
	if stored.LastPeriodStart != nil {
		t.Fatalf("last_period_start = %v after the editor's un-tick, want it cleared", stored.LastPeriodStart)
	}
	if withdrawCalendarPaintsPeriod(stored, logs, "2026-09-14") {
		t.Fatal("the calendar still paints the un-ticked onboarding day as a period day")
	}
}
