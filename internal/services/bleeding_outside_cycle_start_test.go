package services

import (
	"slices"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// web434History is 28-day cycles, the running one from 2026-10-23 bleeding
// through 10-27, plus extra: the day(s) under test.
func web434History(t *testing.T, extra ...models.DailyLog) []models.DailyLog {
	t.Helper()

	logs := cycleStartLogs(t, "2026-07-31", "2026-08-28", "2026-09-25", "2026-10-23")
	for _, day := range []string{"2026-10-24", "2026-10-25", "2026-10-26", "2026-10-27"} {
		logs = append(logs, models.DailyLog{Date: mustParseDay(t, day), IsPeriod: true, Flow: models.FlowMedium})
	}
	return append(logs, extra...)
}

// TestAForcedUncertainStartOpensTheCycleByTheUnmarkedRules pins the WEB-434
// repro: 2026-11-02 forced with "Mark anyway" after the short-gap warning. The
// uncertain mark counts as no mark, so the lone day opens a cycle on the day it
// is logged and the next; with no second period day it lapses on 11-04 and the
// running cycle from 10-23 stands again.
func TestAForcedUncertainStartOpensTheCycleByTheUnmarkedRules(t *testing.T) {
	user := dayFeedbackParityUser(434)
	forced := models.DailyLog{Date: mustParseDay(t, "2026-11-02"), IsPeriod: true, CycleStart: true, IsUncertain: true, Flow: models.FlowMedium}
	logs := web434History(t, forced)

	for _, tc := range []struct {
		today     string
		wantStart string
		wantDay   int
	}{
		{today: "2026-11-02", wantStart: "2026-11-02", wantDay: 1},
		{today: "2026-11-03", wantStart: "2026-11-02", wantDay: 2},
		{today: "2026-11-04", wantStart: "2026-10-23", wantDay: 13},
	} {
		t.Run(tc.today, func(t *testing.T) {
			forEachParityZone(t, func(t *testing.T, location *time.Location) {
				now := localNoon(mustParseDay(t, tc.today), location)
				today := DateAtLocation(now, location)
				stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, now, location), today, location)
				if CalendarDayKey(stats.LastPeriodStart) != tc.wantStart || stats.CurrentCycleDay != tc.wantDay {
					t.Fatalf("last period start %s, cycle day %d; want %s, %d", CalendarDayKey(stats.LastPeriodStart), stats.CurrentCycleDay, tc.wantStart, tc.wantDay)
				}
				published, _, _ := PublishedOverviewStats(user, logs, stats, today, location)
				if published.CurrentPhase == "menstrual" && published.CurrentFertility == FertilityStatusFertile {
					t.Errorf("overview: phase menstrual and fertility fertile on %s", tc.today)
				}
			})
		})
	}
}

// TestNoSurfaceCallsADayMenstrualAndFertileAtOnce pins the WEB-434 floor: a
// day recorded as bleeding that opens no cycle (here spotting, which never
// does) was "menstrual" (read off the log) and "fertile" (read off the running
// projection) on every surface at once. On that day and the next the fertility
// half is withheld everywhere under its own reason; the day after, the bleed is
// history and the projection stands again. 2026-10-31 is the control: inside
// the window, no bleed.
func TestNoSurfaceCallsADayMenstrualAndFertileAtOnce(t *testing.T) {
	user := dayFeedbackParityUser(434)
	spotting := models.DailyLog{Date: mustParseDay(t, "2026-11-02"), IsPeriod: true, CycleStart: true, IsUncertain: true, Flow: models.FlowSpotting}
	logs := web434History(t, spotting)

	cases := []struct {
		today    string
		withheld bool
	}{
		{today: "2026-10-31"},
		{today: "2026-11-02", withheld: true},
		{today: "2026-11-03", withheld: true},
		{today: "2026-11-04"},
	}
	for _, tc := range cases {
		t.Run(tc.today, func(t *testing.T) {
			forEachParityZone(t, func(t *testing.T, location *time.Location) {
				now := localNoon(mustParseDay(t, tc.today), location)
				today := DateAtLocation(now, location)
				stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, now, location), today, location)
				if CalendarDayKey(stats.LastPeriodStart) != "2026-10-23" {
					t.Fatalf("fixture: last period start %s, want 2026-10-23", CalendarDayKey(stats.LastPeriodStart))
				}

				published, verdict, _ := PublishedOverviewStats(user, logs, stats, today, location)
				if published.CurrentPhase == "menstrual" && published.CurrentFertility == FertilityStatusFertile {
					t.Errorf("overview: phase menstrual and fertility fertile on %s", tc.today)
				}
				if verdict.FertilitySuppressed != tc.withheld {
					t.Errorf("fertility suppressed = %v, want %v", verdict.FertilitySuppressed, tc.withheld)
				}
				if hasReason := slices.Contains(verdict.Reasons, SuppressionReasonBleedingOutsideCycleStart); hasReason != tc.withheld {
					t.Errorf("reasons %v, want %s named: %v", verdict.Reasons, SuppressionReasonBleedingOutsideCycleStart, tc.withheld)
				}
				if verdict.PredictionsSuppressed {
					t.Error("the next-period estimate is anchored on a recorded start and stays")
				}
				if tc.withheld && (!published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero()) {
					t.Errorf("overview publishes ovulation %v and window from %v", published.OvulationDate, published.FertilityWindowStart)
				}
				if !tc.withheld && published.OvulationDate.IsZero() {
					t.Error("control: overview publishes no ovulation")
				}

				pageStats, pageVerdict := PublishedStats(user, stats, logs, today, location)
				if pageStats.CurrentPhase == "menstrual" && pageStats.CurrentFertility == FertilityStatusFertile {
					t.Errorf("/stats: phase menstrual and fertility fertile on %s", tc.today)
				}
				if pageVerdict.FertilitySuppressed != tc.withheld {
					t.Errorf("/stats fertility suppressed = %v, want %v", pageVerdict.FertilitySuppressed, tc.withheld)
				}

				cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
				if cycleContext.FertilitySuppressed != tc.withheld {
					t.Errorf("hero fertility suppressed = %v, want %v", cycleContext.FertilitySuppressed, tc.withheld)
				}

				gridFertile := 0
				for _, month := range []time.Month{time.October, time.November, time.December} {
					for _, day := range BuildCalendarDayStates(user, time.Date(2026, month, 1, 0, 0, 0, 0, location), logs, stats, now, location) {
						if day.InMonth && !day.Date.Before(today) && (day.IsOvulation || day.IsFertility || day.IsFertilityEdge || day.IsFertilityPeak) {
							gridFertile++
						}
					}
				}
				if tc.withheld && gridFertile != 0 {
					t.Errorf("the grid draws %d projected fertile or ovulation day(s)", gridFertile)
				}

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
					t.Error("control: the feed sends no ovulation")
				}

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
