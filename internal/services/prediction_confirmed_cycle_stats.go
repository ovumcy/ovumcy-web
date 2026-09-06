package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ResolveConfirmedCycleStats moves the CURRENT cycle's ovulation day, fertile
// window and fertility status onto a thermal shift the owner's own temperatures
// confirm, and reports whether one was found.
//
// A confirmed shift asserts ONE thing: the ovulation has already happened, on
// the day the shared "3-over-6" detector infers from the temperature signal. It
// is an inference from an observation, never a measurement of the ovulation
// itself, and it makes no new claim about fertility beyond what follows from the
// day: past the window the status is not_fertile, and that is all.
//
// It exists because the substitution used to be applied one field at a time.
// PublishedOverviewStats replaced the DATE and left the window and
// CurrentFertility on the projection, while the dashboard hero, the /stats page
// and the calendar grid read the raw projected values — so one shift produced
// an ovulation day from the temperatures beside a window and a fertility status
// from the model, several days apart, on surfaces an owner reads side by side.
// Every reader of a confirmed day now routes through this one function: a
// second site applying half the substitution is how the two halves came to
// disagree in the first place.
//
// The window keeps the projection's own arithmetic (PredictCycleWindow): six
// days, [day-5, day], clamped to the recorded cycle start, and stepped from a
// dateOnly anchor rather than from the caller's request zone — a step taken
// from a request-zone midnight resolves a skipped local midnight backward into
// the previous day.
//
// The clamp cannot fire today, and is kept as the projection's invariant rather
// than deleted as dead code: the shared "3-over-6" detector (cycle_signals.go)
// reads only recorded days on or after the cycle start, so its earliest
// possible confirmed day is cycle day 6 — a full six-value coverline window
// plus the first elevated day — and day-5 of cycle day 6 lands exactly ON the
// start. Deleting it would move the guarantee that a published window never
// precedes the recorded start into another file's series bound, where a later
// change to that bound removes it silently. TestConfirmedShiftAtTheEarliest
// CycleDayNeverCrossesThePeriodStart pins the boundary the argument rests on.
//
// The medical gate is NOT re-stated here: ConfirmedCurrentCycleOvulation reads
// FertilityProjectionSuppressed for every surface, so a cycle whose window is
// withheld today confirms nothing here either and keeps its silence. Suppression
// is the floor, and a confirmed observation must never become a route around
// one.
//
// OvulationImpossible is cleared with the substitution rather than left behind:
// it is the projection's claim that the account's median cycle leaves no room
// for an ovulation, and a shift the owner recorded is the observation that
// answers it. Left true it would also silence ResolveFertilityStatus, which
// reads it first — a window and a date published beside a status of "unknown"
// and an impossibility claim about the day just named.
func ResolveConfirmedCycleStats(user *models.User, logs []models.DailyLog, stats CycleStats, today time.Time, location *time.Location) (CycleStats, bool) {
	confirmedDay, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, location)
	if !ok {
		return stats, false
	}

	if location == nil {
		location = time.UTC
	}
	ovulationDayUTC := dateOnly(confirmedDay)
	fertilityStartUTC := ovulationDayUTC.AddDate(0, 0, -5)
	if periodStart := dateOnly(stats.LastPeriodStart); !stats.LastPeriodStart.IsZero() && fertilityStartUTC.Before(periodStart) {
		// codecov:ignore -- defensive invariant: the detector's series starts at
		// LastPeriodStart, so its earliest confirmed day is cycle day 6 and day-5
		// lands ON the start, never before it (see the comment above).
		fertilityStartUTC = periodStart
	}

	// The arithmetic above stays on dateOnly (UTC midnight) — see the comment
	// on the window's arithmetic above. The PUBLISHED fields are rebuilt at
	// location midnight before leaving this function: every caller computes
	// `today` via DateAtLocation, which is a location midnight, while
	// betweenInclusive and ResolveFertilityStatus below compare instants —
	// so a UTC-midnight window compared against a location-midnight today
	// disagrees by the zone's own offset (day one of the window read as
	// not_fertile in UTC+3, the ovulation day itself read as fertile in
	// UTC-5). CalendarDay keeps the calendar date exactly as computed above
	// and only moves it onto the axis `today` is already on.
	stats.OvulationDate = CalendarDay(ovulationDayUTC, location)
	stats.OvulationImpossible = false
	stats.FertilityWindowStart = CalendarDay(fertilityStartUTC, location)
	stats.FertilityWindowEnd = CalendarDay(ovulationDayUTC, location)
	// Phase and fertility are two orthogonal axes (#416), but both are
	// geometric and both are obliged to describe the date just published beside
	// them — read off the ALREADY updated fields, so neither disagrees with the
	// day a confirmed shift just named.
	stats.CurrentPhase = DetectCurrentPhase(stats, logs, today, location)
	stats.CurrentFertility = ResolveFertilityStatus(stats, today)
	return stats, true
}
