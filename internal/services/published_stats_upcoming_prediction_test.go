package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestPublishedOverviewFollowsTheUpcomingPredictionThePagesRender pins the JSON
// overview to the projection the dashboard header and the calendar grid render
// once the running cycle's ovulation is behind today. A trying-to-conceive owner
// with three completed 28-day cycles (period starts 06-15, 07-13, 08-10, 09-07):
// the running cycle's ovulation is 09-20, and every page rolls it a cycle
// forward to 10-18 with the window 10-13..10-18, while the overview went on
// publishing 09-20, flagged exact. Cycle day 22 is the same divergence before the
// cycle has reached its own length; cycle day 29 is the reported case, where the
// next period that closes the running cycle is today.
func TestPublishedOverviewFollowsTheUpcomingPredictionThePagesRender(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		today           string
		wantNextPeriod  string
		wantOvulation   string
		wantWindowStart string
	}{
		{name: "cycle day 22", today: "2026-09-28", wantNextPeriod: "2026-10-05", wantOvulation: "2026-10-18", wantWindowStart: "2026-10-13"},
		{name: "cycle day 29", today: "2026-10-05", wantNextPeriod: "2026-10-05", wantOvulation: "2026-10-18", wantWindowStart: "2026-10-13"},
	} {
		for _, zoneName := range []string{"UTC", "America/New_York", "Pacific/Auckland"} {
			t.Run(testCase.name+"/"+zoneName, func(t *testing.T) {
				location := calendarDayComparisonZone(t, zoneName)
				user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying}
				logs := cycleStartLogs(t, "2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07")
				now := localNoon(mustParseDay(t, testCase.today), location)
				today := DateAtLocation(now, location)
				stats := BuildCycleStatsFromLogs(user, logs, now, location)
				if got := CalendarDayKey(stats.OvulationDate); got != "2026-09-20" {
					t.Fatalf("fixture: the running cycle's ovulation = %s, want 2026-09-20", got)
				}

				published, suppression, confirmed := PublishedOverviewStats(user, logs, stats, today, location)
				if suppression.PredictionsSuppressed || suppression.FertilitySuppressed || confirmed {
					t.Fatalf("fixture: suppression %+v confirmed %v, want a published, projected ovulation", suppression, confirmed)
				}

				if got := CalendarDayKey(published.NextPeriodStart); got != testCase.wantNextPeriod {
					t.Errorf("next_period_start = %s, want %s", got, testCase.wantNextPeriod)
				}
				if got := CalendarDayKey(published.OvulationDate); got != testCase.wantOvulation {
					t.Errorf("ovulation_date = %s, want %s — the passed running-cycle day instead of the one the pages roll to", got, testCase.wantOvulation)
				}
				if got := CalendarDayKey(published.FertilityWindowStart); got != testCase.wantWindowStart {
					t.Errorf("fertility_window_start = %s, want %s", got, testCase.wantWindowStart)
				}
				if got := CalendarDayKey(published.FertilityWindowEnd); got != testCase.wantOvulation {
					t.Errorf("fertility_window_end = %s, want %s", got, testCase.wantOvulation)
				}

				// The dashboard header names the same two days.
				dashboard := BuildDashboardCycleContext(user, logs, stats, today, location)
				if got, want := CalendarDayKey(published.OvulationDate), CalendarDayKey(dashboard.DisplayOvulationDate); got != want {
					t.Errorf("ovulation_date = %s, dashboard header = %s", got, want)
				}
				if got, want := CalendarDayKey(published.NextPeriodStart), CalendarDayKey(dashboard.DisplayNextPeriodStart); got != want {
					t.Errorf("next_period_start = %s, dashboard header = %s", got, want)
				}
				if published.OvulationExact != dashboard.DisplayOvulationExact {
					t.Errorf("ovulation_exact = %v, dashboard = %v", published.OvulationExact, dashboard.DisplayOvulationExact)
				}

				// The calendar grid marks the published day and shades exactly the
				// published window around it.
				maps := buildCalendarPredictionMaps(user, logs, stats, AddCalendarDays(today, 60, location), now, location)
				if !maps.ovulation[CalendarDayKey(published.OvulationDate)] {
					t.Errorf("the calendar marks no ovulation on the published %s", CalendarDayKey(published.OvulationDate))
				}
				windowStart := CalendarDay(published.FertilityWindowStart, location)
				windowEnd := CalendarDay(published.FertilityWindowEnd, location)
				for day := AddCalendarDays(windowStart, -1, location); !day.After(AddCalendarDays(windowEnd, 1, location)); day = AddCalendarDays(day, 1, location) {
					key := CalendarDayKey(day)
					shaded := maps.fertilityEdge[key] || maps.fertilityPeak[key]
					inside := !day.Before(windowStart) && !day.After(windowEnd)
					if shaded != inside {
						t.Errorf("calendar shades %s fertile = %v, published window %s..%s says %v", key, shaded, CalendarDayKey(windowStart), CalendarDayKey(windowEnd), inside)
					}
				}
			})
		}
	}
}

// TestPublishedOverviewDropsAPassedOvulationWhoseRollFallsPastTheYear: on
// 9999-12-30 the running cycle's ovulation (9999-12-23) is behind today and the
// cycle it rolls into ovulates in year 10000, which no surface spells. The
// dashboard names no ovulation there, so the overview publishes neither the
// passed day nor a window for it.
func TestPublishedOverviewDropsAPassedOvulationWhoseRollFallsPastTheYear(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
	logs := cycleStartLogs(t, "9999-09-17", "9999-10-15", "9999-11-12", "9999-12-10")
	now := localNoon(mustParseDay(t, "9999-12-30"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if got := CalendarDayKey(stats.OvulationDate); got != "9999-12-23" {
		t.Fatalf("fixture: the running cycle's ovulation = %s, want 9999-12-23", got)
	}

	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if suppression.FertilitySuppressed {
		t.Fatalf("fixture: suppression = %+v, want the fertility half published", suppression)
	}
	if !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
		t.Fatalf("ovulation %s window %s..%s, want all absent — the dashboard names none",
			CalendarDayKey(published.OvulationDate), CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(published.FertilityWindowEnd))
	}
	if dashboard := BuildDashboardCycleContext(user, logs, stats, today, time.UTC); !dashboard.DisplayOvulationDate.IsZero() {
		t.Fatalf("fixture: the dashboard names ovulation %s", CalendarDayKey(dashboard.DisplayOvulationDate))
	}
}

// TestPublishedOverviewKeepsTheRunningCycleBeforeItsOvulationPasses is the
// control: while the running cycle's ovulation is still ahead the projection
// does not roll, and the overview publishes the stats' own window unchanged.
func TestPublishedOverviewKeepsTheRunningCycleBeforeItsOvulationPasses(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying}
	logs := cycleStartLogs(t, "2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07")
	now := localNoon(mustParseDay(t, "2026-09-18"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

	published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	for name, pair := range map[string][2]time.Time{
		"next_period_start":      {published.NextPeriodStart, stats.NextPeriodStart},
		"ovulation_date":         {published.OvulationDate, stats.OvulationDate},
		"fertility_window_start": {published.FertilityWindowStart, stats.FertilityWindowStart},
		"fertility_window_end":   {published.FertilityWindowEnd, stats.FertilityWindowEnd},
	} {
		if !sameDay(pair[0], pair[1]) {
			t.Errorf("%s = %s, want the running cycle's %s", name, CalendarDayKey(pair[0]), CalendarDayKey(pair[1]))
		}
	}
	if CalendarDayKey(published.OvulationDate) != "2026-09-20" {
		t.Fatalf("fixture: ovulation_date = %s, want 2026-09-20", CalendarDayKey(published.OvulationDate))
	}
}
