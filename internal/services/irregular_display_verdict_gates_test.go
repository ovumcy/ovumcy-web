package services

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The ovulation range has two more gates than the spread that opens it, and the
// webhook and the feed must hold both exactly as the dashboard does: a
// temperature-confirmed ovulation outranks the range, and a cohort whose
// ovulation cannot be placed has no range to send.

func irregularConfirmedFixture(t *testing.T, withShift bool) (*models.User, []models.DailyLog, time.Time) {
	t.Helper()
	user := irregularVerdictUser(true)
	user.TrackBBT = true
	logs := irregularVerdictLogs(irregularRangeOffsets...)
	if withShift {
		start := irregularVerdictBase.AddDate(0, 0, irregularRangeOffsets[len(irregularRangeOffsets)-1])
		for offset := 3; offset <= 8; offset++ {
			day := start.AddDate(0, 0, offset)
			value := 36.20
			logs = append(logs, models.DailyLog{UserID: user.ID, Date: day, BBT: &value})
		}
		for offset := 9; offset <= 11; offset++ {
			day := start.AddDate(0, 0, offset)
			value := 36.50
			logs = append(logs, models.DailyLog{UserID: user.ID, Date: day, BBT: &value})
		}
	}
	return user, logs, irregularVerdictBase.AddDate(0, 0, irregularRangeDayOffset)
}

func TestIrregularConfirmedOvulationWithdrawsTheRangeFromWebhookAndFeed(t *testing.T) {
	// Control: the same history without temperatures sends the range on both.
	user, logs, now := irregularConfirmedFixture(t, false)
	if _, ok := findDueReminder(DecideDueReminders(user, enabledWebhookSettings(MaxReminderLeadDays), logs, now, time.UTC), DueReminderTypeOvulation); !ok {
		t.Fatal("control: no ovulation reminder without a confirmed shift — the fixture proves nothing")
	}
	if feed := renderConfirmedFeed(user, logs, now); !strings.Contains(feed, "ovulation-window") {
		t.Fatal("control: no ovulation-window event in the feed without a confirmed shift")
	}

	user, logs, now = irregularConfirmedFixture(t, true)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if _, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, time.UTC); !ok {
		t.Fatal("fixture: the thermal shift is not confirmed on the read day")
	}
	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if cycleContext.DisplayOvulationUseRange {
		t.Fatal("dashboard: shows an ovulation range beside a confirmed day")
	}

	if _, ok := findDueReminder(DecideDueReminders(user, enabledWebhookSettings(MaxReminderLeadDays), logs, now, time.UTC), DueReminderTypeOvulation); ok {
		t.Error("webhook: sent the ovulation range for a cycle whose ovulation is confirmed")
	}
	if feed := renderConfirmedFeed(user, logs, now); strings.Contains(feed, "ovulation-window") {
		t.Error("feed: published the ovulation range for a cycle whose ovulation is confirmed")
	}
}

func TestIrregularUnplaceableOvulationSendsNoRangeFromTheFeed(t *testing.T) {
	user := irregularVerdictUser(true)
	// Cycles of 12, 14 and 30 days: the median sits below the shortest cycle an
	// ovulation can be seated in, while the longest lifts the range's minimum.
	logs := irregularVerdictLogs(0, 12, 26, 56)
	now := irregularVerdictBase.AddDate(0, 0, 56+5)
	today := DateAtLocation(now, time.UTC)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)

	prediction := DashboardUpcomingPredictions(stats, user, today, DashboardProjectionCycleLength(user, stats))
	if !prediction.OvulationImpossible {
		t.Skipf("fixture: ovulation is placeable for lengths %d..%d", stats.MinCycleLength, stats.MaxCycleLength)
	}
	if !ResolveProjectionRanges(user, stats, prediction.NextPeriodStart, time.UTC).OvulationUseRange {
		t.Fatal("fixture: no ovulation range is resolved, so the feed gate is not what is under test")
	}
	if feed := renderConfirmedFeed(user, logs, now); strings.Contains(feed, "ovulation-window") {
		t.Error("feed: published an ovulation range where the dashboard says it cannot be predicted")
	}
}

func TestPreviewEventDateNamesTheWholeRange(t *testing.T) {
	if got := previewEventDate(WebhookPayload{EventDate: "2026-05-25"}); got != "2026-05-25" {
		t.Errorf("single day = %q", got)
	}
	if got := previewEventDate(WebhookPayload{EventDate: "2026-05-25", EventDateEnd: "2026-06-04"}); got != "2026-05-25..2026-06-04" {
		t.Errorf("range = %q, want first..last", got)
	}
}
