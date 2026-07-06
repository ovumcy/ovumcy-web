package reminders

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// This file proves the idempotency LAYERING the design mandates: the once-per-
// local-day marker stops the TRIGGER from firing more than once per day, but
// #124's per-reminder watermark is the authoritative backstop — even if two
// passes run in the same fake day (marker failed, or an operator `ovumcy notify`
// cron ran alongside), each reminder ships at most once. It drives the REAL
// services.WebhookNotifyService (not the scheduler's stub PassRunner) so the
// actual watermark write-on-success + decision-suppression path is exercised.

// statefulNotifyRepo is a minimal, watermark-aware NotifyUserRepository: a
// successful send's watermark write is reflected back into the record it returns
// on the next ListAllForNotify, so a second pass sees the advanced watermark and
// the decision suppresses the already-sent reminder — exactly as the real DB
// repository behaves across two passes.
type statefulNotifyRepo struct {
	mu      sync.Mutex
	records []models.WebhookNotifyRecord
}

func (r *statefulNotifyRepo) ListAllForNotify(context.Context) ([]models.WebhookNotifyRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]models.WebhookNotifyRecord, len(r.records))
	copy(out, r.records)
	return out, nil
}

func (r *statefulNotifyRepo) UpdateWebhookWatermark(_ context.Context, userID uint, reminderType string, anchor time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	anchorCopy := anchor
	for i := range r.records {
		if r.records[i].ID != userID {
			continue
		}
		switch reminderType {
		case models.WebhookReminderTypePeriod:
			r.records[i].WebhookPeriodLastSentCycleStart = &anchorCopy
		case models.WebhookReminderTypeOvulation:
			r.records[i].WebhookOvulationLastSentCycleStart = &anchorCopy
		}
	}
	return nil
}

// countingDeliverer counts total successful deliveries and per-URL deliveries.
type countingDeliverer struct {
	mu    sync.Mutex
	byURL map[string]int
	total int
}

func (d *countingDeliverer) Deliver(_ context.Context, url string, _ services.WebhookPayload) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.byURL == nil {
		d.byURL = map[string]int{}
	}
	d.byURL[url]++
	d.total++
	return nil
}

// echoDecryptor returns the stored token as the plaintext URL (no real key).
type echoDecryptor struct{}

func (echoDecryptor) DecryptWebhookURL(_ uint, encryptedURL string) (string, error) {
	return encryptedURL, nil
}

type fixedDisclaimer struct{}

func (fixedDisclaimer) Disclaimer(string) string {
	return "These are estimates, not medical advice or a method of contraception."
}

// dueRecord builds a period-due record for a regular 28-day owner whose last
// period started lastPeriodDaysAgo before now (26 puts the next period ~2 days
// out, inside a lead window of 3).
func dueRecord(id uint, urlToken string, now time.Time, lastPeriodDaysAgo int) models.WebhookNotifyRecord {
	last := now.AddDate(0, 0, -lastPeriodDaysAgo)
	last = time.Date(last.Year(), last.Month(), last.Day(), 0, 0, 0, 0, time.UTC)
	return models.WebhookNotifyRecord{
		ID:                     id,
		CycleLength:            28,
		PeriodLength:           5,
		LutealPhase:            14,
		LastPeriodStart:        &last,
		WebhookEnabled:         true,
		WebhookURL:             urlToken,
		WebhookNotifyPeriod:    true,
		WebhookNotifyOvulation: false,
		ReminderLeadDays:       3,
	}
}

type stubLogReader struct {
	byUser map[uint][]models.DailyLog
}

func (s stubLogReader) ListByUser(_ context.Context, userID uint) ([]models.DailyLog, error) {
	return s.byUser[userID], nil
}

// TestIdempotencyLayeringTwoFiresSameDaySendOnce is the layering proof: the
// scheduler fires the notify pass TWICE within one fake day (simulating a marker
// that failed to persist, or a concurrent operator cron). The real service's
// per-reminder watermark must ensure the owner's period reminder ships exactly
// ONCE across both fires — never twice.
func TestIdempotencyLayeringTwoFiresSameDaySendOnce(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	repo := &statefulNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: {periodStartLog(1, *record.LastPeriodStart)},
	}}
	deliverer := &countingDeliverer{}
	service := services.NewWebhookNotifyService(repo, logs, echoDecryptor{}, deliverer, fixedDisclaimer{})

	// Fire 1: the reminder is due and ships; the watermark advances.
	report1, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if report1.Sent != 1 {
		t.Fatalf("first pass should send exactly one reminder, sent=%d", report1.Sent)
	}

	// Fire 2 in the SAME day: the marker did NOT stop this (we bypass it here on
	// purpose). The watermark, now advanced, must suppress the second send.
	report2, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if report2.Sent != 0 {
		t.Fatalf("second same-day pass must send nothing (watermark backstop), sent=%d", report2.Sent)
	}

	if deliverer.total != 1 {
		t.Fatalf("expected exactly ONE outbound delivery across two same-day fires, got %d", deliverer.total)
	}
	if deliverer.byURL["https://a.example/hook"] != 1 {
		t.Fatalf("owner's endpoint must receive its reminder exactly once, got %d", deliverer.byURL["https://a.example/hook"])
	}
}

// periodStartLog builds a single cycle-start period day for an owner, the
// prediction input the decision needs to project the next period.
func periodStartLog(userID uint, day time.Time) models.DailyLog {
	return models.DailyLog{
		UserID:     userID,
		Date:       day,
		IsPeriod:   true,
		CycleStart: true,
	}
}
