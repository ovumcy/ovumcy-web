package services

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The webhook reminder pass and the dashboard must read the same history. Every
// in-app surface derives its cycle statistics from the last two years
// (StatsOverviewRange); the notify pass is handed the owner's whole stored
// history. The cycle lengths, the completed-cycle count and the overdue verdict
// all read what they are given, so an owner whose old cycles outlive the window
// was paused in the app while a reminder built from a different set of cycles
// left the instance for a third-party endpoint — or the reverse, a reminder
// withheld that the dashboard still showed.
//
// The histories below are the ones that expose it. Only the last
// cyclePredictionWindow cycle lengths feed the statistics, so old cycles reach
// them only when fewer recent lengths exist to fill it, and the span between the
// last old start and the first recent one is itself counted as a cycle length.

// historyWindowCase builds an owner history from cycle starts counted back from
// today. The starts are what DetectCycleStarts reads, so the spans between them
// are the observed cycle lengths.
type historyWindowCase struct {
	name string
	// startsAgo are the cycle starts as days before today, oldest first.
	startsAgo []int
	// wantPaused is the dashboard's NextPeriodEstimatePaused for the history.
	wantPaused bool
	// wholeHistoryPaused is the overdue verdict the UNBOUNDED history would give —
	// the answer the notify pass used to act on. A case where it equals wantPaused
	// would pass without the window ever mattering.
	wholeHistoryPaused bool
}

func historyWindowLogs(today time.Time, startsAgo []int) []models.DailyLog {
	logs := make([]models.DailyLog, 0, len(startsAgo))
	for _, ago := range startsAgo {
		logs = append(logs, models.DailyLog{
			Date:       today.AddDate(0, 0, -ago),
			IsPeriod:   true,
			CycleStart: true,
		})
	}
	return logs
}

func historyWindowEveryNthDay(first int, step int, last int) []int {
	var days []int
	for ago := first; ago >= last; ago -= step {
		days = append(days, ago)
	}
	return days
}

func historyWindowCases() []historyWindowCase {
	return []historyWindowCase{
		{
			// Old 32-day cycles ending 800 days ago, then two 20-day cycles. Whole
			// history: median 32, so day 30 is inside the length and the next period
			// is projected three days out — a reminder is due. The window keeps only
			// the 20-day cycles: day 30 is past 20+7, the app is paused.
			name:               "old long cycles hide an overdue recent history",
			startsAgo:          append(historyWindowEveryNthDay(928, 32, 800), 69, 49, 29),
			wantPaused:         true,
			wholeHistoryPaused: false,
		},
		{
			// The reverse: old 18-day cycles drag the whole-history median down to
			// 18, so the notify pass saw an overdue cycle and stayed silent while the
			// dashboard, reading one 30-day recent cycle, projected the period three
			// days out.
			name:               "old short cycles pause a recent history the dashboard still projects",
			startsAgo:          append(historyWindowEveryNthDay(872, 18, 800), 57, 27),
			wantPaused:         false,
			wholeHistoryPaused: true,
		},
		{
			// Control: a steady 28-day history older than two years, a period due in
			// three days. Nothing differs between the two readings; the reminder is
			// due and the dashboard is not paused, so a guard that merely blanked
			// every reminder would fail here.
			name:               "a steady history past two years is not paused and the reminder is due",
			startsAgo:          historyWindowEveryNthDay(25+28*40, 28, 25),
			wantPaused:         false,
			wholeHistoryPaused: false,
		},
	}
}

func historyWindowUser() *models.User {
	return &models.User{ID: 31, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
}

// TestWebhookReminderPassAndDashboardReadTheSameHistoryWindow pins the parity in
// both directions: with more than two years of history, DecideDueReminders is
// empty exactly when the dashboard reports NextPeriodEstimatePaused.
func TestWebhookReminderPassAndDashboardReadTheSameHistoryWindow(t *testing.T) {
	loc := time.UTC
	today := time.Date(2026, time.October, 2, 0, 0, 0, 0, loc)

	for _, testCase := range historyWindowCases() {
		t.Run(testCase.name, func(t *testing.T) {
			user := historyWindowUser()
			logs := historyWindowLogs(today, testCase.startsAgo)

			// Fixture: the history reaches past the window, and the unbounded read
			// would have given the other verdict.
			windowStart, _ := StatsOverviewRange(today)
			if !logs[0].Date.Before(windowStart) {
				t.Fatalf("fixture: oldest log %s is inside the window starting %s",
					logs[0].Date.Format("2006-01-02"), windowStart.Format("2006-01-02"))
			}
			whole := BuildCycleStatsFromLogs(user, logs, today, loc)
			if got := DashboardCycleOverdue(user, whole); got != testCase.wholeHistoryPaused {
				t.Fatalf("fixture: whole-history DashboardCycleOverdue = %v, want %v", got, testCase.wholeHistoryPaused)
			}

			service := NewDashboardViewService(
				NewStatsService(nil, nil),
				&stubDashboardViewerProvider{logEntry: models.DailyLog{Date: today}},
				&stubDashboardDayStateProvider{logs: logs},
			)
			viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, loc)
			if err != nil {
				t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
			}
			paused := viewData.CycleContext.NextPeriodEstimatePaused
			if paused != testCase.wantPaused {
				t.Fatalf("dashboard NextPeriodEstimatePaused = %v, want %v", paused, testCase.wantPaused)
			}

			settings := WebhookReminderSettings{Enabled: true, NotifyPeriod: true, NotifyOvulation: true, ReminderLeadDays: 3}
			due := DecideDueReminders(user, settings, logs, today, loc)
			if (len(due) == 0) != paused {
				t.Fatalf("DecideDueReminders returned %d reminder(s) while the dashboard's NextPeriodEstimatePaused = %v: reminders must be empty exactly when the estimate is paused", len(due), paused)
			}
		})
	}
}

// The reminder pass cuts the history to the stats window and then reads the
// current phase and the confirmed-shift check from the cut set, where the
// dashboard hands those two the full one. The cut loses nothing they read: the
// window ends at today inclusive, the phase asks only about today's own row, and
// the thermal-shift series stops at today. The sweep puts today before, on and
// after the third elevated reading, with rows (that reading and a logged period)
// recorded for the days ahead, and requires both reads to agree between the two
// sets every day.
func TestStatsWindowCutLeavesThePhaseAndTheConfirmedShiftUnchanged(t *testing.T) {
	user := dayFeedbackParityUser(43)
	cycleStart := time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC)

	logs := lutealTenLogs(t)
	for offset := range 6 {
		logs = append(logs, models.DailyLog{Date: cycleStart.AddDate(0, 0, offset), BBT: new(thermalShiftLowBBT)})
	}
	for offset := 14; offset <= 16; offset++ {
		logs = append(logs, models.DailyLog{Date: cycleStart.AddDate(0, 0, offset), BBT: new(thermalShiftHighBBT)})
	}
	logs = append(logs, models.DailyLog{Date: cycleStart.AddDate(0, 0, 17), IsPeriod: true})
	logs = mergeLogsByDay(logs)

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		sawConfirmed, sawUnconfirmed := false, false
		for offset := 12; offset <= 19; offset++ {
			now := localNoon(cycleStart.AddDate(0, 0, offset), location)
			today := DateAtLocation(now, location)
			windowed := FilterLogsToStatsHistory(logs, now, location)
			if offset < 17 && len(windowed) >= len(logs) {
				t.Fatalf("fixture: the window kept all %d rows with rows still ahead of %s", len(logs), CalendarDayKey(today))
			}
			stats := BuildCycleStatsFromLogs(user, windowed, now, location)
			projected := stats.NextPeriodStart.AddDate(0, 0, -1)

			wantSupersedes := ConfirmedOvulationSupersedes(user, logs, stats, projected, today, location)
			if got := ConfirmedOvulationSupersedes(user, windowed, stats, projected, today, location); got != wantSupersedes {
				t.Fatalf("on %s the confirmed-shift check = %t from the cut history, %t from the full one", CalendarDayKey(today), got, wantSupersedes)
			}
			if wantPhase, gotPhase := DetectCurrentPhase(stats, logs, today, location), DetectCurrentPhase(stats, windowed, today, location); gotPhase != wantPhase {
				t.Fatalf("on %s the phase = %q from the cut history, %q from the full one", CalendarDayKey(today), gotPhase, wantPhase)
			}
			if wantSupersedes {
				sawConfirmed = true
			} else {
				sawUnconfirmed = true
			}
		}
		// Both outcomes occur in the sweep, so agreement is not two constants.
		if !sawConfirmed || !sawUnconfirmed {
			t.Fatalf("fixture: the sweep never saw both a confirmed and an unconfirmed shift (confirmed=%t unconfirmed=%t)", sawConfirmed, sawUnconfirmed)
		}
	})
}

// The dashboard puts a confirmed thermal shift ahead of the projection before it
// publishes; the reminder pass (and the .ics feed beside it) publishes the stats
// as built and honours the shift through ConfirmedOvulationSupersedes instead.
// That is only the same answer while the substitution leaves every value those
// two passes read untouched, so the sweep compares each of them with and without
// it, on days before and after the shift is confirmed: the suppression verdict,
// the projection length, and the next-period and ovulation projections. The
// ovulation reminder's cycle anchor is rebuilt from the same recorded start,
// cycle length and luteal phase as those projections, so it moves only when they
// do. A substitution that began to move one of them — the luteal phase or the
// cycle start, say — would make a reminder leave the instance for a day the
// dashboard no longer shows.
func TestReminderInputsAreTheSameWithAndWithoutTheConfirmedShift(t *testing.T) {
	user := dayFeedbackParityUser(44)
	cycleStart := time.Date(2026, time.February, 26, 0, 0, 0, 0, time.UTC)

	logs := lutealTenLogs(t)
	for offset := range 6 {
		logs = append(logs, models.DailyLog{Date: cycleStart.AddDate(0, 0, offset), BBT: new(thermalShiftLowBBT)})
	}
	for offset := 14; offset <= 16; offset++ {
		logs = append(logs, models.DailyLog{Date: cycleStart.AddDate(0, 0, offset), BBT: new(thermalShiftHighBBT)})
	}
	logs = mergeLogsByDay(logs)

	forEachParityZone(t, func(t *testing.T, location *time.Location) {
		sawConfirmed, sawUnconfirmed := false, false
		for offset := 12; offset <= 19; offset++ {
			now := localNoon(cycleStart.AddDate(0, 0, offset), location)
			today := DateAtLocation(now, location)
			windowed := FilterLogsToStatsHistory(logs, now, location)
			raw := BuildCycleStatsFromLogs(user, windowed, now, location)
			confirmed, wasConfirmed := ResolveConfirmedCycleStats(user, windowed, raw, today, location)
			day := CalendarDayKey(today)

			if wasConfirmed {
				sawConfirmed = true
				// The substitution did something, or the equalities below prove nothing.
				if sameDay(confirmed.OvulationDate, raw.OvulationDate) {
					t.Fatalf("fixture: on %s the confirmed ovulation day equals the projected one", day)
				}
			} else {
				sawUnconfirmed = true
			}

			if got, want := ResolvePredictionSuppression(user, confirmed), ResolvePredictionSuppression(user, raw); !reflect.DeepEqual(got, want) {
				t.Fatalf("on %s the suppression verdict = %+v with the shift, %+v without", day, got, want)
			}
			if got, want := DashboardProjectionCycleLength(user, confirmed), DashboardProjectionCycleLength(user, raw); got != want {
				t.Fatalf("on %s the projection length = %d with the shift, %d without", day, got, want)
			}
			cycleLength := DashboardProjectionCycleLength(user, raw)
			if got, want := DashboardUpcomingPredictions(confirmed, user, today, cycleLength), DashboardUpcomingPredictions(raw, user, today, cycleLength); !reflect.DeepEqual(got, want) {
				t.Fatalf("on %s the upcoming predictions = %+v with the shift, %+v without", day, got, want)
			}
		}
		if !sawConfirmed || !sawUnconfirmed {
			t.Fatalf("fixture: the sweep never saw both a confirmed and an unconfirmed shift (confirmed=%t unconfirmed=%t)", sawConfirmed, sawUnconfirmed)
		}
	})
}

// TestFilterLogsToStatsHistoryKeepsTheWindowInclusive pins the edges of the
// shared window: the day two years back and today are inside, the day before and
// tomorrow are outside.
func TestFilterLogsToStatsHistoryKeepsTheWindowInclusive(t *testing.T) {
	loc := time.UTC
	today := time.Date(2026, time.October, 2, 0, 0, 0, 0, loc)
	from, _ := StatsOverviewRange(today)

	logs := []models.DailyLog{
		{Date: from.AddDate(0, 0, -1)},
		{Date: from},
		{Date: today},
		{Date: today.AddDate(0, 0, 1)},
	}
	got := FilterLogsToStatsHistory(logs, today, loc)
	if len(got) != 2 || !got[0].Date.Equal(from) || !got[1].Date.Equal(today) {
		t.Fatalf("FilterLogsToStatsHistory kept %v, want exactly [%s, %s]", got, from.Format("2006-01-02"), today.Format("2006-01-02"))
	}
}
