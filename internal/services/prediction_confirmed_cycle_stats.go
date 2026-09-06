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
// days, [day-5, day], stepped from a dateOnly anchor rather than from the
// caller's request zone — a step taken from a request-zone midnight resolves
// a skipped local midnight backward into the previous day.
//
// It is never clamped to the recorded cycle start: the shared "3-over-6"
// detector (cycle_signals.go) only ever reads recorded days on or after that
// start, so its earliest possible confirmed day is cycle day 6 (a full
// 6-value coverline window plus the first elevated day) — and day-5 of cycle
// day 6 lands exactly ON the cycle start, never before it. A clamp for an
// earlier day would be dead code for a day the detector cannot produce.
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

	ovulationDay := dateOnly(confirmedDay)
	fertilityStart := ovulationDay.AddDate(0, 0, -5)

	stats.OvulationDate = ovulationDay
	stats.OvulationImpossible = false
	stats.FertilityWindowStart = fertilityStart
	stats.FertilityWindowEnd = ovulationDay
	// Phase and fertility are two orthogonal axes (#416), but both are
	// geometric and both are obliged to describe the date just published beside
	// them — read off the ALREADY updated fields, so neither disagrees with the
	// day a confirmed shift just named.
	stats.CurrentPhase = detectCyclePhase(stats, logs, today)
	stats.CurrentFertility = ResolveFertilityStatus(stats, today)
	return stats, true
}
