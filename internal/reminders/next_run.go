package reminders

import (
	"github.com/ovumcy/ovumcy-web/internal/models"
	"time"
)

// markerKey is the app_state key under which the "ran today" local date is
// stored. It aliases the single models constant so the scheduler and any future
// tooling agree on the string.
const markerKey = models.AppStateKeyLastReminderRunDate

// nextRun returns the next instant, strictly after now, whose LOCAL clock reads
// hour:00:00 in location. It is the scheduler's whole schedule math, kept pure
// (no clock, no I/O) so it is exhaustively table-testable — including the two DST
// edges.
//
// Granularity is hour-only: the target is always the top of the given hour. The
// candidate is built with time.Date in location for today; if that instant is
// not strictly in the future (the hour already passed, or is exactly now), the
// candidate for the next calendar day is used.
//
// DST correctness comes from rebuilding the candidate with time.Date on the
// TARGET day rather than adding 24h to a previous fire:
//
//   - Spring-forward (a local hour is skipped, e.g. 02:00→03:00): time.Date for
//     the missing wall-clock hour normalizes forward to the equivalent real
//     instant, so the pass still fires that day near the intended time and the
//     schedule does not stall.
//   - Fall-back (a local hour repeats, e.g. 02:00 occurs twice): time.Date
//     resolves the target to one concrete instant; the pass fires once for that
//     local date. The once-per-local-day marker guarantees the repeated wall-
//     clock hour cannot trigger a second pass.
//
// Because each call recomputes from the actual current instant in location, a
// scheduler that recomputes every cycle stays pinned to the local hour across a
// transition instead of drifting by the offset delta a bare 24h ticker would
// accumulate.
func nextRun(now time.Time, hour int, location *time.Location) time.Time {
	if location == nil {
		location = time.UTC
	}
	local := now.In(location)

	candidate := time.Date(local.Year(), local.Month(), local.Day(), hour, 0, 0, 0, location)
	if !candidate.After(now) {
		tomorrow := local.AddDate(0, 0, 1)
		candidate = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), hour, 0, 0, 0, location)
	}
	return candidate
}
