package services

import (
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestIrregularRangeModeKeepsItsWindowButNotTheChain: irregular range mode is
// outside the fertility signal (its range is placed from the recorded start), yet
// its chained periods are rolled from the median start like any other, so the
// chain gate still fires on the day after the median.
func TestIrregularRangeModeKeepsItsWindowButNotTheChain(t *testing.T) {
	user := &models.User{IrregularCycle: true}
	stats := publishedStatsBase()
	stats.CurrentCycleDay = 29
	stats.AverageCycleLength = 30
	stats.MinCycleLength, stats.MaxCycleLength = 24, 36
	if !dashboardIrregularPredictionRangeEnabled(user, stats) {
		t.Fatal("fixture: irregular range mode must be on")
	}
	if DashboardCyclePastProjectionLength(user, stats) || FertilityProjectionSuppressed(user, stats) {
		t.Error("irregular range mode withholds its fertility range on the day after the median")
	}
	if !ProjectionChainSuppressed(user, stats) {
		t.Error("irregular range mode chains periods from the missed median start")
	}
}

// TestIrregularRangeModeNamesNoOvulationFromTheMissedStart pins WEB-385: on the
// days the chain gate fires in irregular range mode, the fertility signal does
// not (its range is placed from the recorded start), so the cycle opened by the
// missed median start still reached the feed and the grid as an ovulation and a
// fertile window while the hero named only the range. Every surface now names
// nothing from that cycle.
//
// History: cycles 24/28/28/34/36 (median 28, average 30, min 24, max 36), the
// running one from 2026-09-30; the missed median start is 2026-10-28 and the
// ovulation rolled from it 2026-11-10. Cycle day 28 is the control: the chain
// is drawn and the rolled ovulation is in it.
func TestIrregularRangeModeNamesNoOvulationFromTheMissedStart(t *testing.T) {
	user := dayFeedbackParityUser(385)
	user.IrregularCycle = true
	logs := cycleStartLogs(t, "2026-05-03", "2026-05-27", "2026-06-24", "2026-07-22", "2026-08-25", "2026-09-30")
	const rolled = "2026-11-10"

	cases := []struct {
		cycleDay int
		today    string
		chain    bool
	}{
		{cycleDay: 28, today: "2026-10-27"},
		{cycleDay: 29, today: "2026-10-28", chain: true},
		{cycleDay: 30, today: "2026-10-29", chain: true},
	}
	for _, tc := range cases {
		t.Run(tc.today, func(t *testing.T) {
			forEachParityZone(t, func(t *testing.T, location *time.Location) {
				now := localNoon(mustParseDay(t, tc.today), location)
				today := DateAtLocation(now, location)
				stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, now, location), today, location)
				if stats.CurrentCycleDay != tc.cycleDay || DashboardProjectionCycleLength(user, stats) != 28 || DashboardCycleReferenceLength(user, stats) != 30 ||
					stats.MinCycleLength != 24 || stats.MaxCycleLength != 36 || !dashboardIrregularPredictionRangeEnabled(user, stats) {
					t.Fatalf("fixture: cycle day %d, projection %d, reference %d, min %d, max %d, range mode %v",
						stats.CurrentCycleDay, DashboardProjectionCycleLength(user, stats), DashboardCycleReferenceLength(user, stats),
						stats.MinCycleLength, stats.MaxCycleLength, dashboardIrregularPredictionRangeEnabled(user, stats))
				}

				published, verdict, _ := PublishedOverviewStats(user, logs, stats, today, location)
				if verdict.ProjectionChainSuppressed != tc.chain || verdict.FertilitySuppressed {
					t.Fatalf("fixture: chain suppressed %v (want %v), fertility suppressed %v (want false)",
						verdict.ProjectionChainSuppressed, tc.chain, verdict.FertilitySuppressed)
				}
				if CalendarDaysBetween(today, published.OvulationDate) > 0 {
					t.Errorf("overview publishes ovulation %s ahead of today", CalendarDayKey(published.OvulationDate))
				}

				// The hero names the range from the recorded start and no single day.
				cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
				if !cycleContext.DisplayOvulationUseRange || !cycleContext.DisplayOvulationDate.IsZero() {
					t.Fatalf("hero: range %v, date %s; want the range alone", cycleContext.DisplayOvulationUseRange, CalendarDayKey(cycleContext.DisplayOvulationDate))
				}
				rangeEnd := cycleContext.DisplayOvulationRangeEnd

				feedRolled := false
				for _, event := range calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: location}) {
					if event.kind == calendarFeedKindOvulation && CalendarDaysBetween(rangeEnd, event.date) > 0 {
						if tc.chain {
							t.Errorf("the feed sends ovulation %s, past the range the hero names", CalendarDayKey(event.date))
						}
						feedRolled = feedRolled || CalendarDayKey(event.date) == rolled
					}
				}
				if !tc.chain && !feedRolled {
					t.Errorf("control: the feed does not send the chained ovulation %s", rolled)
				}

				gridRolled := false
				for _, month := range []time.Month{time.October, time.November, time.December} {
					for _, day := range BuildCalendarDayStates(user, time.Date(2026, month, 1, 0, 0, 0, 0, location), logs, stats, now, location) {
						fertile := day.IsOvulation || day.IsFertility || day.IsFertilityEdge || day.IsFertilityPeak
						if !day.InMonth || !day.Date.After(today) || !fertile {
							continue
						}
						if tc.chain {
							t.Errorf("the grid draws projected fertile or ovulation day %s", CalendarDayKey(day.Date))
						}
						gridRolled = gridRolled || (day.IsOvulation && CalendarDayKey(day.Date) == rolled)
					}
				}
				if !tc.chain && !gridRolled {
					t.Errorf("control: the grid does not draw the chained ovulation %s", rolled)
				}

				settings := WebhookReminderSettings{Enabled: true, NotifyOvulation: true, ReminderLeadDays: MaxReminderLeadDays}
				for _, reminder := range DecideDueReminders(user, settings, logs, now, location) {
					if reminder.Type == DueReminderTypeOvulation && CalendarDaysBetween(rangeEnd, reminder.EventDate) > 0 {
						t.Errorf("webhook announces ovulation %s, past the range the hero names", CalendarDayKey(reminder.EventDate))
					}
				}
			})
		})
	}
}

// TestEverySurfaceWithholdsTheFertilityHalfPastTheProjectionLength pins WEB-378:
// dates are projected median-first, the out-of-date band opens on the rounded
// average, and on a right-skewed history the days between them named an
// ovulation in a cycle anchored on a start that is already due or missed.
// From projection-length+1 the fertility half is withheld on every surface
// (reason cycle_past_projection_length); the out-of-date flag, its banner and
// the phase stay on the average and only take over from reference+1.
//
// History: cycles 27/28/28/36 (median 28, average 30), the running one from
// 2026-09-30. Cycle day 28 is the control; days 29 and 30 are the gap; day 31
// opens the out-of-date band, whose own reason replaces the new one.
func TestEverySurfaceWithholdsTheFertilityHalfPastTheProjectionLength(t *testing.T) {
	user := dayFeedbackParityUser(378)
	logs := cycleStartLogs(t, "2026-06-03", "2026-06-30", "2026-07-28", "2026-08-25", "2026-09-30")

	cases := []struct {
		cycleDay   int
		today      string
		withheld   bool
		stale      bool
		wantReason SuppressionReason
	}{
		{cycleDay: 28, today: "2026-10-27"},
		{cycleDay: 29, today: "2026-10-28", withheld: true, wantReason: SuppressionReasonCyclePastProjectionLength},
		{cycleDay: 30, today: "2026-10-29", withheld: true, wantReason: SuppressionReasonCyclePastProjectionLength},
		{cycleDay: 31, today: "2026-10-30", withheld: true, stale: true, wantReason: SuppressionReasonCycleDataStale},
	}
	for _, tc := range cases {
		t.Run(tc.today, func(t *testing.T) {
			forEachParityZone(t, func(t *testing.T, location *time.Location) {
				now := localNoon(mustParseDay(t, tc.today), location)
				today := DateAtLocation(now, location)
				stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, now, location), today, location)
				if stats.CurrentCycleDay != tc.cycleDay || DashboardProjectionCycleLength(user, stats) != 28 || DashboardCycleReferenceLength(user, stats) != 30 {
					t.Fatalf("fixture: cycle day %d, projection %d, reference %d; want %d, 28, 30",
						stats.CurrentCycleDay, DashboardProjectionCycleLength(user, stats), DashboardCycleReferenceLength(user, stats), tc.cycleDay)
				}

				// Shared verdict and the reason the JSON overview publishes.
				published, verdict, _ := PublishedOverviewStats(user, logs, stats, today, location)
				if verdict.FertilitySuppressed != tc.withheld {
					t.Errorf("fertility suppressed = %v, want %v", verdict.FertilitySuppressed, tc.withheld)
				}
				if verdict.PredictionsSuppressed {
					t.Error("fixture: the overdue gate must not fire before day 36")
				}
				hasNew := slices.Contains(verdict.Reasons, SuppressionReasonCyclePastProjectionLength)
				hasStale := slices.Contains(verdict.Reasons, SuppressionReasonCycleDataStale)
				if tc.wantReason == "" && (hasNew || hasStale) {
					t.Errorf("control: reasons %v name a late cycle", verdict.Reasons)
				}
				if tc.wantReason != "" && !slices.Contains(verdict.Reasons, tc.wantReason) {
					t.Errorf("reasons %v, want %s", verdict.Reasons, tc.wantReason)
				}
				if hasNew && hasStale {
					t.Errorf("reasons %v name one state twice", verdict.Reasons)
				}
				if published.CycleDataStale != tc.stale {
					t.Errorf("cycle_data_stale = %v, want %v (the flag stays on the average)", published.CycleDataStale, tc.stale)
				}
				if tc.withheld && (!published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero()) {
					t.Errorf("overview publishes ovulation %v and window from %v", published.OvulationDate, published.FertilityWindowStart)
				}
				if !tc.withheld && published.OvulationDate.IsZero() {
					t.Error("control: overview publishes no ovulation while the data is current")
				}

				// Dashboard hero and the notice that explains the withheld slot.
				cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
				if cycleContext.FertilitySuppressed != tc.withheld {
					t.Errorf("hero fertility suppressed = %v, want %v", cycleContext.FertilitySuppressed, tc.withheld)
				}
				if tc.withheld && !cycleContext.LateCycle.Visible {
					t.Error("the hero withholds the fertility half with no late-cycle notice explaining it")
				}
				if !tc.withheld && cycleContext.LateCycle.Visible {
					t.Errorf("control: late notice %q while the data is current", cycleContext.LateCycle.MessageKey)
				}

				// Calendar grid: no projected fertile or ovulation day ahead.
				gridFertility := 0
				for _, month := range []time.Month{time.October, time.November, time.December} {
					for _, day := range BuildCalendarDayStates(user, time.Date(2026, month, 1, 0, 0, 0, 0, location), logs, stats, now, location) {
						if day.InMonth && day.Date.After(today) && (day.IsOvulation || day.IsFertility || day.IsFertilityEdge || day.IsFertilityPeak) {
							gridFertility++
						}
					}
				}
				if tc.withheld && gridFertility != 0 {
					t.Errorf("the grid draws %d projected fertile or ovulation day(s)", gridFertility)
				}
				if !tc.withheld && gridFertility == 0 {
					t.Error("control: the grid draws no fertile day while the data is current")
				}

				// .ics feed.
				feedOvulation := 0
				for _, event := range calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: location}) {
					if event.kind == calendarFeedKindOvulation || event.kind == calendarFeedKindOvulationWindow {
						feedOvulation++
					}
				}
				if tc.withheld && feedOvulation != 0 {
					t.Errorf("the feed sends %d ovulation event(s)", feedOvulation)
				}
				if !tc.withheld && feedOvulation == 0 {
					t.Error("control: the feed sends no ovulation while the data is current")
				}

				// Webhook reminder.
				settings := WebhookReminderSettings{Enabled: true, NotifyOvulation: true, ReminderLeadDays: MaxReminderLeadDays}
				for _, reminder := range DecideDueReminders(user, settings, logs, now, location) {
					if reminder.Type == DueReminderTypeOvulation && tc.withheld {
						t.Errorf("webhook announces ovulation %s", CalendarDayKey(reminder.EventDate))
					}
				}
			})
		})
	}
}
