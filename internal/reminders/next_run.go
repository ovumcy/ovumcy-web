package reminders

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
// Granularity is hour-only: the target is always the top of the given hour, on a
// day that really has one (fireOnCalendarDay). The candidate is built for today;
// while that instant is not strictly in the future — the hour already passed, is
// exactly now, or the day itself does not exist in this zone — the candidate is
// rebuilt one calendar day later. The loop terminates because a calendar day
// carries the candidate forward by 24h minus an offset delta always smaller than
// a day.
//
// DST correctness comes from rebuilding the candidate on the TARGET day rather
// than adding 24h to a previous fire:
//
//   - Spring-forward (a local hour is skipped, e.g. 02:00→03:00): the wall clock
//     the schedule asks for may not exist, and time.Date's normalization DIRECTION
//     is not guaranteed. Where the skipped hour is midnight (America/Santiago
//     2025-09-07, America/Havana 2026-03-08, both west of UTC) it normalizes
//     BACKWARD, to 23:00 of the PREVIOUS local day. Left alone with hour 0, that
//     candidate sits before now: the scheduler's delay goes negative, the timer
//     fires at once, and a full notify pass over every owner repeats until the
//     transition. fireOnCalendarDay keeps the pass on its intended day instead.
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
	day := now.In(location)

	candidate := fireOnCalendarDay(day, hour, location)
	for !candidate.After(now) {
		day = day.AddDate(0, 0, 1)
		candidate = fireOnCalendarDay(day, hour, location)
	}
	return candidate
}

// fireOnCalendarDay returns the instant on day's calendar date whose local clock
// reads hour:00:00 — or, when that wall clock does not exist because a DST jump
// swallowed it and time.Date normalized the result off the requested date, the
// first instant that date really has.
//
// Skipping such a date instead would drop a whole day of reminders for every
// owner, and nothing downstream compensates: a reminder due only on that day is
// decided against a lead-day window, and the already-sent watermark cannot help
// when no pass ran at all. Resolving to the day's first existing instant is the
// convention the rest of the tree already follows for a day with no midnight, so
// this defers to services.StartOfCalendarDay rather than re-deriving the rule —
// a second implementation of it is how this class of defect returns.
//
// Only hour 0 can reach that branch in practice (a later wall clock exists on
// every day these zones have), and a date that has no instant at all — a zone
// that skips a whole calendar day, e.g. Pacific/Apia 2011-12-30 — falls through
// with time.Date's own answer, which the caller's loop then steps past.
func fireOnCalendarDay(day time.Time, hour int, location *time.Location) time.Time {
	year, month, date := day.Date()

	candidate := time.Date(year, month, date, hour, 0, 0, 0, location)
	if y, m, d := candidate.Date(); y == year && m == month && d == date {
		return candidate
	}
	return services.StartOfCalendarDay(year, month, date, location)
}
