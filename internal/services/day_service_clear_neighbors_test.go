package services

import (
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
	if _, err := service.UpsertDayEntryWithAutoFillAt(10, start,
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

	if err := service.ClearAutoFilledPeriodNeighbors(10, CalendarDay(start, time.UTC), 3, time.UTC); err != nil {
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

	if err := service.ClearAutoFilledPeriodNeighbors(10, CalendarDay(start, time.UTC), 4, time.UTC); err != nil {
		t.Fatalf("ClearAutoFilledPeriodNeighbors: %v", err)
	}

	if !logs.entries["2026-02-11"].IsPeriod {
		t.Fatal("a manually edited day must be preserved")
	}
	if !logs.entries["2026-02-12"].IsPeriod {
		t.Fatal("clearing must stop at the manual edit, leaving later days untouched")
	}
}

func TestClearAutoFilledPeriodNeighbors_NoOpForSingleDayPeriod(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{settings: models.User{PeriodLength: 1}}
	service := NewDayService(logs, users)
	start := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	// periodLength <= 1 must return immediately without touching anything.
	if err := service.ClearAutoFilledPeriodNeighbors(10, start, 1, time.UTC); err != nil {
		t.Fatalf("expected no-op, got error: %v", err)
	}
	if len(logs.entries) != 0 {
		t.Fatalf("expected no writes for a single-day period, got %d entries", len(logs.entries))
	}
}
