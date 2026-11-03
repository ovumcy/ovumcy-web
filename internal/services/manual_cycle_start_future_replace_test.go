package services

import (
	"context"
	"testing"
	"time"
)

// A manual mark that replaces the stored onboarding start moves the stored
// start only to a day on or before the owner's local today: onboarding,
// Settings and import never store a future start, and the cycle anchor ignores
// one. The replaced row's cycle_start is cleared either way.

func replaceStoredStartOn(t *testing.T, email string, mark time.Time, now time.Time, location *time.Location) (stored time.Time, oldStillStart bool) {
	t.Helper()
	repositories, userID := markMoveOwner(t, email, true)
	err := markMoveDayService(repositories).MarkCycleStartManually(context.Background(), userID, mark, now, location, ManualCycleStartOptions{ReplaceExisting: true, MarkUncertain: true})
	if err != nil {
		t.Fatalf("MarkCycleStartManually: %v", err)
	}
	user := markMoveStored(t, repositories, userID)
	if user.LastPeriodStart == nil {
		t.Fatal("stored start was cleared")
	}
	logs := startMoveListLogs(t, repositories, userID)
	if marked, found := startMoveRowOn(logs, CalendarDayKey(mark)); !found || !marked.CycleStart {
		t.Fatalf("%s was not marked: %+v", CalendarDayKey(mark), marked)
	}
	old, found := startMoveRowOn(logs, "2026-09-14")
	return dateOnly(*user.LastPeriodStart), found && old.CycleStart
}

// TestManualCycleStartReplaceOnAFutureDayKeepsTheStoredStart: onboarding start
// 09-14 is today-3; marking tomorrow replaces it but the stored start stays.
func TestManualCycleStartReplaceOnAFutureDayKeepsTheStoredStart(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	stored, oldStillStart := replaceStoredStartOn(t, "mark-replace-future@example.com", startMoveDay(time.September, 18), now, time.UTC)
	if !stored.Equal(startMoveDay(time.September, 14)) {
		t.Fatalf("stored start = %v, want it unchanged at 2026-09-14", stored)
	}
	if oldStillStart {
		t.Fatal("the replaced mark on 09-14 was not cleared")
	}
}

// TestManualCycleStartReplaceOnTodayMovesTheStoredStart: today is not future.
func TestManualCycleStartReplaceOnTodayMovesTheStoredStart(t *testing.T) {
	now := time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)
	stored, _ := replaceStoredStartOn(t, "mark-replace-today@example.com", startMoveDay(time.September, 17), now, time.UTC)
	if !stored.Equal(startMoveDay(time.September, 17)) {
		t.Fatalf("stored start = %v, want 2026-09-17", stored)
	}
}

// TestManualCycleStartReplaceJudgesTodayInTheOwnersZone: at 23:30 UTC on 09-17
// it is already 09-18 in UTC+3, so a mark on 09-18 is today, not future.
func TestManualCycleStartReplaceJudgesTodayInTheOwnersZone(t *testing.T) {
	location := time.FixedZone("UTC+3", 3*60*60)
	now := time.Date(2026, time.September, 17, 23, 30, 0, 0, time.UTC)
	stored, _ := replaceStoredStartOn(t, "mark-replace-zone@example.com", startMoveDay(time.September, 18), now, location)
	if !stored.Equal(startMoveDay(time.September, 18)) {
		t.Fatalf("stored start = %v, want 2026-09-18", stored)
	}
}
