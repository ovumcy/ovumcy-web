package services

import (
	"testing"
	"time"
)

// TestEverySurfaceNamesOneNextPeriodAcrossTheOverdueBand pins WEB-376: from the
// day after the reference length to the overdue gate, the hero and the JSON
// overview name the running cycle's own next period — a day already due or
// behind the owner — so no surface may chain further cycles forward from that
// missed start (ProjectionChainSuppressed). The calendar grid and the .ics feed
// did: on cycle day 31 the grid painted 2026-11-25 and 2026-12-23 beside a hero
// naming 2026-10-28, and the feed sent the chain without the date the hero named.
//
// History: three completed 28-day cycles, the running one from 2026-09-30, so
// the running cycle's next period is 2026-10-28 (cycle day 29) and the overdue
// gate fires on cycle day 36. Cycle day 28 is the control: the data is current
// and the chain is the ordinary projection.
//
// The late notice is asked here too: on cycle day 29 the period is due that very
// day, the cycle has run 28 days, and "1 day longer than your usual range" beside
// "Period likely today" claims an excess nobody has observed yet.
func TestEverySurfaceNamesOneNextPeriodAcrossTheOverdueBand(t *testing.T) {
	user := dayFeedbackParityUser(376)
	logs := cycleStartLogs(t, "2026-07-08", "2026-08-05", "2026-09-02", "2026-09-30")
	const running = "2026-10-28"
	// Past the running cycle's predicted band and short of the first chained
	// start (2026-11-25): any projected period or start-window day on or after
	// this day belongs to the chain.
	const chainFrom = "2026-11-10"

	cases := []struct {
		cycleDay   int
		today      string
		stale      bool
		overdue    bool
		wantHero   string
		lateKey    string
		lateDays   int
		chainDrawn bool
	}{
		{cycleDay: 28, today: "2026-10-27", wantHero: running, chainDrawn: true},
		{cycleDay: 29, today: "2026-10-28", stale: true, wantHero: running, lateKey: LateCycleFertilityPausedKey},
		{cycleDay: 35, today: "2026-11-03", stale: true, wantHero: running, lateKey: LateCycleBeyondRangeKey, lateDays: 6},
		{cycleDay: 36, today: "2026-11-04", stale: true, overdue: true, lateKey: LateCycleBeyondRangeKey, lateDays: 7},
	}

	for _, tc := range cases {
		t.Run(tc.today, func(t *testing.T) {
			forEachParityZone(t, func(t *testing.T, location *time.Location) {
				now := localNoon(mustParseDay(t, tc.today), location)
				today := DateAtLocation(now, location)
				stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, now, location), today, location)

				if stats.CurrentCycleDay != tc.cycleDay {
					t.Fatalf("fixture: cycle day %d, want %d", stats.CurrentCycleDay, tc.cycleDay)
				}
				if got := DashboardCyclePastReferenceLength(user, stats); got != tc.stale {
					t.Fatalf("fixture: out-of-date verdict %v, want %v", got, tc.stale)
				}
				if got := PredictionsSuppressed(user, stats); got != tc.overdue {
					t.Fatalf("fixture: overdue gate %v, want %v", got, tc.overdue)
				}
				if got, want := ProjectionChainSuppressed(user, stats), !tc.chainDrawn; got != want {
					t.Errorf("shared verdict: ProjectionChainSuppressed = %v, want %v", got, want)
				}

				// Dashboard hero.
				cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
				if got := calendarDayKeyOrEmpty(cycleContext.DisplayNextPeriodStart); got != tc.wantHero {
					t.Errorf("hero names next period %q, want %q", got, tc.wantHero)
				}
				if got := cycleContext.LateCycle.MessageKey; got != tc.lateKey {
					t.Errorf("late notice %q, want %q", got, tc.lateKey)
				}
				if got := cycleContext.LateCycle.Days; got != tc.lateDays {
					t.Errorf("late notice counts %d day(s) past the range, want %d", got, tc.lateDays)
				}

				// JSON overview (and /stats, which publishes the same copy).
				published, _, _ := PublishedOverviewStats(user, logs, stats, today, location)
				if got := calendarDayKeyOrEmpty(published.NextPeriodStart); got != tc.wantHero {
					t.Errorf("overview publishes next period %q, want %q", got, tc.wantHero)
				}

				// Calendar grid.
				chainOnGrid := []string{}
				for _, month := range []time.Month{time.October, time.November, time.December} {
					for _, day := range BuildCalendarDayStates(user, time.Date(2026, month, 1, 0, 0, 0, 0, location), logs, stats, now, location) {
						if day.InMonth && (day.IsPredicted || day.IsPredictedStartWindow) && day.DateString >= chainFrom {
							chainOnGrid = append(chainOnGrid, day.DateString)
						}
					}
				}
				if tc.chainDrawn && len(chainOnGrid) == 0 {
					t.Errorf("control: the grid drew no chained cycle while the data is current")
				}
				if !tc.chainDrawn && len(chainOnGrid) != 0 {
					t.Errorf("the grid chains cycles from a missed start: %v", chainOnGrid)
				}

				// .ics feed.
				feedChain := []string{}
				for _, event := range calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: location}) {
					key := CalendarDayKey(event.date)
					if key >= chainFrom {
						feedChain = append(feedChain, event.kind+" "+key)
						continue
					}
					if event.kind == calendarFeedKindPeriod && key != running {
						t.Errorf("the feed sends a period start %s the hero does not name", key)
					}
				}
				if tc.chainDrawn && len(feedChain) == 0 {
					t.Errorf("control: the feed sent no chained cycle while the data is current")
				}
				if !tc.chainDrawn && len(feedChain) != 0 {
					t.Errorf("the feed chains cycles from a missed start: %v", feedChain)
				}

				// Webhook reminder: the period it announces is the hero's.
				settings := WebhookReminderSettings{Enabled: true, NotifyPeriod: true, NotifyOvulation: true, ReminderLeadDays: MaxReminderLeadDays}
				for _, reminder := range DecideDueReminders(user, settings, logs, now, location) {
					if reminder.Type == DueReminderTypePeriod && CalendarDayKey(reminder.EventDate) != tc.wantHero {
						t.Errorf("webhook announces period %s, hero names %q", CalendarDayKey(reminder.EventDate), tc.wantHero)
					}
					if reminder.Type == DueReminderTypeOvulation && tc.stale {
						t.Errorf("webhook announces ovulation %s in the out-of-date band", CalendarDayKey(reminder.EventDate))
					}
				}
			})
		})
	}
}

func calendarDayKeyOrEmpty(day time.Time) string {
	if day.IsZero() {
		return ""
	}
	return CalendarDayKey(day)
}
