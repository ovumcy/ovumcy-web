package services

import (
	"testing"
	"time"
)

// TestDashboardCycleHeroIsHiddenThroughTheOutOfDateAndOverdueBands drives the hero
// from real stats rather than a hand-set context: three 28-day cycles and a
// running one from 2026-03-26, so the reference length is 28. Cycle day 20 is
// current (the control: the ribbon draws), day 30 is out of date but not
// overdue, day 40 is overdue. The ribbon is withheld from the first day the
// out-of-date verdict stands, not only once the overdue gate does.
func TestDashboardCycleHeroIsHiddenThroughTheOutOfDateAndOverdueBands(t *testing.T) {
	user := dayFeedbackParityUser(65)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26")

	cases := []struct {
		name    string
		day     string
		stale   bool
		overdue bool
		visible bool
	}{
		{"current cycle", "2026-04-14", false, false, true},
		{"out of date", "2026-04-24", true, false, false},
		{"overdue", "2026-05-04", true, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := localNoon(mustParseDay(t, tc.day), time.UTC)
			today := DateAtLocation(now, time.UTC)
			stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)

			if cycleContext.CycleDataStale != tc.stale || DashboardCycleOverdue(user, stats) != tc.overdue {
				t.Fatalf("fixture: stale=%v overdue=%v, want stale=%v overdue=%v", cycleContext.CycleDataStale, DashboardCycleOverdue(user, stats), tc.stale, tc.overdue)
			}
			hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})
			if hero.Visible != tc.visible {
				t.Fatalf("hero visible = %v at cycle day %d, want %v", hero.Visible, stats.CurrentCycleDay, tc.visible)
			}
		})
	}
}
