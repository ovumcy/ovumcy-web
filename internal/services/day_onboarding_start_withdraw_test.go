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
