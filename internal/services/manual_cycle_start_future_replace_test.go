package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A manual mark replaces the stored onboarding start only on or before the
// owner's local today. A future-day mark over the stored start is refused and
// writes nothing, so the owner marks it on the day; a future mark over any
// other start keeps the ordinary Replace. Onboarding, Settings and import
// never store a future start, and the cycle anchor ignores one.

func markOverStoredStart(t *testing.T, email string, mark time.Time, now time.Time, location *time.Location) (models.User, []models.DailyLog, error) {
	t.Helper()
	repositories, userID := markMoveOwner(t, email, true)
	err := markMoveDayService(repositories).MarkCycleStartManually(context.Background(), userID, mark, now, location, ManualCycleStartOptions{ReplaceExisting: true, MarkUncertain: true})
	user := markMoveStored(t, repositories, userID)
	if user.LastPeriodStart == nil {
		t.Fatal("stored start was cleared")
	}
	return user, startMoveListLogs(t, repositories, userID), err
}

// TestManualCycleStartOnAFutureDayOverTheStoredStartIsRefused: onboarding start
// 09-14 is today-3; marking tomorrow is refused even with Replace accepted, and
// three days on the cycle still starts on 09-14.
func TestManualCycleStartOnAFutureDayOverTheStoredStartIsRefused(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	user, logs, err := markOverStoredStart(t, "mark-replace-future@example.com", startMoveDay(time.September, 18), now, time.UTC)
	if !errors.Is(err, ErrManualCycleStartMarkOnTheDay) {
		t.Fatalf("expected ErrManualCycleStartMarkOnTheDay, got %v", err)
	}
	if !dateOnly(*user.LastPeriodStart).Equal(startMoveDay(time.September, 14)) {
		t.Fatalf("stored start = %v, want it unchanged at 2026-09-14", user.LastPeriodStart)
	}
	if old, found := startMoveRowOn(logs, "2026-09-14"); !found || !old.CycleStart {
		t.Fatalf("the stored start's mark on 09-14 was cleared: %+v", old)
	}
	if marked, found := startMoveRowOn(logs, "2026-09-18"); found && marked.CycleStart {
		t.Fatalf("09-18 was marked: %+v", marked)
	}
	assertBoundaries(t, logs, BoundaryContextFor(&user, startMoveDay(time.September, 20)), "2026-09-14")
}

// TestManualCycleStartOnTodayOverTheStoredStartReplacesIt: today is not future,
// so the stored start moves and three days on the cycle starts on the mark.
func TestManualCycleStartOnTodayOverTheStoredStartReplacesIt(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	user, logs, err := markOverStoredStart(t, "mark-replace-today@example.com", startMoveDay(time.September, 17), now, time.UTC)
	if err != nil {
		t.Fatalf("MarkCycleStartManually: %v", err)
	}
	if !dateOnly(*user.LastPeriodStart).Equal(startMoveDay(time.September, 17)) {
		t.Fatalf("stored start = %v, want 2026-09-17", user.LastPeriodStart)
	}
	assertBoundaries(t, logs, BoundaryContextFor(&user, startMoveDay(time.September, 20)), "2026-09-17")
}

// TestManualCycleStartJudgesTodayInTheOwnersZone: at 23:30 UTC on 09-17 it is
// already 09-18 in UTC+3, so a mark on 09-18 is today and replaces the start.
func TestManualCycleStartJudgesTodayInTheOwnersZone(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	now := time.Date(2026, time.September, 17, 23, 30, 0, 0, time.UTC)
	user, _, err := markOverStoredStart(t, "mark-replace-zone@example.com", startMoveDay(time.September, 18), now, location)
	if err != nil {
		t.Fatalf("MarkCycleStartManually: %v", err)
	}
	if !dateOnly(*user.LastPeriodStart).Equal(startMoveDay(time.September, 18)) {
		t.Fatalf("stored start = %v, want 2026-09-18", user.LastPeriodStart)
	}
}

// TestManualCycleStartOnAFutureDayOverAnotherStartStillReplaces: the competing
// mark is not the stored start, so the future mark keeps the ordinary Replace.
func TestManualCycleStartOnAFutureDayOverAnotherStartStillReplaces(t *testing.T) {
	repositories, userID := markMoveOwner(t, "mark-replace-future-other@example.com", true)
	earlier := startMoveDay(time.August, 1)
	if err := repositories.Users.UpdateByID(context.Background(), userID, map[string]any{"last_period_start": earlier}); err != nil {
		t.Fatalf("set stored start: %v", err)
	}
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)

	err := markMoveDayService(repositories).MarkCycleStartManually(context.Background(), userID, startMoveDay(time.September, 18), now, time.UTC, ManualCycleStartOptions{ReplaceExisting: true, MarkUncertain: true})
	if err != nil {
		t.Fatalf("MarkCycleStartManually: %v", err)
	}
	logs := startMoveListLogs(t, repositories, userID)
	if old, found := startMoveRowOn(logs, "2026-09-14"); !found || old.CycleStart {
		t.Fatalf("the replaced mark on 09-14 was not cleared: %+v", old)
	}
	if marked, found := startMoveRowOn(logs, "2026-09-18"); !found || !marked.CycleStart {
		t.Fatalf("09-18 was not marked: %+v", marked)
	}
}

// TestCompetingCycleStartsKeepTheStoredStartForAFutureSelection: the replace
// itself never moves the stored start onto a future day, even when a caller
// reaches it without the policy's refusal.
func TestCompetingCycleStartsKeepTheStoredStartForAFutureSelection(t *testing.T) {
	earlier, selected, all := competingStartFixture()
	logs := &movingStartDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub()}
	logs.entries["2026-03-08"] = earlier
	logs.entries["2026-03-10"] = selected
	service := NewDayService(logs, &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &earlier.Date}})
	now := time.Date(2026, time.March, 9, 12, 0, 0, 0, time.UTC)

	if err := service.clearCompetingCycleStarts(context.Background(), 10, all, selected, now, time.UTC); err != nil {
		t.Fatalf("clearCompetingCycleStarts: %v", err)
	}
	if logs.moves != 0 {
		t.Fatalf("expected no stored-start move for a future selection, got %d", logs.moves)
	}
}
