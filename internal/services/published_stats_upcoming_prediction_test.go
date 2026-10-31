package services

import (
	"slices"
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
// publishing 09-20, flagged exact. Cycle day 22 is that divergence before the
// cycle has reached its own length; the reported case, cycle day 29, is pinned
// by TestPublishedOverviewPublishesNoOvulationOnceTheCycleIsLate.
//
// The status is read against the rolled window, which does not cover today, and
// the phase stays the running cycle's "luteal", the dashboard's.
func TestPublishedOverviewFollowsTheUpcomingPredictionThePagesRender(t *testing.T) {
	for _, testCase := range []struct {
		name            string
		today           string
		wantNextPeriod  string
		wantOvulation   string
		wantWindowStart string
		wantFertility   string
		wantBasis       string
		wantPhase       string
	}{
		{name: "cycle day 22", today: "2026-09-28", wantNextPeriod: "2026-10-05", wantOvulation: "2026-10-18", wantWindowStart: "2026-10-13",
			wantFertility: FertilityStatusOutsideEstimatedWindow, wantBasis: FertilityBasisProjection, wantPhase: "luteal"},
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
				assertPublishedStatusAndPhase(t, published, testCase.wantFertility, testCase.wantBasis, testCase.wantPhase)

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

// TestPublishedOverviewPublishesNoOvulationOnceTheCycleIsLate: the reported
// case — the same owner on cycle day 29 (10-05), the day the next period was
// due. The running cycle is past its 28-day reference length, so the fertility
// half is withheld as out of date: the overview publishes neither the passed
// 09-20 nor the rolled 10-18, on the same verdict that withholds it from the
// dashboard. The next period stays published, the day the header names.
func TestPublishedOverviewPublishesNoOvulationOnceTheCycleIsLate(t *testing.T) {
	for _, zoneName := range []string{"UTC", "America/New_York", "Pacific/Auckland"} {
		t.Run(zoneName, func(t *testing.T) {
			location := calendarDayComparisonZone(t, zoneName)
			user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying}
			logs := cycleStartLogs(t, "2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07")
			now := localNoon(mustParseDay(t, "2026-10-05"), location)
			today := DateAtLocation(now, location)
			stats := BuildCycleStatsFromLogs(user, logs, now, location)

			published, suppression, confirmed := PublishedOverviewStats(user, logs, stats, today, location)
			if suppression.PredictionsSuppressed || !suppression.FertilitySuppressed || !slices.Contains(suppression.Reasons, SuppressionReasonCycleDataStale) || confirmed {
				t.Fatalf("fixture: suppression %+v confirmed %v, want the fertility half withheld as out of date", suppression, confirmed)
			}
			if !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
				t.Errorf("ovulation %s window %s..%s, want all absent",
					CalendarDayKey(published.OvulationDate), CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(published.FertilityWindowEnd))
			}
			if got := CalendarDayKey(published.NextPeriodStart); got != "2026-10-05" {
				t.Errorf("next_period_start = %s, want 2026-10-05", got)
			}
			assertPublishedStatusAndPhase(t, published, FertilityStatusUnknown, "", "unknown")

			dashboard := BuildDashboardCycleContext(user, logs, stats, today, location)
			if !dashboard.FertilitySuppressed {
				t.Errorf("the dashboard keeps the fertility half that the overview withholds")
			}
			if got, want := CalendarDayKey(published.NextPeriodStart), CalendarDayKey(dashboard.DisplayNextPeriodStart); got != want {
				t.Errorf("next_period_start = %s, dashboard header = %s", got, want)
			}
		})
	}
}

// TestPublishedOverviewWithholdsTheRolledWindowOnEveryOutOfDateDay drives the
// whole out-of-date band, from the day after the reference length to the last day
// before the overdue gate withholds the next period too, for two histories: one
// whose median and reference agree, and a right-skewed one whose reference sits
// above its median. On every one of those days the running cycle's ovulation is
// behind today, so a response that stopped withholding the fertility half would
// roll it a cycle forward and read the status and phase against the rolled window.
// The overview must publish none of that: no ovulation, no window, "unknown" for
// the status and the phase, and the next period the running cycle is waiting on.
//
// This is the end-to-end half of the stale arm in reconcileMovedWindow: the arm
// itself cannot be reached from here, because the out-of-date verdict and the
// fertility gate are one predicate and the roll returns before the reconcile
// whenever the gate holds. What this test catches is that predicate drifting
// apart, which would publish a rolled ovulation and window on out-of-date data.
func TestPublishedOverviewWithholdsTheRolledWindowOnEveryOutOfDateDay(t *testing.T) {
	for _, history := range []struct {
		name   string
		starts []string
		// The date the running cycle began; cycle day N is this plus N-1 days.
		runningStart string
	}{
		{name: "28-day history", starts: []string{"2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07"}, runningStart: "2026-09-07"},
		{name: "right-skewed history", starts: []string{"2026-05-04", "2026-05-31", "2026-06-28", "2026-07-26", "2026-08-23", "2026-09-28"}, runningStart: "2026-09-28"},
	} {
		t.Run(history.name, func(t *testing.T) {
			user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, UsageGoal: models.UsageGoalTrying}
			logs := cycleStartLogs(t, history.starts...)
			runningStart := mustParseDay(t, history.runningStart)

			exercised := 0
			for cycleDay := 1; cycleDay <= 60; cycleDay++ {
				now := localNoon(AddCalendarDays(runningStart, cycleDay-1, time.UTC), time.UTC)
				today := DateAtLocation(now, time.UTC)
				stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
				if !DashboardCyclePastReferenceLength(user, stats) || DashboardCycleOverdue(user, stats) {
					continue
				}
				exercised++
				if stats.CurrentCycleDay != cycleDay || stats.OvulationDate.IsZero() || !stats.OvulationDate.Before(today) {
					t.Fatalf("cycle day %d: fixture: stats day %d ovulation %s, want the running cycle's ovulation behind today",
						cycleDay, stats.CurrentCycleDay, CalendarDayKey(stats.OvulationDate))
				}

				published, suppression, confirmed := PublishedOverviewStats(user, logs, stats, today, time.UTC)
				if suppression.PredictionsSuppressed || !suppression.FertilitySuppressed || !slices.Contains(suppression.Reasons, SuppressionReasonCycleDataStale) || confirmed || !published.CycleDataStale {
					t.Errorf("cycle day %d: suppression %+v confirmed %v stale %v, want the fertility half withheld as out of date",
						cycleDay, suppression, confirmed, published.CycleDataStale)
				}
				if !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() || published.OvulationExact {
					t.Errorf("cycle day %d: ovulation %s window %s..%s exact %v, want all absent — a rolled window on out-of-date data",
						cycleDay, CalendarDayKey(published.OvulationDate), CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(published.FertilityWindowEnd), published.OvulationExact)
				}
				if published.NextPeriodStart.IsZero() {
					t.Errorf("cycle day %d: next_period_start absent, want the running cycle's own end", cycleDay)
				}
				assertPublishedStatusAndPhase(t, published, FertilityStatusUnknown, "", "unknown")
			}
			// Anti-vacuity: the band is not empty — a history whose band never opens
			// would leave the loop above measuring nothing.
			if exercised == 0 {
				t.Fatal("no cycle day of the fixture fell in the out-of-date band")
			}
		})
	}
}

// TestReconcileMovedWindowWithholdsStatusAndPhaseOnOutOfDateData pins the
// backstop no caller reaches today (out-of-date data suppresses the fertility
// half before any roll): handed a rolled window that covers today on
// out-of-date data, it still answers "unknown" for both, as PublishedStats does.
func TestReconcileMovedWindowWithholdsStatusAndPhaseOnOutOfDateData(t *testing.T) {
	today := mustParseDay(t, "2026-10-15")
	published := CycleStats{
		OvulationDate:        mustParseDay(t, "2026-10-18"),
		FertilityWindowStart: mustParseDay(t, "2026-10-13"),
		FertilityWindowEnd:   mustParseDay(t, "2026-10-18"),
		CurrentPhase:         "luteal",
		CycleDataStale:       true,
	}
	reconcileMovedWindow(&published, nil, today, time.UTC)
	assertPublishedStatusAndPhase(t, published, FertilityStatusUnknown, "", "unknown")
}

// TestPublishedOverviewDropsAPassedOvulationWhoseRollFallsPastTheYear: on
// 9999-12-30 the running cycle's ovulation (9999-12-23) is behind today and the
// cycle it rolls into ovulates in year 10000, which no surface spells. The
// dashboard names no ovulation there, so the overview publishes neither the
// passed day nor a window for it — and with the window gone, no status read
// against it and no phase that compares today with the day it withholds.
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
	// Cycle day 21, nothing logged: the running window would have answered
	// "outside_estimated_window" and "luteal".
	assertPublishedStatusAndPhase(t, published, FertilityStatusUnknown, "", "unknown")
}

// TestPublishedOverviewWithholdsAWindowRolledFromAMissedStart: completed cycles
// of 24, 24, 24, 40 and 40 days (median 24, mean about 30), the running cycle
// from 06-02, today 06-30, cycle day 29 — before the 30-day reference length,
// so the data is not out of date. The running ovulation (06-11) is behind
// today, and the projection rolls it to 07-05 with the window 06-30..07-05 —
// a cycle anchored on the median start, 06-26, which was due and never logged.
// The fertility half is withheld (cycle_past_projection_length, WEB-378): no
// ovulation, no window, an unknown status and phase, and no shaded day on the
// calendar; cycle_data_stale stays false.
func TestPublishedOverviewWithholdsAWindowRolledFromAMissedStart(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14, UsageGoal: models.UsageGoalTrying}
	logs := cycleStartLogs(t, "2026-01-01", "2026-01-25", "2026-02-18", "2026-03-14", "2026-04-23", "2026-06-02")
	now := localNoon(mustParseDay(t, "2026-06-30"), time.UTC)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if got := CalendarDayKey(stats.OvulationDate); got != "2026-06-11" || stats.CurrentFertility != FertilityStatusOutsideEstimatedWindow {
		t.Fatalf("fixture: the running cycle's ovulation = %s status %q, want 2026-06-11 outside its window", got, stats.CurrentFertility)
	}

	published, suppression, confirmed := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if suppression.PredictionsSuppressed || confirmed || published.CycleDataStale {
		t.Fatalf("fixture: suppression %+v confirmed %v stale %v, want the next period published and the data current", suppression, confirmed, published.CycleDataStale)
	}
	if !suppression.FertilitySuppressed || !slices.Contains(suppression.Reasons, SuppressionReasonCyclePastProjectionLength) {
		t.Fatalf("suppression %+v, want the fertility half withheld as cycle_past_projection_length", suppression)
	}
	if !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
		t.Fatalf("overview publishes ovulation %s and window %s..%s rolled from a missed start",
			CalendarDayKey(published.OvulationDate), CalendarDayKey(published.FertilityWindowStart), CalendarDayKey(published.FertilityWindowEnd))
	}
	assertPublishedStatusAndPhase(t, published, FertilityStatusUnknown, "", "unknown")
	maps := buildCalendarPredictionMaps(user, logs, stats, AddCalendarDays(today, 60, time.UTC), now, time.UTC)
	if len(maps.fertilityEdge)+len(maps.fertilityPeak)+len(maps.ovulation)+len(maps.preFertile) != 0 {
		t.Errorf("the calendar shades a fertility projection the overview withholds: edge %v peak %v ovulation %v pre-fertile %v",
			maps.fertilityEdge, maps.fertilityPeak, maps.ovulation, maps.preFertile)
	}
}

// assertPublishedStatusAndPhase checks the three fields read against the
// published window: the fertility status, the window it names as its basis, and
// the phase.
func assertPublishedStatusAndPhase(t *testing.T, published CycleStats, wantFertility string, wantBasis string, wantPhase string) {
	t.Helper()
	if published.CurrentFertility != wantFertility {
		t.Errorf("current_fertility = %q, want %q", published.CurrentFertility, wantFertility)
	}
	if published.FertilityBasis != wantBasis {
		t.Errorf("fertility_basis = %q, want %q", published.FertilityBasis, wantBasis)
	}
	if published.CurrentPhase != wantPhase {
		t.Errorf("current_phase = %q, want %q", published.CurrentPhase, wantPhase)
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
	// Today (cycle day 10) lies inside the running window the stats carried, so
	// "fertile" and "follicular" stood until the window went.
	if stats.CurrentFertility != FertilityStatusFertile {
		t.Fatalf("fixture: the running cycle's status = %q, want fertile", stats.CurrentFertility)
	}
	assertPublishedStatusAndPhase(t, published, FertilityStatusUnknown, "", "unknown")
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
