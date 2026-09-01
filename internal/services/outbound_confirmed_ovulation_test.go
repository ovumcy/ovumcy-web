package services

// outbound_confirmed_ovulation_test.go — the two surfaces that leave the
// instance stop announcing an ovulation the temperatures have already answered.
//
// The on-screen surfaces replace the projected day with the measured one. The
// .ics feed and the webhook reminder cannot: both exist to announce a day still
// ahead. Left alone they kept publishing the projection, so on the projected day
// itself the owner read the measured day on the dashboard and the grid and the
// projected day in a subscribed calendar and in a payload leaving the instance —
// two dates for one shift, at the moment the gap between them is largest.

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// outboundConfirmedFixture builds three 28-day cycles — two completed, which
// clears the first-cycle fertility floor — and a thermal shift in the third.
// Undisturbed readings on cycle days 6..11 fill the coverline window and days
// 12..14 are elevated, so the shared 3-over-6 detector names cycle day 11,
// 2026-03-11, while the model projects cycle day 14: 2026-03-14, which is also
// "today". withShift=false returns the same cycles with no temperatures at all,
// the control every assertion below is read against.
func outboundConfirmedFixture(t *testing.T, withShift bool) (*models.User, []models.DailyLog, time.Time) {
	t.Helper()

	user := &models.User{
		ID:           1,
		Role:         models.RoleOwner,
		TrackBBT:     true,
		CycleLength:  28,
		PeriodLength: 5,
	}

	logs := make([]models.DailyLog, 0, 24)
	for _, start := range []string{"2026-01-04", "2026-02-01", "2026-03-01"} {
		cycleStart := mustParseDashboardDay(t, start)
		for offset := range 5 {
			logs = append(logs, models.DailyLog{
				UserID:     user.ID,
				Date:       cycleStart.AddDate(0, 0, offset),
				IsPeriod:   true,
				CycleStart: offset == 0,
				Flow:       models.FlowMedium,
			})
		}
	}
	if withShift {
		for _, day := range []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11"} {
			logs = append(logs, outboundBBTLog(t, user.ID, day, 36.20))
		}
		for _, day := range []string{"2026-03-12", "2026-03-13", "2026-03-14"} {
			logs = append(logs, outboundBBTLog(t, user.ID, day, 36.50))
		}
	}

	return user, logs, mustParseDashboardDay(t, "2026-03-14")
}

func outboundBBTLog(t *testing.T, userID uint, day string, temperature float64) models.DailyLog {
	t.Helper()

	value := temperature
	return models.DailyLog{UserID: userID, Date: mustParseDashboardDay(t, day), BBT: &value}
}

// TestOutboundFixtureConfirmsTheShift pins the fixture before either surface is
// asked about it, so a failure below reads as a surface defect rather than as a
// fixture that never confirmed anything — and pins the control, so an assertion
// that a date is absent cannot pass on a feed that never carried it.
func TestOutboundFixtureConfirmsTheShift(t *testing.T) {
	user, logs, now := outboundConfirmedFixture(t, true)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

	if got := CalendarDayKey(stats.OvulationDate); got != "2026-03-14" {
		t.Fatalf("fixture: the model must project 2026-03-14, got %s", got)
	}
	confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, now, time.UTC)
	if !ok {
		t.Fatal("fixture: the detector must confirm a shift in this cycle")
	}
	if got := CalendarDayKey(confirmed); got != "2026-03-11" {
		t.Fatalf("fixture: confirmed ovulation = %s, want 2026-03-11", got)
	}

	_, controlLogs, _ := outboundConfirmedFixture(t, false)
	controlStats := BuildCycleStatsFromLogs(user, controlLogs, now, time.UTC)
	if _, ok := ConfirmedCurrentCycleOvulation(user, controlLogs, controlStats, now, time.UTC); ok {
		t.Fatal("control: temperatures were removed, so nothing may be confirmed")
	}
}

// TestConfirmedOvulationLeavesTheNextCyclesProjectionAlone is the bound. The
// confirmation reads the CURRENT cycle only, so a projection that has rolled
// past this cycle's end is about a day the shift says nothing about — withholding
// it would withdraw a reminder the account is owed rather than de-duplicate one.
func TestConfirmedOvulationLeavesTheNextCyclesProjectionAlone(t *testing.T) {
	user, logs, now := outboundConfirmedFixture(t, true)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

	thisCycle := mustParseDashboardDay(t, "2026-03-14")
	if !ConfirmedOvulationSupersedes(user, logs, stats, thisCycle, now, time.UTC) {
		t.Fatal("this cycle's projected day is exactly what the shift supersedes")
	}
	nextCycle := mustParseDashboardDay(t, "2026-04-11")
	if ConfirmedOvulationSupersedes(user, logs, stats, nextCycle, now, time.UTC) {
		t.Fatal("the next cycle's projection is not this shift's subject")
	}
}

// TestConfirmedOvulationSupersedesNothingWithoutAProjectedDay covers the branch
// the webhook reaches. decideDueReminders asks this BEFORE
// decideOvulationReminder's own OvulationImpossible gate, and
// DashboardUpcomingPredictions leaves OvulationDate zero exactly there — a
// cycle too short to seat the luteal phase at all.
//
// The guard is load-bearing rather than defensive: a zero date is year 1, so the
// window bound below it reads as comfortably before this cycle's end and passes,
// the detector then confirms, and the answer would come back true for a day that
// does not exist.
func TestConfirmedOvulationSupersedesNothingWithoutAProjectedDay(t *testing.T) {
	user, logs, now := outboundConfirmedFixture(t, true)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

	if ConfirmedOvulationSupersedes(user, logs, stats, time.Time{}, now, time.UTC) {
		t.Fatal("a projection with no day of its own cannot be one a shift superseded")
	}
}

func TestCalendarFeedWithholdsAnOvulationTheTemperaturesHaveAnswered(t *testing.T) {
	user, logs, now := outboundConfirmedFixture(t, true)

	body := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       logs,
		Now:        now,
		Location:   time.UTC,
		Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
	}))
	if strings.Contains(body, "DTSTART;VALUE=DATE:20260314") {
		t.Fatalf("the feed published the projected ovulation the shift superseded:\n%s", body)
	}
	// The bound, on the surface rather than on the helper: the next projected
	// cycle's ovulation still ships, so this is a superseded day dropped and not
	// the ovulation half of the feed going quiet.
	if !strings.Contains(body, "DTSTART;VALUE=DATE:20260411") {
		t.Fatalf("the next cycle's projected ovulation must still ship:\n%s", body)
	}

	_, controlLogs, _ := outboundConfirmedFixture(t, false)
	control := string(BuildCalendarFeedICS(CalendarFeedICSInput{
		User:       user,
		Logs:       controlLogs,
		Now:        now,
		Location:   time.UTC,
		Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
	}))
	if !strings.Contains(control, "DTSTART;VALUE=DATE:20260314") {
		t.Fatalf("control: with no shift recorded the projected day must still ship:\n%s", control)
	}
}

func TestWebhookReminderWithholdsAnOvulationTheTemperaturesHaveAnswered(t *testing.T) {
	user, logs, now := outboundConfirmedFixture(t, true)

	for _, reminder := range DecideDueReminders(user, enabledWebhookSettings(3), logs, now, time.UTC) {
		if reminder.Type == DueReminderTypeOvulation {
			t.Fatalf("the pass sent the projected ovulation %s the shift superseded", CalendarDayKey(reminder.EventDate))
		}
	}

	_, controlLogs, _ := outboundConfirmedFixture(t, false)
	var sawOvulation bool
	for _, reminder := range DecideDueReminders(user, enabledWebhookSettings(3), controlLogs, now, time.UTC) {
		if reminder.Type == DueReminderTypeOvulation {
			sawOvulation = true
			if got := CalendarDayKey(reminder.EventDate); got != "2026-03-14" {
				t.Fatalf("control: ovulation reminder for %s, want 2026-03-14", got)
			}
		}
	}
	if !sawOvulation {
		t.Fatal("control: with no shift recorded the ovulation reminder must still be due")
	}
}
