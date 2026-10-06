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

// assertCalendarShadesExactlyThePublishedWindow reads the calendar grid built
// from the same stats and checks that it marks the published ovulation day and
// shades as fertile exactly the published window, one day either side included.
func assertCalendarShadesExactlyThePublishedWindow(t *testing.T, user *models.User, logs []models.DailyLog, stats CycleStats, published CycleStats, now time.Time, location *time.Location) {
	t.Helper()
	today := DateAtLocation(now, location)
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
}

// TestPublishedOverviewRollsTheWindowFromTheProjectionLengthNotTheStatsDay: the
// stats carry a running-cycle ovulation (09-20) placed by a 28-day length while
// the projection length is 30, so the day the pages roll to (10-22) is not a
// whole number of projection cycles from it. The window must still be the one
// the calendar shades around 10-22 — 10-17..10-22, ending on the published day —
// not a window shifted by the 32 days between the two ovulations, which ends on
// 10-24.
func TestPublishedOverviewRollsTheWindowFromTheProjectionLengthNotTheStatsDay(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying}
	logs := cycleStartLogs(t, "2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07")
	now := localNoon(mustParseDay(t, "2026-09-28"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	stats.MedianCycleLength, stats.AverageCycleLength = 30, 30
	stats.NextPeriodStart = projectedDay(AddCalendarDays(stats.LastPeriodStart, 30, time.UTC))
	if got := CalendarDayKey(stats.OvulationDate); got != "2026-09-20" {
		t.Fatalf("fixture: the stats' running-cycle ovulation = %s, want 2026-09-20", got)
	}

	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if suppression.FertilitySuppressed {
		t.Fatalf("fixture: suppression = %+v, want the fertility half published", suppression)
	}
	dashboard := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if got := CalendarDayKey(dashboard.DisplayOvulationDate); got != "2026-10-22" {
		t.Fatalf("fixture: the dashboard names ovulation %s, want 2026-10-22", got)
	}
	if got := CalendarDayKey(published.OvulationDate); got != "2026-10-22" {
		t.Errorf("ovulation_date = %s, want the dashboard's 2026-10-22", got)
	}
	if got, want := CalendarDayKey(published.FertilityWindowEnd), CalendarDayKey(published.OvulationDate); got != want {
		t.Errorf("fertility_window_end = %s, want the published ovulation_date %s", got, want)
	}
	if got := CalendarDayKey(published.FertilityWindowStart); got != "2026-10-17" {
		t.Errorf("fertility_window_start = %s, want 2026-10-17", got)
	}
	assertCalendarShadesExactlyThePublishedWindow(t, user, logs, stats, published, now, time.UTC)
}

// TestPublishedOverviewKeepsTheIrregularRangeWindowOnceTheMedianDayPasses: an
// irregular-mode owner with three completed cycles (26, 30, 28) is shown the
// running cycle's ovulation RANGE, 09-18..09-22, which the dashboard does not
// roll, and the calendar keeps shading that cycle's widened window 09-13..09-22.
// On 09-21 the median day (09-20) is behind today, and the overview must still
// publish the running cycle's widened window, not the rolled median window
// 10-13..10-18 no page names for this account.
func TestPublishedOverviewKeepsTheIrregularRangeWindowOnceTheMedianDayPasses(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying, IrregularCycle: true}
	logs := cycleStartLogs(t, "2026-06-15", "2026-07-11", "2026-08-10", "2026-09-07")
	now := localNoon(mustParseDay(t, "2026-09-21"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

	dashboard := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if !dashboard.DisplayOvulationUseRange || CalendarDayKey(dashboard.DisplayOvulationRangeStart) != "2026-09-18" || CalendarDayKey(dashboard.DisplayOvulationRangeEnd) != "2026-09-22" {
		t.Fatalf("fixture: dashboard ovulation range %v %s..%s, want 2026-09-18..2026-09-22",
			dashboard.DisplayOvulationUseRange, CalendarDayKey(dashboard.DisplayOvulationRangeStart), CalendarDayKey(dashboard.DisplayOvulationRangeEnd))
	}

	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if suppression.FertilitySuppressed {
		t.Fatalf("fixture: suppression = %+v, want the fertility half published", suppression)
	}
	if got := CalendarDayKey(published.OvulationDate); got != "2026-09-20" {
		t.Errorf("ovulation_date = %s, want the running cycle's median 2026-09-20", got)
	}
	if got := CalendarDayKey(published.FertilityWindowStart); got != "2026-09-13" {
		t.Errorf("fertility_window_start = %s, want the widened 2026-09-13", got)
	}
	if got, want := CalendarDayKey(published.FertilityWindowEnd), CalendarDayKey(dashboard.DisplayOvulationRangeEnd); got != want {
		t.Errorf("fertility_window_end = %s, want the dashboard range's last day %s", got, want)
	}
	assertCalendarShadesExactlyThePublishedWindow(t, user, logs, stats, published, now, time.UTC)
}

// TestPublishedOverviewClearsTheWindowWhereTheProjectionPlacesNoOvulation: a
// projection length of 14 leaves no ovulation the model can place, and the
// dashboard says so (DisplayOvulationImpossible, no day), while the stats still
// carry the window a 28-day length placed. The overview follows the header: no
// day, no window, ovulation_impossible set.
func TestPublishedOverviewClearsTheWindowWhereTheProjectionPlacesNoOvulation(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying}
	logs := cycleStartLogs(t, "2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07")
	now := localNoon(mustParseDay(t, "2026-09-16"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	stats.MedianCycleLength, stats.AverageCycleLength = 14, 14
	if stats.FertilityWindowStart.IsZero() || stats.OvulationDate.IsZero() {
		t.Fatalf("fixture: the stats carry no running-cycle window")
	}

	dashboard := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if !dashboard.DisplayOvulationImpossible || !dashboard.DisplayOvulationDate.IsZero() {
		t.Fatalf("fixture: dashboard impossible = %v day %s, want impossible and no day", dashboard.DisplayOvulationImpossible, CalendarDayKey(dashboard.DisplayOvulationDate))
	}

	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if suppression.FertilitySuppressed {
		t.Fatalf("fixture: suppression = %+v, want the fertility half published", suppression)
	}
	if !published.OvulationImpossible {
		t.Errorf("ovulation_impossible = false, want the dashboard's true")
	}
	if !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
		t.Errorf("ovulation %s window %s..%s, want all absent beside ovulation_impossible",
			CalendarDayKey(published.OvulationDate), CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(published.FertilityWindowEnd))
	}
}

// TestPublishedOverviewTakesTheNextPeriodFromTheUpcomingPrediction: the stats
// carry a next period (10-03) other than the one DashboardUpcomingPredictions
// names for the running cycle (10-05, its start plus the 28-day projection
// length). In a history built end to end the two coincide, so the fixture moves
// the stats' value by hand; the overview must publish the header's day.
func TestPublishedOverviewTakesTheNextPeriodFromTheUpcomingPrediction(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying}
	logs := cycleStartLogs(t, "2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07")
	now := localNoon(mustParseDay(t, "2026-09-28"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	stats.NextPeriodStart = projectedDay(AddCalendarDays(stats.LastPeriodStart, 26, time.UTC))

	dashboard := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if got := CalendarDayKey(dashboard.DisplayNextPeriodStart); got != "2026-10-05" {
		t.Fatalf("fixture: the dashboard names next period %s, want 2026-10-05", got)
	}

	published, suppression, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if suppression.PredictionsSuppressed {
		t.Fatalf("fixture: suppression = %+v, want the next period published", suppression)
	}
	if got := CalendarDayKey(published.NextPeriodStart); got != "2026-10-05" {
		t.Errorf("next_period_start = %s, want the dashboard's 2026-10-05, not the stats' 2026-10-03", got)
	}
}
