package services

import (
	"testing"
	"time"
)

// The next-period start window on the calendar grid and the dashboard hero is
// the window the dashboard header prints, and it stays the running cycle's
// until the overdue gate: once the median day has passed, the period is late,
// not a cycle away, so the window still holds today rather than opening a
// month out.
func TestStartWindowOnGridAndHeroIsTheHeadersWindow(t *testing.T) {
	cases := []struct {
		name       string
		irregular  bool
		offsets    []int
		dayOffset  int
		pastMedian bool
	}{
		// Cycles of 26, 28, 28 and 34 days: median 28, average 29, a StdDev span.
		{name: "regular before the median", offsets: []int{0, 26, 54, 82, 116}, dayOffset: 116 + 19},
		{name: "regular after the median", offsets: []int{0, 26, 54, 82, 116}, dayOffset: 116 + 28, pastMedian: true},
		// Cycles of 26, 28 and 36 days: median 28, average 30. The irregular
		// window is placed from the last recorded start.
		{name: "irregular after the median", irregular: true, offsets: []int{0, 26, 54, 90}, dayOffset: 90 + 28, pastMedian: true},
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
			if got, want := CalendarDayKey(prediction.NextPeriodStart), CalendarDayKey(stats.NextPeriodStart); got != want {
				t.Fatalf("header projection %s, want the running cycle's %s", got, want)
			}

			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
			if !cycleContext.DisplayNextPeriodUseRange {
				t.Fatal("fixture: the dashboard header shows no start window")
			}
			windowStart, windowEnd := cycleContext.DisplayNextPeriodRangeStart, cycleContext.DisplayNextPeriodRangeEnd
			if tc.pastMedian && !irregularVerdictWithin(today, windowStart, windowEnd) {
				t.Errorf("header window %s..%s does not hold today %s on cycle day %d", CalendarDayKey(windowStart), CalendarDayKey(windowEnd), CalendarDayKey(today), stats.CurrentCycleDay)
			}

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
			drawn := 0
			for _, day := range hero.Days {
				date := AddCalendarDays(cycleStart, day.Day-1, time.UTC)
				want := irregularVerdictWithin(date, windowStart, windowEnd)
				if day.IsStartWindow != want {
					t.Errorf("hero: cycle day %d (%s) start window = %t, want %t (header window %s..%s)",
						day.Day, CalendarDayKey(date), day.IsStartWindow, want, CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
				}
				if day.IsStartWindow {
					drawn++
				}
			}
			if drawn == 0 {
				t.Errorf("hero draws no start window; the header shows %s..%s", CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
			}
		})
	}
}

// The webhook and the .ics feed send the header's window on the same day the
// header holds it: cycles of 26, 28, 28 and 34 days, read on cycle day 29. The
// period is due now, so neither names the cycle after it.
func TestLatePeriodWindowLeavesOnWebhookAndFeedAsTheHeadersWindow(t *testing.T) {
	logs := irregularVerdictLogs(0, 26, 54, 82, 116)
	now := irregularVerdictBase.AddDate(0, 0, 116+28)
	today := DateAtLocation(now, time.UTC)
	user := irregularVerdictUser(false)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	windowStart, windowEnd := cycleContext.DisplayNextPeriodRangeStart, cycleContext.DisplayNextPeriodRangeEnd
	if !cycleContext.DisplayNextPeriodUseRange || !irregularVerdictWithin(today, windowStart, windowEnd) {
		t.Fatalf("fixture: header window %s..%s (shown %t) does not hold today %s",
			CalendarDayKey(windowStart), CalendarDayKey(windowEnd), cycleContext.DisplayNextPeriodUseRange, CalendarDayKey(today))
	}

	t.Run("webhook announces the open window once", func(t *testing.T) {
		reminders := DecideDueReminders(user, enabledWebhookSettings(3), logs, now, time.UTC)
		period, ok := findDueReminder(reminders, DueReminderTypePeriod)
		if !ok {
			t.Fatalf("expected the open window announced to an owner never reminded of it, got %#v", reminders)
		}
		if CalendarDayKey(period.EventDate) != CalendarDayKey(windowStart) || CalendarDayKey(period.EventDateEnd) != CalendarDayKey(windowEnd) {
			t.Fatalf("period reminder %s..%s, want the header's %s..%s",
				CalendarDayKey(period.EventDate), CalendarDayKey(period.EventDateEnd), CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
		}

		settings := enabledWebhookSettings(3)
		sent := windowStart
		settings.PeriodWatermark = &sent
		reminders, watermarked := decideDueReminders(user, settings, logs, now, time.UTC)
		if _, ok := findDueReminder(reminders, DueReminderTypePeriod); ok {
			t.Fatalf("expected no second reminder for a window already announced, got %#v", reminders)
		}
		if watermarked != 1 {
			t.Fatalf("watermark-suppressed = %d, want 1 (the announced window)", watermarked)
		}
	})

	t.Run("feed carries the window, not the cycle after it", func(t *testing.T) {
		events := calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: time.UTC})
		var windows, singles []calendarFeedEvent
		for _, event := range events {
			switch event.kind {
			case calendarFeedKindPeriodWindow:
				windows = append(windows, event)
			case calendarFeedKindPeriod:
				singles = append(singles, event)
			}
		}
		if len(windows) != 1 || CalendarDayKey(windows[0].date) != CalendarDayKey(windowStart) || CalendarDayKey(windows[0].end) != CalendarDayKey(windowEnd) {
			t.Fatalf("feed period windows %#v, want exactly the header's %s..%s", windows, CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
		}
		for _, single := range singles {
			if CalendarDaysBetween(windowEnd, single.date) <= 0 {
				t.Errorf("feed period %s falls in or before the window %s..%s that replaces it", CalendarDayKey(single.date), CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
			}
		}
		if len(singles) != calendarFeedProjectionCycles {
			t.Errorf("feed carries %d chained period(s) after the window, want %d — a late period must not shorten the horizon", len(singles), calendarFeedProjectionCycles)
		}
	})
}
