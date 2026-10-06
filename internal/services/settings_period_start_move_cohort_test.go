package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func startMoveRowOn(logs []models.DailyLog, day string) (models.DailyLog, bool) {
	for _, entry := range logs {
		if entry.Date.UTC().Format("2006-01-02") == day {
			return entry, true
		}
	}
	return models.DailyLog{}, false
}

func startMoveListLogs(t *testing.T, repositories *db.Repositories, userID uint) []models.DailyLog {
	t.Helper()
	logs, err := repositories.DailyLogs.ListByUser(context.Background(), userID)
	if err != nil {
		t.Fatalf("list logs: %v", err)
	}
	return logs
}

func startMoveSaveCycleStart(t *testing.T, repositories *db.Repositories, userID uint, moved time.Time) {
	t.Helper()
	settings := NewSettingsService(repositories.Users)
	settings.AttachDayLogReader(NewDayService(repositories.DailyLogs, repositories.Users))
	if err := settings.SaveCycleSettings(context.Background(), userID, CycleSettingsUpdate{LastPeriodStartSet: true, LastPeriodStart: &moved}); err != nil {
		t.Fatalf("SaveCycleSettings: %v", err)
	}
}

// TestSettingsStartMoveKeepsAHandTickedOldStart: onboarding with auto-fill off
// wrote no day; the owner ticked 09-14 and 09-15 by hand in two saves, then
// turned auto-fill on and moved the start to 09-16. Two separate writes are no
// fill cohort, and the old start's single bare row is a hand tick, not the
// fill's: the move deletes nothing.
func TestSettingsStartMoveKeepsAHandTickedOldStart(t *testing.T) {
	ctx := context.Background()
	service, repositories, userID := withdrawFixture(t, "start-move-hand-ticked@example.com", false)
	for _, day := range []int{14, 15} {
		withdrawSave(t, service, userID, startMoveDay(time.September, day), DayEntryInput{IsPeriod: true, Flow: models.FlowNone})
	}
	before := startMoveListLogs(t, repositories, userID)
	first, foundFirst := startMoveRowOn(before, "2026-09-14")
	second, foundSecond := startMoveRowOn(before, "2026-09-15")
	if !foundFirst || !foundSecond || !IsAutoFilledPeriodCandidate(first, "") || !IsAutoFilledPeriodCandidate(second, "") {
		t.Fatalf("fixture: the hand ticks are not two bare period rows (logged %v)", startMoveLoggedDays(before))
	}
	if first.CreatedAt.Equal(second.CreatedAt) {
		t.Fatal("fixture: the two hand-tick saves share one creation stamp, so they cannot tell a hand tick from a fill")
	}
	if err := repositories.Users.UpdateByID(ctx, userID, map[string]any{"auto_period_fill": true}); err != nil {
		t.Fatalf("enable auto-fill: %v", err)
	}

	startMoveSaveCycleStart(t, repositories, userID, startMoveDay(time.September, 16))

	logs := startMoveListLogs(t, repositories, userID)
	if !startMoveHasPeriodDay(logs, "2026-09-14") {
		t.Fatalf("the hand-ticked old start 2026-09-14 was deleted by the start move (logged %v)", startMoveLoggedDays(logs))
	}
}

// TestSettingsStartMoveKeepsALegacyStampedFill: a fill written before one write
// stamped its whole range carries a distinct creation stamp per row. Nothing
// proves those rows are the fill rather than the owner's ticks, so moving the
// start leaves every one of them — as moves did before the cohort rule.
func TestSettingsStartMoveKeepsALegacyStampedFill(t *testing.T) {
	ctx := context.Background()
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	user := createDayServiceTestUser(t, database, "start-move-legacy-fill@example.com")
	if err := repositories.Users.UpdateByID(ctx, user.ID, map[string]any{"auto_period_fill": true}); err != nil {
		t.Fatalf("enable auto-fill: %v", err)
	}
	if err := repositories.Users.CompleteOnboarding(ctx, user.ID, startMoveDay(time.September, 14), startMoveDay(time.September, 18), true); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}
	fill := startMoveListLogs(t, repositories, user.ID)
	want := []string{"2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17", "2026-09-18"}
	if got := startMoveLoggedDays(fill); len(got) != len(want) {
		t.Fatalf("fixture: onboarding wrote %v, want the fill %v", got, want)
	}
	stamp := time.Date(2026, time.September, 14, 8, 0, 0, 0, time.UTC)
	for index, entry := range fill {
		legacy := stamp.Add(time.Duration(index) * time.Minute)
		if err := database.Model(&models.DailyLog{}).Where("id = ?", entry.ID).UpdateColumn("created_at", legacy).Error; err != nil {
			t.Fatalf("stamp %d: %v", entry.ID, err)
		}
	}

	startMoveSaveCycleStart(t, repositories, user.ID, startMoveDay(time.September, 3))

	logs := startMoveListLogs(t, repositories, user.ID)
	for _, day := range want {
		if !startMoveHasPeriodDay(logs, day) {
			t.Errorf("legacy-stamped fill day %s was deleted by the start move", day)
		}
	}
	if t.Failed() {
		t.Logf("logged %v", startMoveLoggedDays(logs))
	}
}
