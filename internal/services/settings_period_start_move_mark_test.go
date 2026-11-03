package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// markMoveOwner onboards an owner on 09-14 (period through 09-18) with the given
// auto-fill setting, keeping the logged start day onboarding writes.
func markMoveOwner(t *testing.T, email string, autoFill bool) (*db.Repositories, uint) {
	t.Helper()
	ctx := context.Background()
	_, database := newDayServiceIntegration(t)
	repositories := db.NewRepositories(database)
	user := createDayServiceTestUser(t, database, email)
	if err := repositories.Users.UpdateByID(ctx, user.ID, map[string]any{"auto_period_fill": autoFill}); err != nil {
		t.Fatalf("set auto-fill: %v", err)
	}
	if err := repositories.Users.CompleteOnboarding(ctx, user.ID, startMoveDay(time.September, 14), startMoveDay(time.September, 18), autoFill, "", ""); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}
	return repositories, user.ID
}

func markMoveStartCount(logs []models.DailyLog) int {
	count := 0
	for _, entry := range logs {
		if entry.CycleStart {
			count++
		}
	}
	return count
}

func markMoveStored(t *testing.T, repositories *db.Repositories, userID uint) models.User {
	t.Helper()
	stored, err := repositories.Users.LoadSettingsByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("load settings: %v", err)
	}
	stored.Role = models.RoleOwner
	return stored
}

// TestSettingsStartMoveWithAutoFillOffMovesTheLoggedMark: onboarding with
// auto-fill off logged the start day as a cycle start. Correcting the start in
// Settings takes that mark off the old date — and the row with it, since it
// carries nothing else — and marks the new date as a period cycle start.
func TestSettingsStartMoveWithAutoFillOffMovesTheLoggedMark(t *testing.T) {
	repositories, userID := markMoveOwner(t, "mark-move-off@example.com", false)
	if old, found := startMoveRowOn(startMoveListLogs(t, repositories, userID), "2026-09-14"); !found || !old.CycleStart {
		t.Fatalf("fixture: onboarding did not log 09-14 as a cycle start: %+v", old)
	}

	startMoveSaveCycleStart(t, repositories, userID, startMoveDay(time.September, 6))

	logs := startMoveListLogs(t, repositories, userID)
	if old, found := startMoveRowOn(logs, "2026-09-14"); found {
		t.Fatalf("the old start's bare row survived the move: %+v", old)
	}
	moved, found := startMoveRowOn(logs, "2026-09-06")
	if !found || !moved.IsPeriod || !moved.CycleStart {
		t.Fatalf("the new start is not a period cycle start: %+v (found=%v)", moved, found)
	}
	if got := markMoveStartCount(logs); got != 1 {
		t.Fatalf("cycle starts = %d, want 1 (logged %v)", got, startMoveLoggedDays(logs))
	}
	stored := markMoveStored(t, repositories, userID)
	assertBoundaries(t, logs, BoundaryContextFor(&stored, startMoveDay(time.October, 5)), "2026-09-06")
}

// TestSettingsStartMoveKeepsTheNoteOnTheOldStart: the old start day also carries
// a note. The mark is gone, the note and the period day stay.
func TestSettingsStartMoveKeepsTheNoteOnTheOldStart(t *testing.T) {
	repositories, userID := markMoveOwner(t, "mark-move-note@example.com", false)
	day := startMoveDay(time.September, 14)
	entry, found, err := repositories.DailyLogs.FindByUserAndDayRange(context.Background(), userID, day, day.AddDate(0, 0, 1))
	if err != nil || !found {
		t.Fatalf("fixture: no 09-14 row (found=%v err=%v)", found, err)
	}
	entry.Notes = "felt unwell"
	entry.IsUncertain = true
	if err := repositories.DailyLogs.Save(context.Background(), &entry); err != nil {
		t.Fatalf("add note: %v", err)
	}

	startMoveSaveCycleStart(t, repositories, userID, startMoveDay(time.September, 6))

	logs := startMoveListLogs(t, repositories, userID)
	kept, found := startMoveRowOn(logs, "2026-09-14")
	if !found || kept.Notes != "felt unwell" || !kept.IsPeriod {
		t.Fatalf("the old start's note was not kept: %+v (found=%v)", kept, found)
	}
	if kept.CycleStart || kept.IsUncertain {
		t.Fatalf("the old start kept its mark: %+v", kept)
	}
	if moved, found := startMoveRowOn(logs, "2026-09-06"); !found || !moved.CycleStart || !moved.IsPeriod {
		t.Fatalf("the new start is not marked: %+v", moved)
	}
}

// TestSettingsStartMoveWithAutoFillOnCleansTheRunAndMarksTheNewDay: the old
// auto-filled run goes, the new run is filled and its first day is the start.
func TestSettingsStartMoveWithAutoFillOnCleansTheRunAndMarksTheNewDay(t *testing.T) {
	repositories, userID := markMoveOwner(t, "mark-move-on@example.com", true)

	startMoveSaveCycleStart(t, repositories, userID, startMoveDay(time.September, 6))

	logs := startMoveListLogs(t, repositories, userID)
	for _, day := range []string{"2026-09-14", "2026-09-15", "2026-09-16", "2026-09-17", "2026-09-18"} {
		if _, found := startMoveRowOn(logs, day); found {
			t.Errorf("old run day %s survived", day)
		}
	}
	moved, found := startMoveRowOn(logs, "2026-09-06")
	if !found || !moved.IsPeriod || !moved.CycleStart {
		t.Fatalf("the new start is not a period cycle start: %+v", moved)
	}
	if got := markMoveStartCount(logs); got != 1 {
		t.Fatalf("cycle starts = %d, want 1 (logged %v)", got, startMoveLoggedDays(logs))
	}
}

// TestSettingsStartMoveToAFutureDayWritesNoDay: a start after the owner's today
// moves the stored start and releases the old mark, but writes no day, as
// onboarding bounds its own.
func TestSettingsStartMoveToAFutureDayWritesNoDay(t *testing.T) {
	repositories, userID := markMoveOwner(t, "mark-move-future@example.com", false)
	moved := startMoveDay(time.September, 25)
	settings := NewSettingsService(repositories.Users)
	settings.AttachDayLogReader(NewDayService(repositories.DailyLogs, repositories.Users))
	update := CycleSettingsUpdate{LastPeriodStartSet: true, LastPeriodStart: &moved}
	update.now = time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	update.location = time.UTC
	if err := settings.SaveCycleSettings(context.Background(), userID, update); err != nil {
		t.Fatalf("SaveCycleSettings: %v", err)
	}

	logs := startMoveListLogs(t, repositories, userID)
	if got := startMoveLoggedDays(logs); len(got) != 0 {
		t.Fatalf("a future start wrote or kept days: %v", got)
	}
	if stored := markMoveStored(t, repositories, userID); stored.LastPeriodStart == nil || !dateOnly(*stored.LastPeriodStart).Equal(moved) {
		t.Fatalf("stored start = %v, want 2026-09-25", stored.LastPeriodStart)
	}
}

func markMoveDayService(repositories *db.Repositories) *DayService {
	return NewDayServiceWithTx(repositories.DailyLogs, repositories.Users, func(ctx context.Context, fn func(DayLogRepository) error) error {
		return repositories.DailyLogs.WithinTransaction(ctx, func(tx *db.DailyLogRepository) error {
			return fn(tx)
		})
	})
}

// TestManualCycleStartReplaceMovesTheStoredStart: marking 09-16 replaces the
// onboarding start 09-14, which is the stored start; the stored start follows,
// so the grouping sees exactly one start.
func TestManualCycleStartReplaceMovesTheStoredStart(t *testing.T) {
	repositories, userID := markMoveOwner(t, "mark-replace-stored@example.com", true)
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

	err := markMoveDayService(repositories).MarkCycleStartManually(context.Background(), userID, startMoveDay(time.September, 16), now, time.UTC, ManualCycleStartOptions{ReplaceExisting: true, MarkUncertain: true})
	if err != nil {
		t.Fatalf("MarkCycleStartManually: %v", err)
	}

	stored := markMoveStored(t, repositories, userID)
	if stored.LastPeriodStart == nil || !dateOnly(*stored.LastPeriodStart).Equal(startMoveDay(time.September, 16)) {
		t.Fatalf("stored start = %v, want 2026-09-16", stored.LastPeriodStart)
	}
	logs := startMoveListLogs(t, repositories, userID)
	if got := markMoveStartCount(logs); got != 1 {
		t.Fatalf("cycle starts = %d, want 1 (logged %v)", got, startMoveLoggedDays(logs))
	}
	assertBoundaries(t, logs, BoundaryContextFor(&stored, startMoveDay(time.October, 5)), "2026-09-16")
}

// TestManualCycleStartReplaceOfAnotherStartLeavesTheStoredStart: the replaced
// start is a logged mark, not the stored start, so the stored start stays.
func TestManualCycleStartReplaceOfAnotherStartLeavesTheStoredStart(t *testing.T) {
	repositories, userID := markMoveOwner(t, "mark-replace-other@example.com", true)
	earlier := startMoveDay(time.August, 1)
	if err := repositories.Users.UpdateByID(context.Background(), userID, map[string]any{"last_period_start": earlier}); err != nil {
		t.Fatalf("set stored start: %v", err)
	}
	now := time.Date(2026, time.October, 5, 12, 0, 0, 0, time.UTC)

	err := markMoveDayService(repositories).MarkCycleStartManually(context.Background(), userID, startMoveDay(time.September, 16), now, time.UTC, ManualCycleStartOptions{ReplaceExisting: true, MarkUncertain: true})
	if err != nil {
		t.Fatalf("MarkCycleStartManually: %v", err)
	}

	stored := markMoveStored(t, repositories, userID)
	if stored.LastPeriodStart == nil || !dateOnly(*stored.LastPeriodStart).Equal(earlier) {
		t.Fatalf("stored start = %v, want it unchanged at 2026-08-01", stored.LastPeriodStart)
	}
	logs := startMoveListLogs(t, repositories, userID)
	if old, found := startMoveRowOn(logs, "2026-09-14"); !found || old.CycleStart {
		t.Fatalf("the replaced mark on 09-14 was not cleared: %+v", old)
	}
	if marked, found := startMoveRowOn(logs, "2026-09-16"); !found || !marked.CycleStart {
		t.Fatalf("09-16 was not marked: %+v", marked)
	}
}
