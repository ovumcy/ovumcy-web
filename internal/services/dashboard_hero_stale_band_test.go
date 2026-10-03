package services

import (
	"testing"
	"time"
)

// TestDashboardCycleHeroIsHiddenThroughTheOutOfDateAndOverdueBands drives the hero
// from real stats and context. Three 28-day cycles and a running one from
// 2026-03-26 give a reference length of 28: cycle day 20 is current (the control:
// the ribbon draws), day 30 is out of date, day 40 is overdue. Both of the later
// days are also past the reference length, so on their own they pin only that the
// hero is hidden there, whichever conjunct of canRenderDashboardCycleHero does it;
// the next two tests isolate the individual clauses.
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

// TestDashboardCycleHeroIsHiddenWhenOverdueInsideAnInflatedReference isolates the
// paused-estimate clause. A 99-day gap beside three 28-day cycles inflates the
// average-first reference the hero draws against to 53 while the median — and so
// the length the overdue gate measures — stays 28. Cycle day 40 is therefore
// INSIDE the reference (the day bound does not hide it) and not out of date, yet
// overdue: only the paused estimate keeps the ribbon off the page.
func TestDashboardCycleHeroIsHiddenWhenOverdueInsideAnInflatedReference(t *testing.T) {
	user := dayFeedbackParityUser(67)
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26", "2026-07-03")
	now := localNoon(mustParseDay(t, "2026-08-11"), time.UTC) // cycle day 40
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)

	if !DashboardCycleOverdue(user, stats) {
		t.Fatal("fixture: cycle day 40 must be overdue against the 28-day median")
	}
	if reference := DashboardCycleReferenceLength(user, stats); stats.CurrentCycleDay > reference || stats.CurrentCycleDay != 40 {
		t.Fatalf("fixture: cycle day %d must sit inside the inflated reference %d", stats.CurrentCycleDay, reference)
	}
	if cycleContext.CycleDataStale {
		t.Fatal("fixture: the inflated reference must leave the cycle not out of date")
	}
	if hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC}); hero.Visible {
		t.Fatal("an overdue cycle inside an inflated reference must not draw the hero ribbon")
	}
}

// TestCanRenderDashboardCycleHeroRefusesEachSuppressionSignalOnItsOwn hands the
// predicate a context carrying exactly one signal, against stats the day bound
// accepts, so each clause is the only thing standing between the context and a
// drawn ribbon. The empty context is the control.
func TestCanRenderDashboardCycleHeroRefusesEachSuppressionSignalOnItsOwn(t *testing.T) {
	stats := CycleStats{CurrentCycleDay: 10}

	if !canRenderDashboardCycleHero(28, stats, DashboardCycleContext{}) {
		t.Fatal("control: an empty context within the cycle length must render")
	}
	for name, cycleContext := range map[string]DashboardCycleContext{
		"out of date":     {CycleDataStale: true},
		"estimate paused": {NextPeriodEstimatePaused: true},
		"prediction off":  {PredictionDisabled: true},
	} {
		if canRenderDashboardCycleHero(28, stats, cycleContext) {
			t.Fatalf("%s alone must withhold the hero", name)
		}
	}
}
