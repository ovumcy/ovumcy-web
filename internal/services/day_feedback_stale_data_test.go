package services

import (
	"testing"
	"time"
)

// Once the running cycle has outrun the account's reference length both owner
// pages print "unknown" for the phase and the fertility status, and the published
// stats withhold both (PublishedStats). The day-save message reads the same
// published copy, so a fertile line must not be spoken on those days either: it
// is a claim about a window the cycle has already passed.
//
// The history is three 28-day cycles, so the reference length is 28 and the
// default-luteal window of the running cycle (started 2026-03-26) is cycle days
// 9-14, 2026-04-03..04-08. Cycle day 30 is out of date and still short of the
// overdue gate, which withholds the window itself at day 36: the stretch where
// only the staleness verdict stands between the toast and a stale claim.
func TestDayFeedbackIsNeutralInsideTheWindowOnceTheCycleDataIsStale(t *testing.T) {
	user := dayFeedbackParityUser(61)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")
	savedDay := mustParseDay(t, "2026-04-05") // cycle day 11, inside the window

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		fresh := mustParseDay(t, "2026-04-05") // cycle day 11
		stale := mustParseDay(t, "2026-04-24") // cycle day 30

		// Fixtures: the same save is a fertile one while the data is current, and
		// the later "today" is stale without being overdue — otherwise the neutral
		// answer below could come from the overdue gate and not the staleness one.
		if got := dayFeedbackKeyOn(t, user, logs, location, fresh, savedDay); got != daySaveMessageFertile {
			t.Fatalf("fixture: the current cycle's day 11 resolves to %q, want the fertile message", got)
		}
		staleStats := BuildCycleStatsFromLogs(user, logs, localNoon(stale, location), location)
		staleContext := BuildDashboardCycleContext(user, logs, staleStats, DateAtLocation(localNoon(stale, location), location), location)
		if !staleContext.CycleDataStale {
			t.Fatal("fixture: cycle day 30 must be out of date")
		}
		if PredictionsSuppressed(user, staleStats) {
			t.Fatal("fixture: cycle day 30 must not be overdue, or the overdue gate would answer first")
		}

		if got := dayFeedbackKeyOn(t, user, logs, location, stale, savedDay); got != daySaveMessageNeutral {
			t.Fatalf("saving a day inside the window on a stale cycle resolves to %q, want the neutral message", got)
		}
	})
}
