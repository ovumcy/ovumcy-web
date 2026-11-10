package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Raw per-day values stay on owner pages and in the owner's own export; neither
// egress path — the .ics feed or a webhook — carries one. Each egress test feeds
// days that hold every free-text and enumerated day field set to a value no
// egress copy contains, then asserts none of them reaches what leaves.
var rawDayFieldMarkers = []string{
	"raw-note-7f3c",   // Notes
	"raw-factor-7f3c", // CycleFactorKeys
	"36.87",           // BBT
	models.CervicalMucusEggWhite,
	models.SexActivityUnprotected,
	models.PregnancyTestNegative,
	models.FlowHeavy,
}

func withRawDayFields(logs []models.DailyLog) []models.DailyLog {
	bbt := 36.87
	enriched := make([]models.DailyLog, len(logs))
	for index, entry := range logs {
		entry.Notes = "raw-note-7f3c"
		entry.CycleFactorKeys = []string{"raw-factor-7f3c"}
		entry.BBT = &bbt
		entry.CervicalMucus = models.CervicalMucusEggWhite
		entry.SexActivity = models.SexActivityUnprotected
		entry.PregnancyTest = models.PregnancyTestNegative
		entry.Flow = models.FlowHeavy
		entry.Mood = 5
		entry.SymptomIDs = []uint{41, 42}
		enriched[index] = entry
	}
	return enriched
}

func assertNoRawDayField(t *testing.T, where string, body string) {
	t.Helper()
	for _, marker := range rawDayFieldMarkers {
		if strings.Contains(body, marker) {
			t.Fatalf("%s carries the raw day value %q:\n%s", where, marker, body)
		}
	}
}

func TestCalendarFeedCarriesNoRawDayField(t *testing.T) {
	t.Parallel()

	for _, detailed := range []bool{false, true} {
		user := predictableFeedUser(t, "2026-03-02")
		user.CalendarFeedDetailedTitles = detailed
		body := string(buildTestFeedICS(CalendarFeedICSInput{
			User:       user,
			Logs:       withRawDayFields(predictableFeedLogs(t)),
			Now:        mustParseDashboardDay(t, "2026-03-20"),
			Location:   time.UTC,
			Disclaimer: "Predictions are estimates, not medical advice or a method of contraception.",
			Titles:     CalendarFeedTitles{Period: "Period (estimate)", Ovulation: "Ovulation (estimate)"},
		}))

		if !strings.Contains(body, "BEGIN:VEVENT") {
			t.Fatalf("anchor: the enriched days produced no event (detailed=%v), so the sweep would assert nothing:\n%s", detailed, body)
		}
		assertNoRawDayField(t, "the .ics feed", body)
	}
}

func TestWebhookPayloadCarriesNoRawDayField(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	periodOwner := dueRecord(1, "https://a.example/hook", now, 26)
	periodOwner.WebhookNotifyOvulation = true
	ovulationOwner := dueRecord(2, "https://b.example/hook", now, 12)
	ovulationOwner.WebhookNotifyOvulation = true

	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{periodOwner, ovulationOwner}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: withRawDayFields([]models.DailyLog{periodStartLog(1, *periodOwner.LastPeriodStart)}),
		2: withRawDayFields(completedCycleStartLogs(2, *ovulationOwner.LastPeriodStart)),
	}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	if _, err := service.RunOnce(context.Background(), now, time.UTC, false); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	seenKinds := map[string]bool{}
	for _, delivery := range deliverer.deliveries() {
		seenKinds[delivery.payload.Type] = true
		body, err := json.Marshal(delivery.payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		assertNoRawDayField(t, "the webhook JSON envelope", string(body))
		assertNoRawDayField(t, "the webhook ntfy body", delivery.payload.Message+"\n\n"+delivery.payload.Disclaimer)
	}
	for _, kind := range []string{DueReminderTypePeriod, DueReminderTypeOvulation} {
		if !seenKinds[kind] {
			t.Fatalf("anchor: expected a %q delivery from the enriched days, got kinds %v", kind, seenKinds)
		}
	}
}
