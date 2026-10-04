package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The irregular-mode display verdict on every surface that names a projected
// date: the dashboard header and banner, the calendar grid, the JSON overview
// (PublishedStats), the webhook reminder and the .ics feed.
//
//   - One or two completed cycles (DashboardAwaitingIrregularHistory): the
//     dashboard shows "needs more cycles" instead of both dates, so no surface
//     names either — the grid paints no projection, the reminder and the feed
//     send nothing.
//   - Three or more with a spread: the dashboard shows a start window and an
//     ovulation range, so the reminder and the feed carry those ranges, not the
//     median day inside them.
//
// Each case has a positive control on the same history that does send, so a
// silent surface reads as the verdict and not as a fixture that never fires.

var irregularVerdictBase = time.Date(2026, time.January, 5, 0, 0, 0, 0, time.UTC)

// irregularVerdictLogs records one period start per offset (days from the base).
func irregularVerdictLogs(offsets ...int) []models.DailyLog {
	logs := make([]models.DailyLog, 0, len(offsets))
	for _, offset := range offsets {
		logs = append(logs, models.DailyLog{Date: irregularVerdictBase.AddDate(0, 0, offset), IsPeriod: true, CycleStart: true})
	}
	return logs
}

func irregularVerdictUser(irregular bool) *models.User {
	return &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14, IrregularCycle: irregular}
}

// Two completed cycles (26 and 34 days), the current one started on day 60.
var irregularThinOffsets = []int{0, 26, 60}

// irregularThinDays are the two days the thin fixture is read on: cycle day 6,
// with the ovulation inside the widest lead window, and cycle day 19, with the
// next period inside it.
var irregularThinDays = map[string]int{"ovulation in window": 60 + 5, "next period in window": 60 + 18}

func TestIrregularThinHistorySendsNoDateFromAnySurface(t *testing.T) {
	logs := irregularVerdictLogs(irregularThinOffsets...)
	for name, dayOffset := range irregularThinDays {
		t.Run(name, func(t *testing.T) {
			now := irregularVerdictBase.AddDate(0, 0, dayOffset)
			today := DateAtLocation(now, time.UTC)

			// Positive control: the same history without irregular mode does send,
			// paint and publish — so what follows is the verdict, not a quiet fixture.
			regular := irregularVerdictUser(false)
			if len(DecideDueReminders(regular, enabledWebhookSettings(MaxReminderLeadDays), logs, now, time.UTC)) == 0 {
				t.Fatal("control: a regular owner on this history gets no reminder — the fixture proves nothing")
			}
			if len(calendarFeedEvents(CalendarFeedICSInput{User: regular, Logs: logs, Now: now, Location: time.UTC})) == 0 {
				t.Fatal("control: a regular owner on this history gets no feed event")
			}
			regularStats := BuildCycleStatsFromLogs(regular, logs, now, time.UTC)
			if irregularVerdictPaintedDays(buildCalendarPredictionMaps(regular, logs, regularStats, today.AddDate(0, 2, 0), now, time.UTC)) == 0 {
				t.Fatal("control: a regular owner on this history gets no projected grid day")
			}

			user := irregularVerdictUser(true)
			stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
			if stats.CompletedCycleCount != 2 {
				t.Fatalf("fixture: CompletedCycleCount = %d, want 2", stats.CompletedCycleCount)
			}

			// Every surface is checked and every divergence reported (Errorf), so a
			// run on a tree without the shared verdict names each surface that sends.
			cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
			if !cycleContext.DisplayNextPeriodNeedsData || !cycleContext.DisplayOvulationNeedsData {
				t.Errorf("dashboard: needs-data captions = %t/%t, want both", cycleContext.DisplayNextPeriodNeedsData, cycleContext.DisplayOvulationNeedsData)
			}
			if !cycleContext.DisplayNextPeriodStart.IsZero() || !cycleContext.DisplayOvulationDate.IsZero() {
				t.Errorf("dashboard: names next period %s / ovulation %s beside \"needs more cycles\"",
					CalendarDayKey(cycleContext.DisplayNextPeriodStart), CalendarDayKey(cycleContext.DisplayOvulationDate))
			}
			if banner := BuildDashboardReminderBanner(cycleContext, today, MaxReminderLeadDays); banner.Show {
				t.Errorf("banner: shows %q", banner.Kind)
			}

			published, suppression := PublishedStats(user, stats, logs, today, time.UTC)
			if !suppression.PredictionsSuppressed || !irregularVerdictHasReason(suppression, "irregular_needs_more_cycles") {
				t.Errorf("published verdict = %+v, want both halves suppressed with the irregular reason", suppression)
			}
			if !published.NextPeriodStart.IsZero() || !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() {
				t.Errorf("JSON overview: publishes next period %s / ovulation %s", CalendarDayKey(published.NextPeriodStart), CalendarDayKey(published.OvulationDate))
			}

			if painted := irregularVerdictPaintedDays(buildCalendarPredictionMaps(user, logs, stats, today.AddDate(0, 2, 0), now, time.UTC)); painted != 0 {
				t.Errorf("calendar grid: paints %d projected day(s)", painted)
			}
			if reminders := DecideDueReminders(user, enabledWebhookSettings(MaxReminderLeadDays), logs, now, time.UTC); len(reminders) != 0 {
				t.Errorf("webhook: sends %d reminder(s), first %s on %s", len(reminders), reminders[0].Type, CalendarDayKey(reminders[0].EventDate))
			}
			if events := calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: time.UTC}); len(events) != 0 {
				t.Errorf(".ics feed: sends %d event(s), first %s on %s", len(events), events[0].kind, CalendarDayKey(events[0].date))
			}
		})
	}
}

// Three completed cycles (26, 30 and 34 days), the current one started on day
// 90; read on cycle day 17, where the start window (cycle days 27–35) opens
// inside the lead window and the ovulation range is open today.
var irregularRangeOffsets = []int{0, 26, 56, 90}

const irregularRangeDayOffset = 90 + 16

func TestIrregularSpreadSendsTheDashboardRangeNotTheMedian(t *testing.T) {
	logs := irregularVerdictLogs(irregularRangeOffsets...)
	now := irregularVerdictBase.AddDate(0, 0, irregularRangeDayOffset)
	today := DateAtLocation(now, time.UTC)
	user := irregularVerdictUser(true)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if stats.CompletedCycleCount != 3 || stats.MinCycleLength >= stats.MaxCycleLength {
		t.Fatalf("fixture: %d completed cycles, lengths %d..%d — want three with a spread", stats.CompletedCycleCount, stats.MinCycleLength, stats.MaxCycleLength)
	}

	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if !cycleContext.DisplayNextPeriodUseRange || !cycleContext.DisplayOvulationUseRange {
		t.Fatalf("dashboard: ranges = %t/%t, want both", cycleContext.DisplayNextPeriodUseRange, cycleContext.DisplayOvulationUseRange)
	}
	windowStart, windowEnd := cycleContext.DisplayNextPeriodRangeStart, cycleContext.DisplayNextPeriodRangeEnd
	ovulationStart, ovulationEnd := cycleContext.DisplayOvulationRangeStart, cycleContext.DisplayOvulationRangeEnd
	if banner := BuildDashboardReminderBanner(cycleContext, today, MaxReminderLeadDays); banner.Show {
		t.Errorf("banner: counts down to %q where the header shows a range", banner.Kind)
	}

	reminders := DecideDueReminders(user, enabledWebhookSettings(MaxReminderLeadDays), logs, now, time.UTC)
	period, ok := findDueReminder(reminders, DueReminderTypePeriod)
	if !ok {
		t.Errorf("webhook: no period reminder for a start window opening inside the lead window, got %#v", reminders)
	} else if CalendarDayKey(period.EventDate) != CalendarDayKey(windowStart) {
		t.Errorf("webhook: period reminder on %s, want the start window's first day %s (dashboard window %s..%s)",
			CalendarDayKey(period.EventDate), CalendarDayKey(windowStart), CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
	}
	ovulation, ok := findDueReminder(reminders, DueReminderTypeOvulation)
	if !ok {
		t.Errorf("webhook: no ovulation reminder while the ovulation range is open, got %#v", reminders)
	} else if CalendarDayKey(ovulation.EventDate) != CalendarDayKey(ovulationStart) {
		t.Errorf("webhook: ovulation reminder on %s, want the range's first day %s (dashboard range %s..%s)",
			CalendarDayKey(ovulation.EventDate), CalendarDayKey(ovulationStart), CalendarDayKey(ovulationStart), CalendarDayKey(ovulationEnd))
	}

	events := calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: time.UTC})
	prediction := DashboardUpcomingPredictions(stats, user, today, DashboardProjectionCycleLength(user, stats))
	// The median ovulation of this cycle has passed, so the projection has
	// rolled to the NEXT cycle's day — outside the range, which belongs to this
	// cycle. That day must stay in the feed: a window replaces only the single
	// day inside it.
	if irregularVerdictWithin(prediction.OvulationDate, ovulationStart, ovulationEnd) {
		t.Fatalf("fixture: the rolled ovulation %s should fall after the range %s..%s", CalendarDayKey(prediction.OvulationDate), CalendarDayKey(ovulationStart), CalendarDayKey(ovulationEnd))
	}
	var periodWindow, ovulationWindow, rolledOvulation bool
	for _, event := range events {
		day := CalendarDayKey(event.date)
		switch {
		case event.kind == "period-window" && day == CalendarDayKey(windowStart):
			periodWindow = true
		case event.kind == "ovulation-window" && day == CalendarDayKey(ovulationStart):
			ovulationWindow = true
		case event.kind == "period" && irregularVerdictWithin(event.date, windowStart, windowEnd):
			t.Errorf(".ics feed: sends the next period %s as one day inside the window %s..%s", day, CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
		case event.kind == "ovulation" && irregularVerdictWithin(event.date, ovulationStart, ovulationEnd):
			t.Errorf(".ics feed: sends the ovulation %s as one day inside the range %s..%s", day, CalendarDayKey(ovulationStart), CalendarDayKey(ovulationEnd))
		case event.kind == "ovulation" && day == CalendarDayKey(prediction.OvulationDate):
			rolledOvulation = true
		}
	}
	if !periodWindow || !ovulationWindow {
		t.Errorf(".ics feed: period window %t, ovulation window %t, want both", periodWindow, ovulationWindow)
	}
	if !rolledOvulation {
		t.Errorf(".ics feed: drops the next cycle's ovulation %s, which lies outside this cycle's range", CalendarDayKey(prediction.OvulationDate))
	}

	// The grid's start-window shading is the dashboard's window, day for day.
	maps := buildCalendarPredictionMaps(user, logs, stats, today.AddDate(0, 2, 0), now, time.UTC)
	for day := windowStart; !day.After(windowEnd); day = day.AddDate(0, 0, 1) {
		if !maps.predictedStartRange[CalendarDayKey(day)] {
			t.Errorf("calendar grid: start window misses %s of %s..%s", CalendarDayKey(day), CalendarDayKey(windowStart), CalendarDayKey(windowEnd))
		}
	}
	if len(maps.predictedStartRange) != CalendarDaysBetween(windowStart, windowEnd)+1 {
		t.Errorf("calendar grid: start window shades %d day(s), want the dashboard's %d", len(maps.predictedStartRange), CalendarDaysBetween(windowStart, windowEnd)+1)
	}
}

func irregularVerdictPaintedDays(maps calendarPredictionMaps) int {
	return len(maps.predictedPeriod) + len(maps.predictedStartRange) + len(maps.preFertile) + len(maps.fertilityEdge) +
		len(maps.fertilityPeak) + len(maps.ovulation) + len(maps.tentativeOvulation)
}

func irregularVerdictWithin(day time.Time, first time.Time, last time.Time) bool {
	return CalendarDaysBetween(first, day) >= 0 && CalendarDaysBetween(day, last) >= 0
}

func irregularVerdictHasReason(suppression PredictionSuppression, reason SuppressionReason) bool {
	for _, candidate := range suppression.Reasons {
		if candidate == reason {
			return true
		}
	}
	return false
}
