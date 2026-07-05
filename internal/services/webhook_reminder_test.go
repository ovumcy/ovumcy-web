package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// mustParseWebhookReminderDay parses a YYYY-MM-DD calendar day in the given
// location for the webhook-reminder tests. Keeping location explicit lets the
// timezone cases build the same wall-clock day in different zones.
func mustParseWebhookReminderDay(t *testing.T, raw string, location *time.Location) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, location)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}

// regularWebhookUser is the baseline owner for the reminder tests: regular
// (non-irregular, non-unpredictable) cycle so DashboardUpcomingPredictions
// yields single-date estimates, 28-day cycle, 14-day luteal phase.
func regularWebhookUser() *models.User {
	return &models.User{
		CycleLength:  28,
		PeriodLength: 5,
		LutealPhase:  14,
	}
}

// enabledWebhookSettings returns settings with delivery on, both kinds on, the
// given lead window, and no watermarks (nothing sent yet).
func enabledWebhookSettings(leadDays int) WebhookReminderSettings {
	return WebhookReminderSettings{
		Enabled:          true,
		NotifyPeriod:     true,
		NotifyOvulation:  true,
		ReminderLeadDays: leadDays,
	}
}

// findDueReminder returns the reminder of the given type, or false when absent.
func findDueReminder(reminders []DueReminder, reminderType string) (DueReminder, bool) {
	for _, reminder := range reminders {
		if reminder.Type == reminderType {
			return reminder, true
		}
	}
	return DueReminder{}, false
}

// TestReminderWithinWindowBoundaries pins the lead-window predicate directly,
// covering the exact calendar-day boundaries the plan calls out: an event
// exactly leadDays out is due; leadDays+1 is not yet; today (0 days) is due; and
// an already-past date is never due. Driving the predicate directly (rather than
// through the projection, which rolls the next-period date forward on the event
// day and shifts a passed ovulation forward) is what makes the 0-day and
// past-day boundaries deterministically reachable.
func TestReminderWithinWindowBoundaries(t *testing.T) {
	today := mustParseWebhookReminderDay(t, "2026-03-10", time.UTC)
	const leadDays = 3

	cases := []struct {
		name      string
		eventDate string
		zeroDate  bool
		want      bool
	}{
		{name: "today is due at day zero", eventDate: "2026-03-10", want: true},
		{name: "tomorrow is due", eventDate: "2026-03-11", want: true},
		{name: "exactly lead days out is due", eventDate: "2026-03-13", want: true},
		{name: "one day beyond the lead window is not yet due", eventDate: "2026-03-14", want: false},
		{name: "far in the future is not due", eventDate: "2026-04-10", want: false},
		{name: "yesterday (past) is not due", eventDate: "2026-03-09", want: false},
		{name: "zero date (not calculable) is not due", zeroDate: true, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eventDate := time.Time{}
			if !tc.zeroDate {
				eventDate = mustParseWebhookReminderDay(t, tc.eventDate, time.UTC)
			}
			if got := reminderWithinWindow(today, eventDate, leadDays); got != tc.want {
				t.Fatalf("reminderWithinWindow(%s, %s, %d) = %v, want %v",
					today.Format("2006-01-02"), tc.eventDate, leadDays, got, tc.want)
			}
		})
	}
}

// TestReminderWithinWindowZeroLeadOnlyToday locks in that a zero lead window
// ("only on the day itself") admits the event day and nothing else.
func TestReminderWithinWindowZeroLeadOnlyToday(t *testing.T) {
	today := mustParseWebhookReminderDay(t, "2026-03-10", time.UTC)

	if !reminderWithinWindow(today, today, 0) {
		t.Fatalf("expected event today to be due with a zero lead window")
	}
	tomorrow := mustParseWebhookReminderDay(t, "2026-03-11", time.UTC)
	if reminderWithinWindow(today, tomorrow, 0) {
		t.Fatalf("expected event tomorrow to be excluded by a zero lead window")
	}
}

// TestDecideDueRemindersOvulationWindowBoundaries drives the full decision path
// (logs → BuildCycleStatsFromLogs → DashboardUpcomingPredictions) for the
// ovulation reminder across the window boundary. Ovulation is the natural
// vehicle for this because, unlike the next-period date, its predicted date
// stays put while it is still upcoming, so moving "today" walks it cleanly from
// several days out to the day itself. With the period log on 2026-03-01 and a
// 28-day cycle, ovulation lands on 2026-03-14; the period estimate (2026-03-29)
// stays outside the 3-day window in every row, isolating the ovulation branch.
func TestDecideDueRemindersOvulationWindowBoundaries(t *testing.T) {
	const leadDays = 3
	user := regularWebhookUser()
	logs := []models.DailyLog{
		{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
	}

	cases := []struct {
		name       string
		today      string
		wantDue    bool
		wantEvent  string
		wantAnchor string
	}{
		{name: "ovulation four days out is not yet due", today: "2026-03-10", wantDue: false},
		{name: "ovulation exactly lead days out is due", today: "2026-03-11", wantDue: true, wantEvent: "2026-03-14", wantAnchor: "2026-03-01"},
		{name: "ovulation tomorrow is due", today: "2026-03-13", wantDue: true, wantEvent: "2026-03-14", wantAnchor: "2026-03-01"},
		{name: "ovulation today is due", today: "2026-03-14", wantDue: true, wantEvent: "2026-03-14", wantAnchor: "2026-03-01"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := mustParseWebhookReminderDay(t, tc.today, time.UTC)
			reminders := DecideDueReminders(user, enabledWebhookSettings(leadDays), logs, now, time.UTC)

			ovulation, ok := findDueReminder(reminders, DueReminderTypeOvulation)
			if ok != tc.wantDue {
				t.Fatalf("ovulation due = %v, want %v (reminders=%#v)", ok, tc.wantDue, reminders)
			}
			if !tc.wantDue {
				return
			}
			if got := ovulation.EventDate.Format("2006-01-02"); got != tc.wantEvent {
				t.Fatalf("ovulation event date = %s, want %s", got, tc.wantEvent)
			}
			if got := ovulation.CycleAnchor.Format("2006-01-02"); got != tc.wantAnchor {
				t.Fatalf("ovulation cycle anchor = %s, want %s", got, tc.wantAnchor)
			}
			if ovulation.LeadDays != leadDays {
				t.Fatalf("ovulation lead days = %d, want %d", ovulation.LeadDays, leadDays)
			}
			if !ovulation.Estimate {
				t.Fatalf("expected Estimate=true (a predicted date is never fact)")
			}
		})
	}
}

// TestDecideDueRemindersPeriodWindow drives the period reminder through the full
// path. With the period log on 2026-03-01 and a 28-day cycle, the next period is
// predicted for 2026-03-29; walking "today" toward it exercises the window. The
// anchor is the next-period date itself (the start of the upcoming cycle).
func TestDecideDueRemindersPeriodWindow(t *testing.T) {
	const leadDays = 3
	user := regularWebhookUser()
	logs := []models.DailyLog{
		{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
	}

	cases := []struct {
		name      string
		today     string
		wantDue   bool
		wantEvent string
	}{
		{name: "period four days out is not yet due", today: "2026-03-25", wantDue: false},
		{name: "period exactly lead days out is due", today: "2026-03-26", wantDue: true, wantEvent: "2026-03-29"},
		{name: "period tomorrow is due", today: "2026-03-28", wantDue: true, wantEvent: "2026-03-29"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			now := mustParseWebhookReminderDay(t, tc.today, time.UTC)
			reminders := DecideDueReminders(user, enabledWebhookSettings(leadDays), logs, now, time.UTC)

			period, ok := findDueReminder(reminders, DueReminderTypePeriod)
			if ok != tc.wantDue {
				t.Fatalf("period due = %v, want %v (reminders=%#v)", ok, tc.wantDue, reminders)
			}
			if !tc.wantDue {
				return
			}
			if got := period.EventDate.Format("2006-01-02"); got != tc.wantEvent {
				t.Fatalf("period event date = %s, want %s", got, tc.wantEvent)
			}
			if got := period.CycleAnchor.Format("2006-01-02"); got != tc.wantEvent {
				t.Fatalf("period cycle anchor = %s, want %s (anchor is the next cycle start)", got, tc.wantEvent)
			}
			if !period.Estimate {
				t.Fatalf("expected Estimate=true")
			}
		})
	}
}

// TestDecideDueRemindersIdempotencyByWatermark proves the "at most once per
// cycle" gate: when the incoming per-kind watermark already equals the event's
// cycle anchor, that reminder is skipped; a watermark pointing at a different
// (earlier) cycle does not suppress it. Uses today=2026-03-28 with a 14-day lead
// so BOTH the imminent period (event 2026-03-29) and the next cycle's ovulation
// (event 2026-04-11) fall inside the window and can be independently gated. Both
// events belong to the cycle starting 2026-03-29, so they share that anchor
// date — but each is keyed by its own watermark, so gating stays per-kind.
func TestDecideDueRemindersIdempotencyByWatermark(t *testing.T) {
	const leadDays = 14
	user := regularWebhookUser()
	logs := []models.DailyLog{
		{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
	}
	now := mustParseWebhookReminderDay(t, "2026-03-28", time.UTC)

	// Baseline (no watermarks): both kinds fire. Capture their anchors.
	base := DecideDueReminders(user, enabledWebhookSettings(leadDays), logs, now, time.UTC)
	basePeriod, okPeriod := findDueReminder(base, DueReminderTypePeriod)
	baseOvulation, okOvulation := findDueReminder(base, DueReminderTypeOvulation)
	if !okPeriod || !okOvulation {
		t.Fatalf("expected both reminders in the baseline, got %#v", base)
	}

	t.Run("matching period watermark skips period only", func(t *testing.T) {
		anchor := basePeriod.CycleAnchor
		settings := enabledWebhookSettings(leadDays)
		settings.PeriodWatermark = &anchor
		reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
		if _, ok := findDueReminder(reminders, DueReminderTypePeriod); ok {
			t.Fatalf("expected period suppressed by matching watermark, got %#v", reminders)
		}
		if _, ok := findDueReminder(reminders, DueReminderTypeOvulation); !ok {
			t.Fatalf("expected ovulation still due (its watermark is unset), got %#v", reminders)
		}
	})

	t.Run("matching ovulation watermark skips ovulation only", func(t *testing.T) {
		anchor := baseOvulation.CycleAnchor
		settings := enabledWebhookSettings(leadDays)
		settings.OvulationWatermark = &anchor
		reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
		if _, ok := findDueReminder(reminders, DueReminderTypeOvulation); ok {
			t.Fatalf("expected ovulation suppressed by matching watermark, got %#v", reminders)
		}
		if _, ok := findDueReminder(reminders, DueReminderTypePeriod); !ok {
			t.Fatalf("expected period still due (its watermark is unset), got %#v", reminders)
		}
	})

	t.Run("stale watermark from an earlier cycle fires exactly once", func(t *testing.T) {
		// A watermark from the PREVIOUS cycle (one cycle length earlier) must
		// not suppress this cycle's reminder — the new cycle is a distinct
		// anchor.
		stalePeriod := basePeriod.CycleAnchor.AddDate(0, 0, -user.CycleLength)
		staleOvulation := baseOvulation.CycleAnchor.AddDate(0, 0, -user.CycleLength)
		settings := enabledWebhookSettings(leadDays)
		settings.PeriodWatermark = &stalePeriod
		settings.OvulationWatermark = &staleOvulation
		reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
		if _, ok := findDueReminder(reminders, DueReminderTypePeriod); !ok {
			t.Fatalf("expected period to fire past a stale watermark, got %#v", reminders)
		}
		if _, ok := findDueReminder(reminders, DueReminderTypeOvulation); !ok {
			t.Fatalf("expected ovulation to fire past a stale watermark, got %#v", reminders)
		}
	})

	t.Run("both matching watermarks skip both", func(t *testing.T) {
		periodAnchor := basePeriod.CycleAnchor
		ovulationAnchor := baseOvulation.CycleAnchor
		settings := enabledWebhookSettings(leadDays)
		settings.PeriodWatermark = &periodAnchor
		settings.OvulationWatermark = &ovulationAnchor
		reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
		if len(reminders) != 0 {
			t.Fatalf("expected no reminders when both watermarks match, got %#v", reminders)
		}
	})
}

// TestDecideDueRemindersSuppression covers the medical-safety gate: whenever the
// app itself suppresses in-app predictions, the decision emits nothing — never a
// date the app refuses to show. Both suppression triggers are exercised:
// unpredictable-cycle mode (DashboardPredictionDisabled) and a positive
// pregnancy test with no later cycle start (stats.PregnancyPaused).
func TestDecideDueRemindersSuppression(t *testing.T) {
	const leadDays = 20
	now := mustParseWebhookReminderDay(t, "2026-03-26", time.UTC)
	baseLogs := []models.DailyLog{
		{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
	}

	t.Run("unpredictable cycle emits nothing", func(t *testing.T) {
		user := regularWebhookUser()
		user.UnpredictableCycle = true
		reminders := DecideDueReminders(user, enabledWebhookSettings(leadDays), baseLogs, now, time.UTC)
		if len(reminders) != 0 {
			t.Fatalf("expected no reminders for unpredictable-cycle mode, got %#v", reminders)
		}
	})

	t.Run("pregnancy paused emits nothing", func(t *testing.T) {
		user := regularWebhookUser()
		logs := []models.DailyLog{
			{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
			// Positive test after the last cycle start pauses predictions.
			{Date: mustParseWebhookReminderDay(t, "2026-03-20", time.UTC), PregnancyTest: models.PregnancyTestPositive},
		}
		reminders := DecideDueReminders(user, enabledWebhookSettings(leadDays), logs, now, time.UTC)
		if len(reminders) != 0 {
			t.Fatalf("expected no reminders while pregnancy-paused, got %#v", reminders)
		}
	})
}

// TestDecideDueRemindersToggles covers the enable switches: the master
// webhook-enabled flag off suppresses everything, and each per-kind flag off
// omits exactly that kind while leaving the other in place. today=2026-03-28
// with a 14-day lead puts both kinds in the window so each toggle can be
// isolated.
func TestDecideDueRemindersToggles(t *testing.T) {
	const leadDays = 14
	user := regularWebhookUser()
	logs := []models.DailyLog{
		{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
	}
	now := mustParseWebhookReminderDay(t, "2026-03-28", time.UTC)

	t.Run("webhook disabled emits nothing", func(t *testing.T) {
		settings := enabledWebhookSettings(leadDays)
		settings.Enabled = false
		reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
		if len(reminders) != 0 {
			t.Fatalf("expected no reminders when webhook delivery is disabled, got %#v", reminders)
		}
	})

	t.Run("period notifications off omits period only", func(t *testing.T) {
		settings := enabledWebhookSettings(leadDays)
		settings.NotifyPeriod = false
		reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
		if _, ok := findDueReminder(reminders, DueReminderTypePeriod); ok {
			t.Fatalf("expected no period reminder when NotifyPeriod is off, got %#v", reminders)
		}
		if _, ok := findDueReminder(reminders, DueReminderTypeOvulation); !ok {
			t.Fatalf("expected ovulation reminder to remain, got %#v", reminders)
		}
	})

	t.Run("ovulation notifications off omits ovulation only", func(t *testing.T) {
		settings := enabledWebhookSettings(leadDays)
		settings.NotifyOvulation = false
		reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
		if _, ok := findDueReminder(reminders, DueReminderTypeOvulation); ok {
			t.Fatalf("expected no ovulation reminder when NotifyOvulation is off, got %#v", reminders)
		}
		if _, ok := findDueReminder(reminders, DueReminderTypePeriod); !ok {
			t.Fatalf("expected period reminder to remain, got %#v", reminders)
		}
	})
}

// TestDecideDueRemindersTimezoneResolvesOnOwnerLocalDay proves that "due" is
// evaluated on the OWNER-LOCAL calendar day, not on UTC. The same instant is
// interpreted in two zones: at 2026-03-14T02:00Z the local day is 2026-03-14 in
// Tokyo (UTC+9) but still 2026-03-13 in Los Angeles (UTC-7). The predicted
// ovulation is 2026-03-14, so with a 1-day lead window it is "today" (due) in
// Tokyo and "tomorrow" (also due, but one day out) in Los Angeles — and with a
// 0-day window it is due in Tokyo but NOT yet in Los Angeles. This is exactly
// the tz-sensitivity the plan requires (an event that is "today" in one location
// and "tomorrow" in another).
func TestDecideDueRemindersTimezoneResolvesOnOwnerLocalDay(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}
	losAngeles, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("load America/Los_Angeles: %v", err)
	}

	user := regularWebhookUser()
	// Period log built as a plain calendar day (UTC-midnight), matching how
	// stored date-only values persist.
	logs := []models.DailyLog{
		{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
	}
	// 2026-03-14 02:00 UTC → 03-14 11:00 in Tokyo, 03-13 in Los Angeles.
	nowInstant := time.Date(2026, time.March, 14, 2, 0, 0, 0, time.UTC)

	t.Run("zero lead: due today in Tokyo, not yet in Los Angeles", func(t *testing.T) {
		settings := enabledWebhookSettings(0)

		tokyoReminders := DecideDueReminders(user, settings, logs, nowInstant, tokyo)
		tokyoOvulation, ok := findDueReminder(tokyoReminders, DueReminderTypeOvulation)
		if !ok {
			t.Fatalf("expected ovulation due today in Tokyo, got %#v", tokyoReminders)
		}
		if got := tokyoOvulation.EventDate.Format("2006-01-02"); got != "2026-03-14" {
			t.Fatalf("Tokyo ovulation event date = %s, want 2026-03-14", got)
		}

		laReminders := DecideDueReminders(user, settings, logs, nowInstant, losAngeles)
		if _, ok := findDueReminder(laReminders, DueReminderTypeOvulation); ok {
			t.Fatalf("expected ovulation NOT yet due in Los Angeles with a zero lead window, got %#v", laReminders)
		}
	})

	t.Run("one-day lead: due in both, but a day nearer in Tokyo", func(t *testing.T) {
		settings := enabledWebhookSettings(1)

		tokyoReminders := DecideDueReminders(user, settings, logs, nowInstant, tokyo)
		if _, ok := findDueReminder(tokyoReminders, DueReminderTypeOvulation); !ok {
			t.Fatalf("expected ovulation due in Tokyo, got %#v", tokyoReminders)
		}

		laReminders := DecideDueReminders(user, settings, logs, nowInstant, losAngeles)
		laOvulation, ok := findDueReminder(laReminders, DueReminderTypeOvulation)
		if !ok {
			t.Fatalf("expected ovulation due (tomorrow) in Los Angeles with a one-day lead, got %#v", laReminders)
		}
		if got := laOvulation.EventDate.Format("2006-01-02"); got != "2026-03-14" {
			t.Fatalf("Los Angeles ovulation event date = %s, want 2026-03-14", got)
		}
	})
}

// TestWebhookReminderSettingsFromNotifyRecord verifies the persistence
// read-projection adapter copies exactly the decision fields (and not the
// ciphertext URL or prediction inputs) so the eventual batch caller can hand a
// WebhookNotifyRecord straight into the decision.
func TestWebhookReminderSettingsFromNotifyRecord(t *testing.T) {
	periodWatermark := mustParseWebhookReminderDay(t, "2026-02-01", time.UTC)
	ovulationWatermark := mustParseWebhookReminderDay(t, "2026-02-15", time.UTC)
	record := models.WebhookNotifyRecord{
		WebhookEnabled:                     true,
		WebhookURL:                         "ciphertext-should-be-ignored",
		WebhookNotifyPeriod:                true,
		WebhookNotifyOvulation:             false,
		ReminderLeadDays:                   5,
		WebhookPeriodLastSentCycleStart:    &periodWatermark,
		WebhookOvulationLastSentCycleStart: &ovulationWatermark,
	}

	settings := WebhookReminderSettingsFromNotifyRecord(record)
	if !settings.Enabled || !settings.NotifyPeriod || settings.NotifyOvulation {
		t.Fatalf("flags not copied: %#v", settings)
	}
	if settings.ReminderLeadDays != 5 {
		t.Fatalf("lead days = %d, want 5", settings.ReminderLeadDays)
	}
	if settings.PeriodWatermark == nil || !settings.PeriodWatermark.Equal(periodWatermark) {
		t.Fatalf("period watermark not copied: %#v", settings.PeriodWatermark)
	}
	if settings.OvulationWatermark == nil || !settings.OvulationWatermark.Equal(ovulationWatermark) {
		t.Fatalf("ovulation watermark not copied: %#v", settings.OvulationWatermark)
	}
}

// TestDecideDueRemindersOutOfRangeLeadIsClamped confirms an out-of-range stored
// lead value cannot widen the window past MaxReminderLeadDays: a 999-day lead is
// clamped to the max, so an ovulation ~16 days out (beyond the 14-day cap for
// this fixture) is still not due, and the emitted LeadDays reflects the clamp.
func TestDecideDueRemindersOutOfRangeLeadIsClamped(t *testing.T) {
	user := regularWebhookUser()
	logs := []models.DailyLog{
		{Date: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC), IsPeriod: true, CycleStart: true},
	}
	// today 2026-03-24: ovulation shifts to 2026-04-11 (18 days out), period
	// 2026-03-29 (5 days out). With lead clamped to 14, ovulation is out of the
	// window; period is in it and carries the clamped lead.
	now := mustParseWebhookReminderDay(t, "2026-03-24", time.UTC)
	settings := enabledWebhookSettings(999)

	reminders := DecideDueReminders(user, settings, logs, now, time.UTC)
	if _, ok := findDueReminder(reminders, DueReminderTypeOvulation); ok {
		t.Fatalf("expected ovulation excluded once the lead is clamped to the max, got %#v", reminders)
	}
	period, ok := findDueReminder(reminders, DueReminderTypePeriod)
	if !ok {
		t.Fatalf("expected period reminder within the clamped window, got %#v", reminders)
	}
	if period.LeadDays != MaxReminderLeadDays {
		t.Fatalf("period lead days = %d, want clamped %d", period.LeadDays, MaxReminderLeadDays)
	}
}

// TestDecideDueRemindersNoDataEmitsNothing confirms an owner with no cycle data
// (no logs, no baseline) produces no reminders: there is no next-period or
// ovulation date to be within any window.
func TestDecideDueRemindersNoDataEmitsNothing(t *testing.T) {
	user := regularWebhookUser()
	now := mustParseWebhookReminderDay(t, "2026-03-10", time.UTC)

	reminders := DecideDueReminders(user, enabledWebhookSettings(3), nil, now, time.UTC)
	if len(reminders) != 0 {
		t.Fatalf("expected no reminders without cycle data, got %#v", reminders)
	}
}

// TestOvulationCycleAnchor exercises the anchor derivation directly, including
// the guards the public decision path cannot reach with consistent stats (a
// zero last-period-start / non-positive cycle length), plus the same-cycle and
// forward-shifted cases. It mirrors DashboardUpcomingPredictions' own
// projection: with a last period start of 2026-03-01 (28-day cycle, 14-day
// luteal), ovulation is 2026-03-14 in the current cycle; once "today" is past
// that ovulation, the anchor advances one cycle to 2026-03-29.
func TestOvulationCycleAnchor(t *testing.T) {
	baseStats := CycleStats{
		LastPeriodStart: mustParseWebhookReminderDay(t, "2026-03-01", time.UTC),
		LutealPhase:     14,
	}

	t.Run("zero last period start yields no anchor", func(t *testing.T) {
		got := ovulationCycleAnchor(CycleStats{LutealPhase: 14}, mustParseWebhookReminderDay(t, "2026-03-10", time.UTC), 28)
		if !got.IsZero() {
			t.Fatalf("expected zero anchor for zero last period start, got %s", got.Format("2006-01-02"))
		}
	})

	t.Run("non-positive cycle length yields no anchor", func(t *testing.T) {
		got := ovulationCycleAnchor(baseStats, mustParseWebhookReminderDay(t, "2026-03-10", time.UTC), 0)
		if !got.IsZero() {
			t.Fatalf("expected zero anchor for zero cycle length, got %s", got.Format("2006-01-02"))
		}
	})

	t.Run("current cycle anchors at the last period start", func(t *testing.T) {
		got := ovulationCycleAnchor(baseStats, mustParseWebhookReminderDay(t, "2026-03-10", time.UTC), 28)
		if want := "2026-03-01"; got.Format("2006-01-02") != want {
			t.Fatalf("expected anchor %s, got %s", want, got.Format("2006-01-02"))
		}
	})

	t.Run("past ovulation shifts the anchor forward one cycle", func(t *testing.T) {
		// today after the 2026-03-14 ovulation => shift to the next cycle.
		got := ovulationCycleAnchor(baseStats, mustParseWebhookReminderDay(t, "2026-03-20", time.UTC), 28)
		if want := "2026-03-29"; got.Format("2006-01-02") != want {
			t.Fatalf("expected shifted anchor %s, got %s", want, got.Format("2006-01-02"))
		}
	})
}
