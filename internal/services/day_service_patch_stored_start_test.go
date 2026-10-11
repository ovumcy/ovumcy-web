package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A day without a row on the stored onboarding start renders its period ticked
// (withOnboardingStartTicked). A partial write reads that day the same way: a
// save that leaves is_period out keeps the tick the owner saw, a base of
// is_period=true is current rather than a change made elsewhere, and a stated
// is_period=false un-ticks the start.

func storedStartPatchFixture(day time.Time) (*startClearingDayLogs, *DayService) {
	logs := &startClearingDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub()}
	users := &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &day}}
	return logs, NewDayService(logs, users)
}

func TestPatchDayEntryKeepsTheStoredStartTickOnARowlessDay(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs, service := storedStartPatchFixture(day)

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true},
		DayEntryBase{Values: DayEntryInput{IsPeriod: true}, Fields: DayEntryFields{Mood: true}}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected a mood save on the row-less stored start to save, got %v", err)
	}
	if !saved.IsPeriod || saved.Mood != 4 {
		t.Fatalf("expected the rendered period tick kept beside the mood, got is_period=%v mood=%d", saved.IsPeriod, saved.Mood)
	}
	if len(logs.clearedDays) != 0 {
		t.Fatalf("a save that leaves is_period out must keep the stored start, got clears %v", logs.clearedDays)
	}
}

func TestPatchDayEntryUnTicksARowlessStoredStartAgainstItsRenderedBase(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs, service := storedStartPatchFixture(day)
	fields := DayEntryFields{IsPeriod: true}

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{IsPeriod: false}, fields,
		DayEntryBase{Values: DayEntryInput{IsPeriod: true}, Fields: fields}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected the un-tick based on the rendered tick to save, got %v", err)
	}
	if saved.IsPeriod {
		t.Fatal("expected the un-tick written")
	}
	if len(logs.clearedDays) != 1 || !logs.clearedDays[0].Equal(day) {
		t.Fatalf("expected the un-tick to withdraw the start on %s, got clears %v", day, logs.clearedDays)
	}
}

func TestPatchDayEntryLeavesARowlessDayOffTheStoredStartUnticked(t *testing.T) {
	start := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	day := start.AddDate(0, 0, 1)
	_, service := storedStartPatchFixture(start)

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true}, DayEntryBase{}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected the mood save to save, got %v", err)
	}
	if saved.IsPeriod {
		t.Fatal("a row-less day other than the stored start must not gain a period")
	}
}
