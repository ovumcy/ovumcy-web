package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Coverage for ClearAutoFilledPeriodNeighbors, the heuristic that walks the days
// following a period start and clears auto-filled ("bare") period days while
// preserving any day the user has manually edited. The mutation baseline showed
// this body had no test exercising it at all (NOT COVERED), despite it mutating
// persisted health data.

func seedAutoFilledPeriod(t *testing.T, periodLength int) (*DayService, *dayLogRepositoryStub, time.Time) {
	t.Helper()
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{settings: models.User{PeriodLength: periodLength, AutoPeriodFill: true}}
	service := NewDayService(logs, users)

	start := time.Date(2026, time.February, 10, 8, 0, 0, 0, time.UTC)
	now := start.AddDate(0, 0, periodLength) // far enough ahead that all fill days are in the past
	if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, start,
		DayEntryInput{IsPeriod: true, Flow: models.FlowLight}, now, time.UTC); err != nil {
		t.Fatalf("seed auto-filled period: %v", err)
	}
	return service, logs, start
}

func TestClearAutoFilledPeriodNeighbors_ClearsBareFillDays(t *testing.T) {
	service, logs, start := seedAutoFilledPeriod(t, 3)

	for _, key := range []string{"2026-02-11", "2026-02-12"} {
		if !logs.entries[key].IsPeriod {
			t.Fatalf("precondition: %s should be an auto-filled period day", key)
		}
	}

	if err := service.ClearAutoFilledPeriodNeighbors(context.Background(), 10, CalendarDay(start, time.UTC), 3, time.UTC); err != nil {
		t.Fatalf("ClearAutoFilledPeriodNeighbors: %v", err)
	}

	// The start day is never touched; the bare following days are cleared.
	if !logs.entries["2026-02-10"].IsPeriod {
		t.Fatal("the period start day must not be cleared")
	}
	for _, key := range []string{"2026-02-11", "2026-02-12"} {
		entry := logs.entries[key]
		if entry.IsPeriod || entry.Flow != models.FlowNone {
			t.Fatalf("expected %s cleared, got IsPeriod=%t Flow=%q", key, entry.IsPeriod, entry.Flow)
		}
	}
}

func TestClearAutoFilledPeriodNeighbors_PreservesManualEditAndStops(t *testing.T) {
	service, logs, start := seedAutoFilledPeriod(t, 4) // fills 02-10..02-13

	// Give the first following day a manual signal so it is no longer a bare
	// auto-fill candidate. Clearing must stop there and spare the later days.
	manual := logs.entries["2026-02-11"]
	manual.Mood = MinDayMood
	logs.entries["2026-02-11"] = manual

	if err := service.ClearAutoFilledPeriodNeighbors(context.Background(), 10, CalendarDay(start, time.UTC), 4, time.UTC); err != nil {
		t.Fatalf("ClearAutoFilledPeriodNeighbors: %v", err)
	}

	if !logs.entries["2026-02-11"].IsPeriod {
		t.Fatal("a manually edited day must be preserved")
	}
	if !logs.entries["2026-02-12"].IsPeriod {
		t.Fatal("clearing must stop at the manual edit, leaving later days untouched")
	}
}

func TestShouldClearAutoFilledNeighbors_DependsOnPreviousDay(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	start := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	// Previous day is not a period -> this start is bare, neighbors may be cleared.
	logs.entries["2026-02-09"] = models.DailyLog{UserID: 10, Date: start.AddDate(0, 0, -1), IsPeriod: false}
	shouldClear, err := service.shouldClearAutoFilledNeighbors(context.Background(), 10, start, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !shouldClear {
		t.Fatal("expected true when the preceding day is not a period")
	}

	// Previous day is itself a period -> the start is mid-period, do not clear.
	logs.entries["2026-02-09"] = models.DailyLog{UserID: 10, Date: start.AddDate(0, 0, -1), IsPeriod: true}
	shouldClear, err = service.shouldClearAutoFilledNeighbors(context.Background(), 10, start, time.UTC)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shouldClear {
		t.Fatal("expected false when the preceding day is a period")
	}
}

func TestClearAutoFilledNeighborsIfBare_OnlyClearsWhenStartIsBare(t *testing.T) {
	service, logs, start := seedAutoFilledPeriod(t, 3) // fills 02-10..02-12

	// Preceding day is a period -> start is mid-period -> must NOT clear neighbors.
	logs.entries["2026-02-09"] = models.DailyLog{UserID: 10, Date: start.AddDate(0, 0, -1), IsPeriod: true}
	if err := service.clearAutoFilledNeighborsIfBare(context.Background(), 10, CalendarDay(start, time.UTC), 3, time.UTC); err != nil {
		t.Fatalf("clearAutoFilledNeighborsIfBare: %v", err)
	}
	if !logs.entries["2026-02-11"].IsPeriod {
		t.Fatal("neighbors must be preserved when the start is mid-period")
	}

	// Now the preceding day is not a period -> start is bare -> neighbors cleared.
	logs.entries["2026-02-09"] = models.DailyLog{UserID: 10, Date: start.AddDate(0, 0, -1), IsPeriod: false}
	if err := service.clearAutoFilledNeighborsIfBare(context.Background(), 10, CalendarDay(start, time.UTC), 3, time.UTC); err != nil {
		t.Fatalf("clearAutoFilledNeighborsIfBare: %v", err)
	}
	if logs.entries["2026-02-11"].IsPeriod {
		t.Fatal("bare-start neighbors must be cleared")
	}
}

func TestClearAutoFilledPeriodNeighbors_NoOpForSingleDayPeriod(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{settings: models.User{PeriodLength: 1}}
	service := NewDayService(logs, users)
	start := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	// periodLength <= 1 must return immediately without touching anything.
	if err := service.ClearAutoFilledPeriodNeighbors(context.Background(), 10, start, 1, time.UTC); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
	if len(logs.entries) != 0 {
		t.Fatalf("expected no writes for a single-day period, got %d entries", len(logs.entries))
	}
}

func TestDeleteDayEntry_RemovesPersistedEntry(t *testing.T) {
	service, logs, start := seedAutoFilledPeriod(t, 1) // single day 02-10
	if _, ok := logs.entries["2026-02-10"]; !ok {
		t.Fatal("precondition: the entry should exist before deletion")
	}

	if err := service.DeleteDayEntry(context.Background(), 10, start, time.UTC); err != nil {
		t.Fatalf("DeleteDayEntry: %v", err)
	}

	if _, ok := logs.entries["2026-02-10"]; ok {
		t.Fatal("expected the day entry to be deleted")
	}
}

func TestDayServiceResolveManualCycleStartPolicy_UsesUsersLogs(t *testing.T) {
	logs := newDayLogRepositoryStub()
	logs.entries["2026-01-01"] = models.DailyLog{
		UserID: 10, Date: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		IsPeriod: true, CycleStart: true,
	}
	service := NewDayService(logs, &dayUserRepositoryStub{})

	user := &models.User{}
	user.ID = 10
	day := time.Date(2026, time.January, 10, 0, 0, 0, 0, time.UTC) // 9-day gap from the start

	policy, err := service.ResolveManualCycleStartPolicy(context.Background(), user, day, day, time.UTC)
	if err != nil {
		t.Fatalf("ResolveManualCycleStartPolicy: %v", err)
	}
	if policy.ShortGapDays != 9 {
		t.Fatalf("expected short gap of 9 days from the user's logs, got %d", policy.ShortGapDays)
	}
}
