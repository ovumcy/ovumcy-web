package services

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// --- stubs ------------------------------------------------------------------

// capturedDelivery records one (url, payload) pair the notify pass attempted.
type capturedDelivery struct {
	url     string
	payload WebhookPayload
}

// stubDeliverer captures every delivery and can be told to fail for specific
// URLs, so a test can prove which owner's body went to which owner's URL and
// that a failure leaves the watermark unadvanced.
type stubDeliverer struct {
	mu        sync.Mutex
	captured  []capturedDelivery
	failURLs  map[string]bool
	failEvery bool
}

func (stub *stubDeliverer) Deliver(_ context.Context, decryptedURL string, payload WebhookPayload) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.captured = append(stub.captured, capturedDelivery{url: decryptedURL, payload: payload})
	if stub.failEvery || stub.failURLs[decryptedURL] {
		return errors.New("stub delivery failure")
	}
	return nil
}

func (stub *stubDeliverer) deliveries() []capturedDelivery {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	out := make([]capturedDelivery, len(stub.captured))
	copy(out, stub.captured)
	return out
}

// stubDecryptor maps a ciphertext token to a plaintext URL deterministically,
// so tests never need a real SECRET_KEY. The stored WebhookURL in each record is
// the plaintext-of-record token; here we simply echo it as the "decrypted" URL.
type stubDecryptor struct {
	failFor map[uint]bool
}

func (stub stubDecryptor) DecryptWebhookURL(userID uint, encryptedURL string) (string, error) {
	if stub.failFor[userID] {
		return "", errors.New("stub decrypt failure")
	}
	return encryptedURL, nil
}

// stubDisclaimer returns a fixed disclaimer, standing in for the i18n adapter.
type stubDisclaimer struct{ text string }

func (stub stubDisclaimer) Disclaimer(string) string { return stub.text }

// watermarkWrite records one watermark advance.
type watermarkWrite struct {
	userID       uint
	reminderType string
	anchor       time.Time
}

// stubNotifyRepo serves a fixed record set and records every watermark write, so
// a test can assert watermarks advance ONLY on success.
type stubNotifyRepo struct {
	records    []models.WebhookNotifyRecord
	listErr    error
	mu         sync.Mutex
	watermarks []watermarkWrite
}

func (stub *stubNotifyRepo) ListAllForNotify(context.Context) ([]models.WebhookNotifyRecord, error) {
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	return stub.records, nil
}

func (stub *stubNotifyRepo) UpdateWebhookWatermark(_ context.Context, userID uint, reminderType string, anchor time.Time) error {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	stub.watermarks = append(stub.watermarks, watermarkWrite{userID: userID, reminderType: reminderType, anchor: anchor})
	return nil
}

func (stub *stubNotifyRepo) writes() []watermarkWrite {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	out := make([]watermarkWrite, len(stub.watermarks))
	copy(out, stub.watermarks)
	return out
}

// stubLogReader serves per-user logs from a map.
type stubLogReader struct {
	byUser map[uint][]models.DailyLog
}

func (stub stubLogReader) ListByUser(_ context.Context, userID uint) ([]models.DailyLog, error) {
	return stub.byUser[userID], nil
}

// --- helpers ----------------------------------------------------------------

// periodStartLog builds a single cycle-start period day for an owner, which the
// prediction path needs to project the next period.
func periodStartLog(userID uint, day time.Time) models.DailyLog {
	return models.DailyLog{
		UserID:     userID,
		Date:       day,
		IsPeriod:   true,
		CycleStart: true,
	}
}

// dueRecord returns a notify record for a regular 28-day owner whose last period
// started lastPeriodDaysAgo before now, with webhook delivery on and the given
// ciphertext-of-record URL token. With a 28-day cycle and lastPeriodDaysAgo=26,
// the next period is ~2 days out — inside a lead window of 3.
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
		WebhookNotifyOvulation: false, // keep to a single, deterministic period reminder
		ReminderLeadDays:       3,
	}
}

func newTestNotifyService(repo *stubNotifyRepo, logs stubLogReader, decryptor stubDecryptor, deliverer WebhookDeliverer) *WebhookNotifyService {
	return NewWebhookNotifyService(repo, logs, decryptor, deliverer, stubDisclaimer{text: "These are estimates, not medical advice or a method of contraception."})
}

// --- tests ------------------------------------------------------------------

// TestNotifyCrossOwnerIsolation is THE headline security test: in a two-owner
// batch, owner A's reminder body is POSTed ONLY to A's URL and B's only to B's;
// neither owner's payload ever reaches the other's URL.
func TestNotifyCrossOwnerIsolation(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)

	recordA := dueRecord(1, "https://ntfy.a.example/topicA", now, 26)
	recordB := dueRecord(2, "https://gotify.b.example/topicB", now, 26)

	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{recordA, recordB}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: {periodStartLog(1, *recordA.LastPeriodStart)},
		2: {periodStartLog(2, *recordB.LastPeriodStart)},
	}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Sent != 2 {
		t.Fatalf("expected 2 reminders sent (one per owner), got %d (due=%d failed=%d)", report.Sent, report.Due, report.Failed)
	}

	byURL := map[string][]capturedDelivery{}
	for _, delivery := range deliverer.deliveries() {
		byURL[delivery.url] = append(byURL[delivery.url], delivery)
	}

	// Each owner's URL received exactly one delivery, and no owner's URL received
	// the other's. We tag isolation by the event date embedded in the payload —
	// both owners share a date here, so instead assert the URL→count mapping and
	// that A's URL never appears alongside B's topic and vice versa.
	if len(byURL["https://ntfy.a.example/topicA"]) != 1 {
		t.Fatalf("owner A URL should receive exactly one delivery, got %d", len(byURL["https://ntfy.a.example/topicA"]))
	}
	if len(byURL["https://gotify.b.example/topicB"]) != 1 {
		t.Fatalf("owner B URL should receive exactly one delivery, got %d", len(byURL["https://gotify.b.example/topicB"]))
	}
	// No delivery went to any URL other than the two owner URLs.
	for target := range byURL {
		if target != "https://ntfy.a.example/topicA" && target != "https://gotify.b.example/topicB" {
			t.Fatalf("delivery went to an unexpected URL: %q", target)
		}
	}
}

// TestNotifyCrossOwnerHealthDataStaysScoped strengthens isolation: owner A and B
// have DIFFERENT predicted dates, and we assert A's date only ever appears in a
// request to A's URL (never in a request to B's URL).
func TestNotifyCrossOwnerHealthDataStaysScoped(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)

	// A: last period 26 days ago → next period ~2 days out (2026-03-14-ish).
	recordA := dueRecord(1, "https://a.example/hook", now, 26)
	// B: last period 27 days ago → next period ~1 day out (a different date).
	recordB := dueRecord(2, "https://b.example/hook", now, 27)

	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{recordA, recordB}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: {periodStartLog(1, *recordA.LastPeriodStart)},
		2: {periodStartLog(2, *recordB.LastPeriodStart)},
	}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	if _, err := service.RunOnce(context.Background(), now, time.UTC, false); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	var dateA, dateB string
	for _, delivery := range deliverer.deliveries() {
		switch delivery.url {
		case "https://a.example/hook":
			dateA = delivery.payload.EventDate
		case "https://b.example/hook":
			dateB = delivery.payload.EventDate
		default:
			t.Fatalf("unexpected delivery URL %q", delivery.url)
		}
	}
	if dateA == "" || dateB == "" {
		t.Fatalf("both owners should have been delivered (A=%q B=%q)", dateA, dateB)
	}
	if dateA == dateB {
		t.Fatalf("test setup expected distinct predicted dates, both were %q", dateA)
	}
	// A's date must never have appeared in B's request and vice versa.
	for _, delivery := range deliverer.deliveries() {
		if delivery.url == "https://b.example/hook" && delivery.payload.EventDate == dateA {
			t.Fatal("cross-owner leak: owner A's predicted date reached owner B's URL")
		}
		if delivery.url == "https://a.example/hook" && delivery.payload.EventDate == dateB {
			t.Fatal("cross-owner leak: owner B's predicted date reached owner A's URL")
		}
	}
}

// TestNotifyDisclaimerPresentInEveryPayload proves the mandatory medical-safety
// disclaimer rides in every delivered payload.
func TestNotifyDisclaimerPresentInEveryPayload(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	if _, err := service.RunOnce(context.Background(), now, time.UTC, false); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	deliveries := deliverer.deliveries()
	if len(deliveries) == 0 {
		t.Fatal("expected at least one delivery")
	}
	for _, delivery := range deliveries {
		if !strings.Contains(delivery.payload.Disclaimer, "not medical advice or a method of contraception") {
			t.Fatalf("payload missing disclaimer: %q", delivery.payload.Disclaimer)
		}
	}
}

// TestNotifyWatermarkAdvancesOnlyOnSuccess proves the write-on-success rule: a
// successful send advances the watermark; a failed send leaves it unwritten so a
// later pass retries.
func TestNotifyWatermarkAdvancesOnlyOnSuccess(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	recordOK := dueRecord(1, "https://ok.example/hook", now, 26)
	recordFail := dueRecord(2, "https://fail.example/hook", now, 26)

	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{recordOK, recordFail}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: {periodStartLog(1, *recordOK.LastPeriodStart)},
		2: {periodStartLog(2, *recordFail.LastPeriodStart)},
	}}
	deliverer := &stubDeliverer{failURLs: map[string]bool{"https://fail.example/hook": true}}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Sent != 1 || report.Failed != 1 {
		t.Fatalf("expected sent=1 failed=1, got sent=%d failed=%d", report.Sent, report.Failed)
	}

	writes := repo.writes()
	if len(writes) != 1 {
		t.Fatalf("expected exactly one watermark write (the successful owner), got %d", len(writes))
	}
	if writes[0].userID != 1 {
		t.Fatalf("watermark should have advanced only for owner 1, got owner %d", writes[0].userID)
	}
	if writes[0].reminderType != DueReminderTypePeriod {
		t.Fatalf("expected period watermark, got %q", writes[0].reminderType)
	}
	// The failed owner must be flagged for observability.
	if len(report.OwnerIDsFailed) != 1 || report.OwnerIDsFailed[0] != 2 {
		t.Fatalf("expected owner 2 flagged as failed, got %v", report.OwnerIDsFailed)
	}
}

// TestNotifyIdempotentSecondPassSkips proves idempotency forward: once a reminder
// is sent and its watermark set, a second pass with that watermark sends nothing.
func TestNotifyIdempotentSecondPassSkips(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}

	// First pass: sends once, records the watermark it wrote.
	repo1 := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	deliverer1 := &stubDeliverer{}
	service1 := newTestNotifyService(repo1, logs, stubDecryptor{}, deliverer1)
	report1, err := service1.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if report1.Sent != 1 {
		t.Fatalf("first pass expected 1 sent, got %d", report1.Sent)
	}
	writes := repo1.writes()
	if len(writes) != 1 {
		t.Fatalf("first pass expected 1 watermark write, got %d", len(writes))
	}
	sentAnchor := writes[0].anchor

	// Second pass: feed the SAME record but with the period watermark now set to
	// the anchor the first pass wrote → the decision must skip it.
	record2 := record
	record2.WebhookPeriodLastSentCycleStart = &sentAnchor
	repo2 := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record2}}
	deliverer2 := &stubDeliverer{}
	service2 := newTestNotifyService(repo2, logs, stubDecryptor{}, deliverer2)
	report2, err := service2.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if report2.Sent != 0 {
		t.Fatalf("second pass must send nothing (idempotent), sent=%d", report2.Sent)
	}
	if len(deliverer2.deliveries()) != 0 {
		t.Fatal("second pass made an outbound delivery despite the watermark")
	}
	if report2.SkippedIdempotent != 1 {
		t.Fatalf("second pass should report 1 skipped-idempotent, got %d", report2.SkippedIdempotent)
	}
}

// TestNotifyRetriesAfterFailure proves idempotency the other direction: a failed
// delivery does not advance the watermark, so a subsequent pass retries it.
func TestNotifyRetriesAfterFailure(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}

	// First pass fails delivery → no watermark.
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	failing := &stubDeliverer{failEvery: true}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, failing)
	report1, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if report1.Failed != 1 || report1.Sent != 0 {
		t.Fatalf("first pass expected failed=1 sent=0, got failed=%d sent=%d", report1.Failed, report1.Sent)
	}
	if len(repo.writes()) != 0 {
		t.Fatal("failed delivery must NOT advance the watermark")
	}

	// Second pass with the SAME (unadvanced) record succeeds → retried & sent.
	repo2 := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	ok := &stubDeliverer{}
	service2 := newTestNotifyService(repo2, logs, stubDecryptor{}, ok)
	report2, err := service2.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if report2.Sent != 1 {
		t.Fatalf("retry pass expected sent=1, got %d", report2.Sent)
	}
}

// TestNotifyDryRunMakesNoRequestOrWatermark proves --dry-run computes due
// reminders but performs no delivery and writes no watermark.
func TestNotifyDryRunMakesNoRequestOrWatermark(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, true)
	if err != nil {
		t.Fatalf("RunOnce dry: %v", err)
	}
	if report.Due == 0 {
		t.Fatal("dry run should still compute that a reminder is due")
	}
	if report.Sent != 0 {
		t.Fatalf("dry run must not send, sent=%d", report.Sent)
	}
	if len(deliverer.deliveries()) != 0 {
		t.Fatal("dry run made an outbound delivery")
	}
	if len(repo.writes()) != 0 {
		t.Fatal("dry run wrote a watermark")
	}
	if !report.DryRun {
		t.Fatal("report should record DryRun=true")
	}
	// The dry-run preview must describe what would be sent with the destination
	// HOST only — never the full URL or its path/token.
	if len(report.DryRunPreview) != report.Due {
		t.Fatalf("expected one preview line per due reminder, got %d preview vs %d due", len(report.DryRunPreview), report.Due)
	}
	for _, line := range report.DryRunPreview {
		if line.Host != "a.example" {
			t.Fatalf("preview should carry host-only, got %q", line.Host)
		}
		if strings.Contains(line.Host, "/") || strings.Contains(line.Host, "hook") {
			t.Fatalf("preview host leaked path/URL: %q", line.Host)
		}
		if line.Type == "" || line.EventDate == "" {
			t.Fatalf("preview line missing type/date: %+v", line)
		}
	}
}

// TestNotifyDecryptFailureSkipsOwner proves a decrypt failure (e.g. after a
// SECRET_KEY rotation) fails safe: that owner is skipped, others still deliver,
// and the pass does not error.
func TestNotifyDecryptFailureSkipsOwner(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	badOwner := dueRecord(1, "ciphertext-1", now, 26)
	goodOwner := dueRecord(2, "https://good.example/hook", now, 26)
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{badOwner, goodOwner}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: {periodStartLog(1, *badOwner.LastPeriodStart)},
		2: {periodStartLog(2, *goodOwner.LastPeriodStart)},
	}}
	deliverer := &stubDeliverer{}
	decryptor := stubDecryptor{failFor: map[uint]bool{1: true}}
	service := newTestNotifyService(repo, logs, decryptor, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce should not error on a per-owner decrypt failure: %v", err)
	}
	if report.OwnersScanned != 2 {
		t.Fatalf("expected 2 owners scanned, got %d", report.OwnersScanned)
	}
	if report.Sent != 1 {
		t.Fatalf("expected the good owner to still receive its reminder, sent=%d", report.Sent)
	}
	for _, delivery := range deliverer.deliveries() {
		if strings.HasPrefix(delivery.url, "ciphertext") {
			t.Fatalf("delivered to a still-encrypted URL: %q", delivery.url)
		}
	}
}

// TestNotifyListErrorIsPassLevelFailure proves a failure to list owners is a
// pass-level error (the CLI exits non-zero), unlike a per-owner failure.
func TestNotifyListErrorIsPassLevelFailure(t *testing.T) {
	repo := &stubNotifyRepo{listErr: errors.New("db down")}
	service := newTestNotifyService(repo, stubLogReader{}, stubDecryptor{}, &stubDeliverer{})
	_, err := service.RunOnce(context.Background(), time.Now(), time.UTC, false)
	if err == nil {
		t.Fatal("expected a pass-level error when listing owners fails")
	}
}

// TestNotifySkipsDisabledOwner proves an owner with webhook delivery off is never
// contacted.
func TestNotifySkipsDisabledOwner(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	record.WebhookEnabled = false
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(deliverer.deliveries()) != 0 || report.Sent != 0 {
		t.Fatalf("disabled owner must not be contacted, deliveries=%d sent=%d", len(deliverer.deliveries()), report.Sent)
	}
}
