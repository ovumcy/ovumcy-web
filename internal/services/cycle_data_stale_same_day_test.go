package services

import (
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// staleBandSignals is every place the out-of-date band shows up for one
// account on one day: the pages' flag, the published flag, the reason the
// suppression verdict publishes, the withheld fertility half and the late
// notice that explains it.
type staleBandSignals struct {
	pageFlag      bool
	publishedFlag bool
	reason        bool
	fertility     bool
	notice        bool
}

func readStaleBandSignals(user *models.User, logs []models.DailyLog, stats CycleStats, today time.Time, location *time.Location) staleBandSignals {
	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, location)
	return staleBandSignals{
		pageFlag:      cycleContext.CycleDataStale,
		publishedFlag: published.CycleDataStale,
		reason:        slices.Contains(suppression.Reasons, SuppressionReasonCycleDataStale),
		fertility:     suppression.FertilitySuppressed,
		notice:        cycleContext.LateCycle.Visible,
	}
}

func (signals staleBandSignals) agreeOn(want bool) bool {
	return signals.pageFlag == want && signals.publishedFlag == want && signals.reason == want &&
		signals.fertility == want && signals.notice == want
}

// TestStaleBandFlagReasonAndNoticeTurnOnTheSameDay sweeps cycle days L-1 to
// one past the overdue gate on three 28-day cycles. The published
// `cycle_data_stale` flag, the `cycle_data_stale` suppression reason, the
// withheld fertility half and the late-cycle notice must all be off through
// day L and all on from day L+1, overdue included: a day on which one of them
// moved alone is a payload that names a withhold it did not make, or makes one
// it does not explain.
func TestStaleBandFlagReasonAndNoticeTurnOnTheSameDay(t *testing.T) {
	user, logs, _ := staleBandFixture(t)
	runningStart := mustParseDay(t, "2026-05-31")
	const referenceLength = 28
	const overdueDay = referenceLength + 7 + 1

	for cycleDay := referenceLength - 1; cycleDay <= overdueDay+1; cycleDay++ {
		now := localNoon(runningStart.AddDate(0, 0, cycleDay-1), time.UTC)
		today := DateAtLocation(now, time.UTC)
		stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
		if stats.CurrentCycleDay != cycleDay || DashboardCycleReferenceLength(user, stats) != referenceLength {
			t.Fatalf("fixture: cycle day %d against reference %d, want %d against %d",
				stats.CurrentCycleDay, DashboardCycleReferenceLength(user, stats), cycleDay, referenceLength)
		}
		if got := DashboardCycleOverdue(user, stats); got != (cycleDay >= overdueDay) {
			t.Fatalf("fixture: cycle day %d overdue=%v", cycleDay, got)
		}

		want := cycleDay > referenceLength
		if signals := readStaleBandSignals(user, logs, stats, today, time.UTC); !signals.agreeOn(want) {
			t.Errorf("cycle day %d: %+v, want every signal %v", cycleDay, signals, want)
		}
	}
}

// TestStaleBandSignalsAgreeWhereTheAnchorAndTheStatsPart covers the inputs on
// which an anchor-and-today measurement and the stats' own cycle day answer
// differently. The flag is the fertility signal itself, so it follows the cycle
// day the stats carry and never raises `cycle_data_stale` beside a fertility
// half the same payload still publishes.
func TestStaleBandSignalsAgreeWhereTheAnchorAndTheStatsPart(t *testing.T) {
	today := time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC)

	t.Run("stats with no start beside a stored start forty days back", func(t *testing.T) {
		storedStart := today.AddDate(0, 0, -39)
		user := &models.User{Role: models.RoleOwner, CycleLength: 28, LastPeriodStart: &storedStart}
		stats := CycleStats{AverageCycleLength: 28, MedianCycleLength: 28, CompletedCycleCount: 3}
		if DashboardCycleStaleAnchor(user, stats, today, time.UTC).IsZero() ||
			!DashboardCycleDataLooksStale(DashboardCycleStaleAnchor(user, stats, today, time.UTC), today, 28) {
			t.Fatal("fixture: the stored start must read as out of date when measured from the anchor")
		}

		if signals := readStaleBandSignals(user, nil, stats, today, time.UTC); !signals.agreeOn(false) {
			t.Fatalf("%+v: the stats carry no cycle day, so nothing is withheld and nothing may be flagged", signals)
		}
	})

	t.Run("stats built on day L read against the next day", func(t *testing.T) {
		user, logs, _ := staleBandFixture(t)
		builtOn := localNoon(mustParseDay(t, "2026-06-27"), time.UTC)
		stats := BuildCycleStatsFromLogs(user, logs, builtOn, time.UTC)
		readOn := mustParseDay(t, "2026-06-28")
		if stats.CurrentCycleDay != 28 ||
			!DashboardCycleDataLooksStale(DashboardCycleStaleAnchor(user, stats, readOn, time.UTC), readOn,DashboardCycleReferenceLength(user, stats)) {
			t.Fatalf("fixture: stats on cycle day %d must read as out of date only when measured against the later today", stats.CurrentCycleDay)
		}

		if signals := readStaleBandSignals(user, logs, stats, readOn, time.UTC); !signals.agreeOn(false) {
			t.Fatalf("%+v: the stats say cycle day 28, so the flag may not run a day ahead of the reason", signals)
		}
	})
}
