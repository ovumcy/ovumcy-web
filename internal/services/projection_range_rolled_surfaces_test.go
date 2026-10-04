package services

import (
	"testing"
	"time"
)

// The next-period start window on the calendar grid and the dashboard hero is
// the window the dashboard header prints. Once the median day has passed (and
// the cycle is not yet overdue) the header's projection rolls to the cycle
// after it; the grid follows it there, and the hero — a ribbon of the cycle
// that opened at the last recorded start — draws no window rather than the
// following cycle's or a stale one.
func TestStartWindowOnGridAndHeroIsTheHeadersWindow(t *testing.T) {
	cases := []struct {
		name      string
		irregular bool
		offsets   []int
		dayOffset int
		rolled    bool
		heroDraws bool
	}{
		// Cycles of 26, 28, 28 and 34 days: median 28, average 29, a StdDev span.
		{name: "regular before the median", offsets: []int{0, 26, 54, 82, 116}, dayOffset: 116 + 19, heroDraws: true},
		{name: "regular after the median", offsets: []int{0, 26, 54, 82, 116}, dayOffset: 116 + 28, rolled: true},
		// Cycles of 26, 28 and 36 days: median 28, average 30. The irregular
		// window is placed from the last recorded start, so the roll leaves it
		// on this cycle and the hero keeps it.
		{name: "irregular after the median", irregular: true, offsets: []int{0, 26, 54, 90}, dayOffset: 90 + 28, rolled: true, heroDraws: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := irregularVerdictLogs(tc.offsets...)
			now := irregularVerdictBase.AddDate(0, 0, tc.dayOffset)
			today := DateAtLocation(now, time.UTC)
			user := irregularVerdictUser(tc.irregular)
			stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
			if PredictionsSuppressed(user, stats) {
				t.Fatalf("fixture: predictions suppressed on cycle day %d", stats.CurrentCycleDay)
			}
			prediction := DashboardUpcomingPredictions(stats, user, today, DashboardProjectionCycleLength(user, stats))
			if rolled := CalendarDayKey(prediction.NextPeriodStart) != CalendarDayKey(stats.NextPeriodStart); rolled != tc.rolled {
				t.Fatalf("fixture: header projection %s against stats %s, rolled = %t, want %t",
					CalendarDayKey(prediction.NextPeriodStart), CalendarDayKey(stats.NextPeriodStart), rolled, tc.rolled)
			}

			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
			if !cycleContext.DisplayNextPeriodUseRange {
				t.Fatal("fixture: the dashboard header shows no start window")
			}
			windowStart, windowEnd := cycleContext.DisplayNextPeriodRangeStart, cycleContext.DisplayNextPeriodRangeEnd

			maps := buildCalendarPredictionMaps(user, logs, stats, today.AddDate(0, 3, 0), now, time.UTC)
			for day := windowStart; !day.After(windowEnd); day = day.AddDate(0, 0, 1) {
				if !maps.predictedStartRange[CalendarDayKey(day)] {
					t.Errorf("calendar grid: start window misses %s of the header's %s..%s", CalendarDayKey(day), CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
				}
			}
			if want := CalendarDaysBetween(windowStart, windowEnd) + 1; len(maps.predictedStartRange) != want {
				t.Errorf("calendar grid: start window shades %d day(s), want the header's %d", len(maps.predictedStartRange), want)
			}

			hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})
			if !hero.Visible {
				t.Fatalf("fixture: hero hidden on cycle day %d", stats.CurrentCycleDay)
			}
			cycleStart := CalendarDay(stats.LastPeriodStart, time.UTC)
			for _, day := range hero.Days {
				date := AddCalendarDays(cycleStart, day.Day-1, time.UTC)
				want := tc.heroDraws && irregularVerdictWithin(date, windowStart, windowEnd)
				if day.IsStartWindow != want {
					t.Errorf("hero: cycle day %d (%s) start window = %t, want %t (header window %s..%s)",
						day.Day, CalendarDayKey(date), day.IsStartWindow, want, CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
				}
			}
			if !tc.heroDraws && hero.AxisDays != hero.CycleLength {
				t.Errorf("hero: axis %d days for a %d-day cycle — stretched toward a window it does not draw", hero.AxisDays, hero.CycleLength)
			}
		})
	}
}
