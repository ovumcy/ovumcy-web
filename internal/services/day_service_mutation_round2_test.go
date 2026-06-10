package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestClearAutoFilledPeriodNeighbors_LoopBoundIsExclusive_DoesNotClearDayAtPeriodLengthOffset(t *testing.T) {
	// seedAutoFilledPeriod(t,3) auto-fills the start (02-10) plus the two
	// following days 02-11 and 02-12 as bare period candidates.
	service, logs, start := seedAutoFilledPeriod(t, 3)

	// Inject a contiguous bare auto-fill candidate at offset == periodLength
	// (start + 3 days = 02-13). The documented window is the periodLength-1
	// days following the start, so 02-13 is OUTSIDE the window and must never
	// be cleared. A `<` -> `<=` boundary mutation would walk one day too far
	// and clear it.
	extraDay := start.AddDate(0, 0, 3)
	logs.entries["2026-02-13"] = models.DailyLog{
		ID:       99,
		UserID:   10,
		Date:     CalendarDay(extraDay, time.UTC),
		IsPeriod: true,
		Flow:     models.FlowLight,
	}

	if err := service.ClearAutoFilledPeriodNeighbors(context.Background(), 10, CalendarDay(start, time.UTC), 3, time.UTC); err != nil {
		t.Fatalf("ClearAutoFilledPeriodNeighbors: %v", err)
	}

	// In-window days are cleared (sanity: the loop did run and reach offsets 1,2).
	for _, key := range []string{"2026-02-11", "2026-02-12"} {
		if logs.entries[key].IsPeriod {
			t.Fatalf("precondition: expected in-window day %s to be cleared", key)
		}
	}

	// The day at offset == periodLength must remain an untouched period day.
	// Under the `<=` mutant it would have been cleared.
	if !logs.entries["2026-02-13"].IsPeriod {
		t.Fatal("expected day at offset==periodLength (2026-02-13) to remain a period day; loop bound must be exclusive")
	}
	if logs.entries["2026-02-13"].Flow != models.FlowLight {
		t.Fatalf("expected 2026-02-13 flow preserved as %q, got %q", models.FlowLight, logs.entries["2026-02-13"].Flow)
	}
}

func TestAutoFillFollowingPeriodDays_NonUTCLocationNotOverwritten_FillsThroughLocalToday(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})

	// Tokyo is UTC+9 with no DST, so the civil-date shift is deterministic.
	tokyo := time.FixedZone("Asia/Tokyo", 9*60*60)

	// Date-only anchor; offsets 1 and 2 target 02-11 and 02-12.
	start := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)

	// now = 2026-02-11 20:00 UTC. In Tokyo that is 2026-02-12 05:00, so the
	// user's local "today" is 02-12. With the real location, today=02-12 and
	// the offset-2 target (02-12) is NOT after today, so it is auto-filled.
	// If line 577 overwrites the location with UTC (the `!=` mutant), today
	// collapses to 02-11 and the offset-2 target 02-12 IS after today, so the
	// loop breaks and 02-12 is never created.
	now := time.Date(2026, time.February, 11, 20, 0, 0, 0, time.UTC)

	if err := service.AutoFillFollowingPeriodDays(context.Background(), 10, start, 3, models.FlowLight, now, tokyo); err != nil {
		t.Fatalf("AutoFillFollowingPeriodDays: %v", err)
	}

	// Offset-1 day is filled under both correct code and the mutant.
	if entry, ok := logs.entries["2026-02-11"]; !ok || !entry.IsPeriod {
		t.Fatalf("expected offset-1 day 2026-02-11 to be auto-filled as a period day, got ok=%t entry=%#v", ok, entry)
	}

	// Offset-2 day must be filled because the user's local today (Tokyo) is
	// 02-12. The `==`->`!=` mutant overwrites location with UTC, shifts today
	// back to 02-11, breaks before this fill, and leaves 02-12 absent.
	entry, ok := logs.entries["2026-02-12"]
	if !ok {
		t.Fatal("expected offset-2 day 2026-02-12 to be auto-filled when the caller location is a non-UTC zone; a location overwritten to UTC stops the fill early")
	}
	if !entry.IsPeriod {
		t.Fatalf("expected 2026-02-12 to be a period day, got IsPeriod=%t", entry.IsPeriod)
	}
}
