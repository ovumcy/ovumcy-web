package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestBleedingTodaySuppressesFertilityWhenTheInstanceZoneIsADayBehind pins
// WEB-485. The feed and the webhook pass hand the derivation `now` in the
// INSTANCE zone and the owner's zone as location. For an owner east of the
// instance, early in their local morning, the instance is still on yesterday;
// the derivation re-took today from `now` and dropped the owner's own log for
// today, so the WEB-434 bleeding-outside-start suppression missed and the feed
// and webhook announced a fertile window on a day logged as bleeding.
func TestBleedingTodaySuppressesFertilityWhenTheInstanceZoneIsADayBehind(t *testing.T) {
	owner, err := time.LoadLocation("Pacific/Kiritimati")
	if err != nil {
		t.Skipf("tzdata: %v", err)
	}
	user := dayFeedbackParityUser(485)
	spotting := models.DailyLog{Date: mustParseDay(t, "2026-11-02"), IsPeriod: true, CycleStart: true, IsUncertain: true, Flow: models.FlowSpotting}
	logs := web434History(t, spotting)

	for _, tc := range []struct {
		ownerDay string
		cycleDay int
		withheld bool
	}{
		{ownerDay: "2026-10-31", cycleDay: 9},
		{ownerDay: "2026-11-02", cycleDay: 11, withheld: true},
		{ownerDay: "2026-11-03", cycleDay: 12, withheld: true},
	} {
		t.Run(tc.ownerDay, func(t *testing.T) {
			day := mustParseDay(t, tc.ownerDay)
			now := time.Date(day.Year(), day.Month(), day.Day(), 8, 0, 0, 0, owner).UTC()
			if CalendarDayKey(dateOnly(now)) == tc.ownerDay {
				t.Fatalf("fixture: instance day %s must lag the owner's", CalendarDayKey(dateOnly(now)))
			}

			stats := BuildCycleStatsFromLogs(user, logs, now, owner)
			if stats.CurrentCycleDay != tc.cycleDay {
				t.Errorf("cycle day %d, want the owner's %d", stats.CurrentCycleDay, tc.cycleDay)
			}
			if stats.BleedingOutsideCycleStart != tc.withheld {
				t.Errorf("BleedingOutsideCycleStart = %v, want %v", stats.BleedingOutsideCycleStart, tc.withheld)
			}

			feedOvulation := 0
			for _, event := range calendarFeedEvents(CalendarFeedICSInput{User: user, Logs: logs, Now: now, Location: owner}) {
				if event.kind == calendarFeedKindOvulation || event.kind == calendarFeedKindOvulationWindow {
					feedOvulation++
				}
			}
			if tc.withheld && feedOvulation != 0 {
				t.Errorf("the feed sends %d ovulation event(s) on a bleeding day", feedOvulation)
			}
			if !tc.withheld && feedOvulation == 0 {
				t.Error("control: the feed sends no ovulation")
			}

			settings := WebhookReminderSettings{Enabled: true, NotifyOvulation: true, ReminderLeadDays: MaxReminderLeadDays}
			for _, reminder := range DecideDueReminders(user, settings, logs, now, owner) {
				if reminder.Type == DueReminderTypeOvulation && tc.withheld {
					t.Errorf("webhook announces ovulation %s on a bleeding day", CalendarDayKey(reminder.EventDate))
				}
			}
		})
	}
}
