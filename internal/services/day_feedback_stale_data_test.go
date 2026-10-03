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

		// The window is behind a stale cycle, so a save through the handler's path is
		// a backfill and neutral for that reason alone. The verdict is held against the
		// policy itself, with the saved day also standing as today: the stale copy
		// must answer neutral, and the same copy with only the staleness cleared must
		// answer fertile, or the check would be a no-op behind the backfill rule.
		today := DateAtLocation(localNoon(stale, location), location)
		staleLogs := FilterLogsToStatsHistory(logs, today, location)
		stats := BuildCycleStatsFromLogs(user, staleLogs, localNoon(stale, location), location)
		_, published, suppression := ConfirmedAndPublishedStats(user, logs, stats, today, location)
		saved := DateAtLocation(savedDay, location)
		if !published.CycleDataStale {
			t.Fatal("fixture: the published stats of cycle day 30 must carry the out-of-date verdict")
		}
		if got := resolveDaySaveMessageKey(user, saved, saved, published, suppression); got != daySaveMessageNeutral {
			t.Fatalf("a same-day save inside the window on a stale cycle resolves to %q, want the neutral message", got)
		}
		published.CycleDataStale = false
		if got := resolveDaySaveMessageKey(user, saved, saved, published, suppression); got != daySaveMessageFertile {
			t.Fatalf("with the staleness cleared the same save resolves to %q, want the fertile message", got)
		}
	})
}

// The overdue tier of the day-save message. Once the running cycle is more than a
// week past its own length the fertility half of the verdict is withheld
// (PredictionSuppression.FertilitySuppressed), and a save must answer the neutral
// message however the window itself reads. The history is the one above: reference
// length 28, so cycle day 40 (2026-05-04) is overdue and the stale test's day 30 is
// not.
//
// The end-to-end half proves the fixture really is overdue and neutral. The policy
// half isolates the verdict: the window and the staleness come from the FRESH
// published copy, where the same same-day save is fertile, and only the verdict
// is the overdue one — so the neutral answer can only come from the overdue tier.
func TestDayFeedbackIsNeutralInsideTheWindowOnceTheCycleIsOverdue(t *testing.T) {
	user := dayFeedbackParityUser(62)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")
	savedDay := mustParseDay(t, "2026-04-05") // cycle day 11, inside the window

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		freshToday := DateAtLocation(localNoon(savedDay, location), location)
		freshStats := BuildCycleStatsFromLogs(user, FilterLogsToStatsHistory(logs, freshToday, location), localNoon(savedDay, location), location)
		_, fresh, freshVerdict := ConfirmedAndPublishedStats(user, logs, freshStats, freshToday, location)

		overdueDay := mustParseDay(t, "2026-05-04") // cycle day 40
		overdueToday := DateAtLocation(localNoon(overdueDay, location), location)
		overdueStats := BuildCycleStatsFromLogs(user, FilterLogsToStatsHistory(logs, overdueToday, location), localNoon(overdueDay, location), location)
		_, _, overdueVerdict := ConfirmedAndPublishedStats(user, logs, overdueStats, overdueToday, location)

		if !overdueVerdict.FertilitySuppressed || !PredictionsSuppressed(user, overdueStats) {
			t.Fatal("fixture: cycle day 40 must be overdue and suppress the fertility half")
		}
		if freshVerdict.FertilitySuppressed || fresh.CycleDataStale {
			t.Fatal("fixture: the fresh copy must publish an unsuppressed, current window")
		}

		if got := dayFeedbackKeyOn(t, user, logs, location, overdueDay, overdueDay); got != daySaveMessageNeutral {
			t.Fatalf("a save on an overdue cycle resolves to %q, want the neutral message", got)
		}

		saved := DateAtLocation(savedDay, location)
		if got := resolveDaySaveMessageKey(user, saved, saved, fresh, freshVerdict); got != daySaveMessageFertile {
			t.Fatalf("control: the unsuppressed same-day save resolves to %q, want the fertile message", got)
		}
		if got := resolveDaySaveMessageKey(user, saved, saved, fresh, overdueVerdict); got != daySaveMessageNeutral {
			t.Fatalf("the overdue verdict over an in-window same-day save resolves to %q, want the neutral message", got)
		}
	})
}
