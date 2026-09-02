package reminders

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// This file proves the idempotency LAYERING the design mandates: the TRIGGER
// aims at one pass per local day — the timer loop recomputes its next fire
// strictly after the instant that just fired (nextRun), and the once-per-local-
// day marker adds restart safety on top, being read by runCatchUp only — but
// #124's per-reminder watermark is the authoritative backstop. Even if two
// passes run in the same fake day (a restart the marker did not cover, or an
// operator `ovumcy notify` cron running alongside), each reminder ships at most
// once. It drives the REAL services.WebhookNotifyService (not the scheduler's
// stub PassRunner) so the actual claim-before-delivery + decision-suppression
// path is exercised.
//
// Two shapes of "twice" live here and neither implies the other: the passes may
// run one after the other (the second reads what the first wrote), or they may
// OVERLAP (both read before either writes). The serial test covers the first, the
// barrier test the second.

// statefulNotifyRepo is a minimal, watermark-aware NotifyUserRepository holding
// the persisted watermark column of each owner in memory: the claim a send takes
// is reflected back into the record it returns on the next ListAllForNotify, so a
// later pass sees the advanced watermark and the decision suppresses the
// already-sent reminder — exactly as the real DB repository behaves.
//
// The two writers mirror the repository exactly. The claim is the conditional
// UPDATE ... WHERE id = ? AND col IS <the value the pass read> — one row affected
// means this pass owns the send, zero rows means the column moved since the
// snapshot, so another pass owns it. The claim also pins the owner's revocation
// epoch, so a pass whose snapshot predates a settings save, a disable, an
// endpoint change, a lead-window change or a clear-data wipe loses it and
// delivers nothing. The release is the compare-and-set back,
// conditional on the column still holding the anchor this pass wrote.
//
// snapshot, when set, turns ListAllForNotify into a rendezvous: no caller leaves
// with a snapshot until every participant has one. That is the overlap window a
// serial pair of passes can never reach — in production the due-set is computed
// from the record snapshot, so two passes that both list before either writes
// each see an uncovered anchor.
type statefulNotifyRepo struct {
	mu       sync.Mutex
	records  []models.WebhookNotifyRecord
	snapshot *sync.WaitGroup
	// delivered models webhook_last_delivered_at per owner. It is deliberately
	// NOT reflected back into the notify projection: the pass never reads the
	// delivery mark, and a stub that fed it back would hide a projection that
	// started to.
	delivered map[uint]time.Time
}

func (r *statefulNotifyRepo) ListAllForNotify(context.Context) ([]models.WebhookNotifyRecord, error) {
	r.mu.Lock()
	out := make([]models.WebhookNotifyRecord, len(r.records))
	copy(out, r.records)
	r.mu.Unlock()

	if r.snapshot != nil {
		r.snapshot.Done()
		r.snapshot.Wait()
	}
	return out, nil
}

func (r *statefulNotifyRepo) ClaimWebhookWatermark(_ context.Context, userID uint, reminderType string, anchor time.Time, previous *time.Time, configVersion int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	column := r.column(userID, reminderType)
	if column == nil {
		return false, nil
	}
	if !r.epochMatches(userID, configVersion) {
		// The owner revoked the configuration this pass read: it may not send.
		return false, nil
	}
	if !sameWatermark(*column, previous) {
		// The column moved since this pass read it: another pass owns the send.
		return false, nil
	}
	anchorUTC := utcMidnight(anchor)
	*column = &anchorUTC
	return true, nil
}

// sameWatermark reports whether a stored watermark equals the value a pass
// expected to find, NULL included — the stub's spelling of the claim's
// compare-and-set predicate.
func sameWatermark(stored *time.Time, expected *time.Time) bool {
	if stored == nil || expected == nil {
		return stored == nil && expected == nil
	}
	return stored.Equal(*expected)
}

func (r *statefulNotifyRepo) ReleaseWebhookWatermark(_ context.Context, userID uint, reminderType string, anchor time.Time, previous *time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	column := r.column(userID, reminderType)
	if column == nil {
		return nil
	}
	anchorUTC := utcMidnight(anchor)
	if *column == nil || !(*column).Equal(anchorUTC) {
		return nil
	}
	*column = previous
	return nil
}

// MarkWebhookDelivered mirrors the repository's second write: the epoch the
// delivering pass pinned must still be the stored one, and the stamp only moves
// forward. It advances no epoch and touches no watermark, which is what lets the
// overlap test below assert that recording a delivery cannot cost this same pass
// its next claim.
func (r *statefulNotifyRepo) MarkWebhookDelivered(_ context.Context, userID uint, deliveredAt time.Time, configVersion int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.epochMatches(userID, configVersion) {
		return nil
	}
	if r.delivered == nil {
		r.delivered = map[uint]time.Time{}
	}
	if existing, ok := r.delivered[userID]; ok && !deliveredAt.After(existing) {
		return nil
	}
	r.delivered[userID] = deliveredAt
	return nil
}

// epochMatches reports whether the owner's stored revocation epoch is still the
// one the claiming pass's snapshot carried — the stub's spelling of the claim's
// "webhook_config_version = ?" predicate. An unknown owner cannot match, which
// is the fail-closed direction for an egress path.
func (r *statefulNotifyRepo) epochMatches(userID uint, configVersion int) bool {
	for i := range r.records {
		if r.records[i].ID == userID {
			return r.records[i].WebhookConfigVersion == configVersion
		}
	}
	return false
}

// column returns a pointer to the watermark field of one owner's record, or nil
// when the owner or the reminder kind is unknown.
func (r *statefulNotifyRepo) column(userID uint, reminderType string) **time.Time {
	for i := range r.records {
		if r.records[i].ID != userID {
			continue
		}
		switch reminderType {
		case models.WebhookReminderTypePeriod:
			return &r.records[i].WebhookPeriodLastSentCycleStart
		case models.WebhookReminderTypeOvulation:
			return &r.records[i].WebhookOvulationLastSentCycleStart
		}
	}
	return nil
}

// utcMidnight reproduces the repository's canonicalization of a cycle anchor, so
// the stub compares the same values the SQL predicate does.
func utcMidnight(anchor time.Time) time.Time {
	year, month, day := anchor.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
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

// fixedDisclaimer is the notify pass's localized-copy seam, answered with fixed
// English text. These cases are about idempotency, not language, so the entries
// are returned regardless of the language asked for.
type fixedDisclaimer struct{}

func (fixedDisclaimer) Disclaimer(string) string {
	return "Predictions are estimates, not medical advice or a method of contraception."
}

func (fixedDisclaimer) Message(_ string, key string) string {
	switch key {
	case "webhook.reminder.period.title":
		return "Period reminder"
	case "webhook.reminder.period.message":
		return "Estimated next period around %s."
	case "webhook.reminder.ovulation.title":
		return "Ovulation reminder"
	case "webhook.reminder.ovulation.message":
		return "Estimated ovulation around %s."
	default:
		return ""
	}
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
// notify pass is run TWICE within one fake day (simulating a restart the marker
// did not cover, or a concurrent operator cron). The real service's
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

	// Fire 2 in the SAME day: the trigger's once-per-day layer is bypassed here on
	// purpose — this case calls the service directly, so neither the loop's
	// rollover nor the marker is in the path. The watermark, now advanced, must
	// suppress the second send.
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

// TestConcurrentNotifyPassesDeliverOnce is the overlap proof the serial test
// above cannot give. Two passes run at once — the scheduler goroutine and an
// operator `ovumcy notify` process — and both take their record snapshot before
// either writes anything. Both therefore decide the same reminder is due for the
// same (owner, kind, cycle anchor).
//
// Exactly ONE outbound delivery may leave the instance: a reminder derived from
// health data must reach the owner's endpoint once per cycle, and an overlapping
// pass is not a second cycle. The claim taken before delivery is what settles
// which pass owns the send; the loser skips without delivering.
func TestConcurrentNotifyPassesDeliverOnce(t *testing.T) {
	const passes = 2

	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	var snapshot sync.WaitGroup
	snapshot.Add(passes)
	repo := &statefulNotifyRepo{records: []models.WebhookNotifyRecord{record}, snapshot: &snapshot}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: {periodStartLog(1, *record.LastPeriodStart)},
	}}
	deliverer := &countingDeliverer{}
	service := services.NewWebhookNotifyService(repo, logs, echoDecryptor{}, deliverer, fixedDisclaimer{})

	reports := make([]services.NotifyReport, passes)
	errs := make([]error, passes)
	var running sync.WaitGroup
	running.Add(passes)
	for pass := range passes {
		go func(index int) {
			defer running.Done()
			reports[index], errs[index] = service.RunOnce(context.Background(), now, time.UTC, false)
		}(pass)
	}
	running.Wait()

	for index, err := range errs {
		if err != nil {
			t.Fatalf("pass %d: %v", index, err)
		}
	}

	if deliverer.total != 1 {
		t.Fatalf("expected exactly ONE outbound delivery across %d overlapping passes, got %d", passes, deliverer.total)
	}
	if got := deliverer.byURL["https://a.example/hook"]; got != 1 {
		t.Fatalf("owner's endpoint must receive its reminder exactly once, got %d", got)
	}

	sent, skipped, failed := 0, 0, 0
	for _, report := range reports {
		sent += report.Sent
		skipped += report.SkippedIdempotent
		failed += report.Failed
	}
	if sent != 1 {
		t.Fatalf("exactly one pass may account the reminder as sent, aggregate_sent=%d", sent)
	}
	if failed != 0 {
		t.Fatalf("a claim lost to a concurrent pass is a skip, not a failure, aggregate_failed=%d", failed)
	}
	if skipped != passes-1 {
		t.Fatalf("the pass that lost the claim must account it as skipped-idempotent, aggregate_skipped=%d", skipped)
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
