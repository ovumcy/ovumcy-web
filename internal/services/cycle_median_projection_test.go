package services

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestDashboardProjectionCycleLengthIsMedianFirst pins the projection length as
// median-first, in deliberate contrast to the average-first
// DashboardCycleReferenceLength. Both fall back to the user's configured cycle
// length and then the default, but only when no observed statistic exists.
func TestDashboardProjectionCycleLengthIsMedianFirst(t *testing.T) {
	cases := []struct {
		name  string
		user  *models.User
		stats CycleStats
		want  int
	}{
		{
			name:  "prefers median over average",
			user:  &models.User{CycleLength: 30},
			stats: CycleStats{MedianCycleLength: 28, AverageCycleLength: 34},
			want:  28,
		},
		{
			name:  "rounds average when no median",
			user:  &models.User{CycleLength: 30},
			stats: CycleStats{MedianCycleLength: 0, AverageCycleLength: 29.6},
			want:  30,
		},
		{
			name:  "falls back to configured cycle length with no observed statistic",
			user:  &models.User{CycleLength: 31},
			stats: CycleStats{},
			want:  31,
		},
		{
			name:  "falls back to the default with no statistic and no valid configuration",
			user:  nil,
			stats: CycleStats{},
			want:  models.DefaultCycleLength,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DashboardProjectionCycleLength(tc.user, tc.stats); got != tc.want {
				t.Fatalf("DashboardProjectionCycleLength = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestMedianProjectionAgreesAcrossSurfaces is the cross-surface regression for
// the missed-log merge scenario: cycle lengths [28,28,28,28,60] give a median of
// 28 but a mean of ~34. Before this fix the dashboard hero, webhook reminder, and
// .ics feed projected from the mean (34) while stats and the calendar grid used
// the median (28), so those surfaces drifted ~6 days late. This asserts all four
// next-period surfaces now agree on the median-based date, and that it is NOT the
// mean-based date.
func TestMedianProjectionAgreesAcrossSurfaces(t *testing.T) {
	loc := time.UTC
	base := mustParseWebhookReminderDay(t, "2026-01-01", loc)

	// Six explicit cycle starts spaced to yield lengths [28,28,28,28,60].
	startOffsets := []int{0, 28, 56, 84, 112, 172}
	logs := make([]models.DailyLog, 0, len(startOffsets))
	for _, offset := range startOffsets {
		logs = append(logs, models.DailyLog{
			Date:       base.AddDate(0, 0, offset),
			IsPeriod:   true,
			CycleStart: true,
		})
	}

	lastStart := base.AddDate(0, 0, 172)      // the most recent cycle start
	today := lastStart.AddDate(0, 0, 18)      // still inside the first projected cycle
	medianNext := lastStart.AddDate(0, 0, 28) // median projection (correct)
	meanNext := lastStart.AddDate(0, 0, 34)   // mean projection (the old bug)

	user := regularWebhookUser()
	stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(user, logs, today, loc)

	// Guard the scenario itself: median 28 must diverge from mean 34 here.
	if stats.MedianCycleLength != 28 {
		t.Fatalf("scenario setup: MedianCycleLength = %d, want 28", stats.MedianCycleLength)
	}
	if got := int(stats.AverageCycleLength + 0.5); got != 34 {
		t.Fatalf("scenario setup: rounded AverageCycleLength = %d, want 34", got)
	}
	if DashboardProjectionCycleLength(user, stats) != 28 {
		t.Fatalf("projection length = %d, want 28", DashboardProjectionCycleLength(user, stats))
	}
	if DashboardCycleReferenceLength(user, stats) != 34 {
		t.Fatalf("reference length = %d, want 34 (average-first, retained for warnings)", DashboardCycleReferenceLength(user, stats))
	}

	assertDay := func(label string, got, want time.Time) {
		t.Helper()
		if got.Format("2006-01-02") != want.Format("2006-01-02") {
			t.Fatalf("%s = %s, want %s", label, got.Format("2006-01-02"), want.Format("2006-01-02"))
		}
	}

	// 1. stats / calendar authoritative next-period (median-based).
	assertDay("stats.NextPeriodStart", stats.NextPeriodStart, medianNext)

	// 2. Dashboard hero displayed next-period.
	cycleContext := BuildDashboardCycleContext(user, stats, today, loc)
	assertDay("DisplayNextPeriodStart", cycleContext.DisplayNextPeriodStart, medianNext)

	// 3. Webhook period reminder anchor/event (lead wide enough to cover 28 days).
	reminders := DecideDueReminders(user, enabledWebhookSettings(14), logs, today, loc)
	period, ok := findDueReminder(reminders, DueReminderTypePeriod)
	if !ok {
		t.Fatalf("expected a period reminder within the lead window, got %#v", reminders)
	}
	assertDay("webhook period EventDate", period.EventDate, medianNext)

	// 4. First .ics period event.
	events := calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: today, Location: loc})
	firstPeriod, ok := firstEventOfKind(events, "period")
	if !ok {
		t.Fatalf("expected a period event in the .ics feed, got %#v", events)
	}
	assertDay("first .ics period event", firstPeriod, medianNext)

	// And prove the surfaces are no longer on the mean-based date.
	if cycleContext.DisplayNextPeriodStart.Format("2006-01-02") == meanNext.Format("2006-01-02") {
		t.Fatalf("dashboard still projecting from the mean (%s)", meanNext.Format("2006-01-02"))
	}
}

func firstEventOfKind(events []calendarFeedEvent, kind string) (time.Time, bool) {
	for _, event := range events {
		if event.kind == kind {
			return event.date, true
		}
	}
	return time.Time{}, false
}

// Guard against an accidental data-minimization regression alongside the date
// change: the .ics SUMMARY must stay neutral even in this scenario.
func TestMedianProjectionKeepsNeutralICSSummary(t *testing.T) {
	loc := time.UTC
	base := mustParseWebhookReminderDay(t, "2026-01-01", loc)
	logs := []models.DailyLog{{Date: base, IsPeriod: true, CycleStart: true}}
	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User: regularWebhookUser(), Logs: logs, Now: base, Location: loc, Disclaimer: "estimate only",
	}))
	if !strings.Contains(body, "SUMMARY:"+calendarFeedNeutralSummary) {
		t.Fatalf("expected neutral SUMMARY in feed body")
	}
}
