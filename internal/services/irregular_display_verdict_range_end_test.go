package services

import (
	"strings"
	"testing"
	"time"
)

// The second half of TestIrregularSpreadSendsTheDashboardRangeNotTheMedian: a
// range that leaves the instance carries its LAST day as well as its first —
// on the reminder, in the payload, in the sentence of every locale, and as the
// feed event's DTEND — so no consumer is left reading the first day as the one.

func TestIrregularSpreadRangeLeavesWithItsLastDay(t *testing.T) {
	logs := irregularVerdictLogs(irregularRangeOffsets...)
	now := irregularVerdictBase.AddDate(0, 0, irregularRangeDayOffset)
	today := DateAtLocation(now, time.UTC)
	user := irregularVerdictUser(true)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	windowEnd, ovulationEnd := cycleContext.DisplayNextPeriodRangeEnd, cycleContext.DisplayOvulationRangeEnd

	reminders := DecideDueReminders(user, enabledWebhookSettings(MaxReminderLeadDays), logs, now, time.UTC)
	period, _ := findDueReminder(reminders, DueReminderTypePeriod)
	ovulation, _ := findDueReminder(reminders, DueReminderTypeOvulation)
	if CalendarDayKey(period.EventDateEnd) != CalendarDayKey(windowEnd) || CalendarDayKey(ovulation.EventDateEnd) != CalendarDayKey(ovulationEnd) {
		t.Fatalf("webhook: range ends period %s / ovulation %s, want the dashboard's %s / %s",
			CalendarDayKey(period.EventDateEnd), CalendarDayKey(ovulation.EventDateEnd), CalendarDayKey(windowEnd), CalendarDayKey(ovulationEnd))
	}

	provider := newLocaleCopyProvider(t)
	service := NewWebhookNotifyService(nil, nil, nil, nil, provider)
	for _, language := range provider.manager.SupportedLanguages() {
		for _, reminder := range []DueReminder{period, ovulation} {
			payload := service.buildPayload(reminder, language)
			first, last := reminder.EventDate.Format("2006-01-02"), reminder.EventDateEnd.Format("2006-01-02")
			if payload.EventDate != first || payload.EventDateEnd != last {
				t.Fatalf("%s %s payload: %s..%s, want %s..%s", language, reminder.Type, payload.EventDate, payload.EventDateEnd, first, last)
			}
			if !strings.Contains(payload.Message, first) || !strings.Contains(payload.Message, last) || strings.Contains(payload.Message, "%!") {
				t.Fatalf("%s %s message %q must name both %s and %s", language, reminder.Type, payload.Message, first, last)
			}
		}
	}

	// A watermark written before windows were keyed on their first day holds the
	// median inside the window; it still covers the window, so an upgrade does not
	// announce the current cycle a second time.
	median := DashboardUpcomingPredictions(stats, user, today, DashboardProjectionCycleLength(user, stats)).NextPeriodStart
	if !irregularVerdictWithin(median, period.EventDate, period.EventDateEnd) {
		t.Fatalf("fixture: the median %s should lie inside the window %s..%s", CalendarDayKey(median), CalendarDayKey(period.EventDate), CalendarDayKey(period.EventDateEnd))
	}
	oldShape := enabledWebhookSettings(MaxReminderLeadDays)
	oldShape.PeriodWatermark = &median
	again, skipped := decideDueReminders(user, oldShape, logs, now, time.UTC)
	if _, resent := findDueReminder(again, DueReminderTypePeriod); resent || skipped == 0 {
		t.Fatalf("webhook: an old-shape watermark on the median %s does not cover the window (resent=%t, skipped=%d)", CalendarDayKey(median), resent, skipped)
	}

	// A single-date reminder keeps the field absent.
	single := service.buildPayload(DueReminder{Type: DueReminderTypePeriod, EventDate: today}, "en")
	if single.EventDateEnd != "" {
		t.Fatalf("single-date payload carries event_date_end %q", single.EventDateEnd)
	}

	feed := string(BuildCalendarFeedICS(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: time.UTC, Disclaimer: "estimate"}))
	for _, want := range []struct{ start, end time.Time }{
		{cycleContext.DisplayNextPeriodRangeStart, windowEnd},
		{cycleContext.DisplayOvulationRangeStart, ovulationEnd},
	} {
		block := "DTSTART;VALUE=DATE:" + want.start.Format(calendarFeedDateLayout) + "\r\nDTEND;VALUE=DATE:" + want.end.AddDate(0, 0, 1).Format(calendarFeedDateLayout)
		if !strings.Contains(feed, block) {
			t.Fatalf(".ics feed: no event spanning %s..%s:\n%s", CalendarDayKey(want.start), CalendarDayKey(want.end), feed)
		}
	}
}
