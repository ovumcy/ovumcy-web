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
	records      []models.WebhookNotifyRecord
	listErr      error
	watermarkErr error
	mu           sync.Mutex
	watermarks   []watermarkWrite
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
	return stub.watermarkErr
}

func (stub *stubNotifyRepo) writes() []watermarkWrite {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	out := make([]watermarkWrite, len(stub.watermarks))
	copy(out, stub.watermarks)
	return out
}

// stubLogReader serves per-user logs from a map, or errors when err is set.
type stubLogReader struct {
	byUser map[uint][]models.DailyLog
	err    error
}

func (stub stubLogReader) ListByUser(_ context.Context, userID uint) ([]models.DailyLog, error) {
	if stub.err != nil {
		return nil, stub.err
	}
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

// TestNotifySkipsOwnerWithEmptyDecryptedURL proves an enabled owner whose stored
// URL decrypts to empty (webhook armed but no endpoint) is skipped with no
// delivery — the "nothing deliverable" branch.
func TestNotifySkipsOwnerWithEmptyDecryptedURL(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "", now, 26) // empty ciphertext token -> decryptor echoes ""
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.OwnersScanned != 1 {
		t.Fatalf("expected 1 owner scanned, got %d", report.OwnersScanned)
	}
	if len(deliverer.deliveries()) != 0 || report.Sent != 0 || report.Due != 0 {
		t.Fatalf("owner with empty endpoint must not be contacted, deliveries=%d sent=%d due=%d", len(deliverer.deliveries()), report.Sent, report.Due)
	}
}

// TestNotifySkipsOwnerWhenLogReadFails proves a per-owner log-read failure is
// contained: that owner is skipped and the pass does not error.
func TestNotifySkipsOwnerWhenLogReadFails(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{err: errors.New("logs table gone")}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("a per-owner log-read failure must not fail the pass: %v", err)
	}
	if report.OwnersScanned != 1 {
		t.Fatalf("expected 1 owner scanned, got %d", report.OwnersScanned)
	}
	if len(deliverer.deliveries()) != 0 || report.Sent != 0 {
		t.Fatalf("owner with unreadable logs must be skipped, deliveries=%d sent=%d", len(deliverer.deliveries()), report.Sent)
	}
}

// TestNotifyWatermarkWriteFailureStillCountsSent proves the delivery-vs-watermark
// split: a watermark write that fails AFTER a successful 2xx is logged but does
// NOT turn the send into a failure — the reminder was delivered, so it counts as
// sent (a stuck watermark would at worst re-send next pass, never lose data).
func TestNotifyWatermarkWriteFailureStillCountsSent(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}, watermarkErr: errors.New("watermark write failed")}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Sent != 1 {
		t.Fatalf("a delivered reminder counts as sent even when the watermark write fails, sent=%d", report.Sent)
	}
	if report.Failed != 0 {
		t.Fatalf("a watermark write failure is not a delivery failure, failed=%d", report.Failed)
	}
}

// ovulationDueRecord returns a record whose predicted ovulation is inside the
// lead window: with a 28-day cycle and a 14-day luteal phase, ovulation is ~day
// 14 of the cycle, so a last period ~12 days ago puts it ~2 days out. Both kinds
// are on so the ovulation branch (and its copy) is exercised.
func ovulationDueRecord(id uint, urlToken string, now time.Time) models.WebhookNotifyRecord {
	last := now.AddDate(0, 0, -12)
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
		WebhookNotifyOvulation: true,
		ReminderLeadDays:       3,
	}
}

// TestNotifyDeliversOvulationReminderCopy proves the ovulation reminder path:
// an ovulation-due owner gets a payload whose title/message use the ovulation
// copy (not the period copy), covering the ovulation branch of reminderCopy.
func TestNotifyDeliversOvulationReminderCopy(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := ovulationDueRecord(1, "https://a.example/hook", now)
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	var ovulation *capturedDelivery
	for i := range deliverer.deliveries() {
		d := deliverer.deliveries()[i]
		if d.payload.Type == DueReminderTypeOvulation {
			ovulation = &d
			break
		}
	}
	if ovulation == nil {
		t.Fatalf("expected an ovulation reminder to be delivered (sent=%d, due=%d)", report.Sent, report.Due)
	}
	if !strings.Contains(ovulation.payload.Title, "Ovulation") {
		t.Fatalf("ovulation payload should use the ovulation title, got %q", ovulation.payload.Title)
	}
	if !strings.Contains(ovulation.payload.Message, "ovulation") {
		t.Fatalf("ovulation payload should use the ovulation message, got %q", ovulation.payload.Message)
	}
}

// TestAppendUniqueID pins the failed-owner dedup helper directly: appending an
// id already present is a no-op (so an owner with multiple failing reminders is
// listed once); a new id is appended. Driving the helper directly makes the
// "already present" branch deterministic without depending on a cycle position
// that yields two simultaneously-due reminders.
func TestAppendUniqueID(t *testing.T) {
	var ids []uint

	ids = appendUniqueID(ids, 7)
	if len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("expected [7], got %v", ids)
	}

	ids = appendUniqueID(ids, 7) // duplicate -> no-op
	if len(ids) != 1 {
		t.Fatalf("appending a duplicate must be a no-op, got %v", ids)
	}

	ids = appendUniqueID(ids, 9) // new id -> appended
	if len(ids) != 2 || ids[1] != 9 {
		t.Fatalf("expected [7 9], got %v", ids)
	}
}

// TestResolveOwnerLocationPrefersPersistedTimezone proves the per-owner timezone
// resolution: a valid persisted IANA zone is used; an invalid one falls back to
// the server location; an empty one falls back too.
func TestResolveOwnerLocationPrefersPersistedTimezone(t *testing.T) {
	fallback := time.UTC

	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	if got := resolveOwnerLocation("America/New_York", fallback); got.String() != ny.String() {
		t.Fatalf("valid persisted zone should be used, got %q", got.String())
	}
	if got := resolveOwnerLocation("Not/AZone", fallback); got != fallback {
		t.Fatalf("invalid persisted zone should fall back to server location, got %q", got.String())
	}
	if got := resolveOwnerLocation("   ", fallback); got != fallback {
		t.Fatalf("empty persisted zone should fall back to server location, got %q", got.String())
	}
}

// TestNotifyDecisionMatchesDashboardWhenLogsAreEmpty is a regression test for the
// bug where userFromNotifyRecord built a models.User without Role, causing
// ApplyUserCycleBaseline to bail out early (it gates on user.Role ==
// models.RoleOwner) for the webhook pass only — the dashboard and .ics feed
// build a full user and always apply the baseline. With zero day logs (e.g. an
// owner onboarded with auto_period_fill=false), BuildCycleStats alone has
// nothing to project from, so without the baseline the webhook decision saw no
// next-period date at all and DecideDueReminders returned nothing, while the
// dashboard still showed a next-period date from the user's LastPeriodStart
// anchor. This asserts the webhook decision's period-due date equals the
// dashboard path's (BuildCycleStatsFromLogs with a full owner user +
// DashboardUpcomingPredictions) for the SAME zero-log input.
func TestNotifyDecisionMatchesDashboardWhenLogsAreEmpty(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)

	user := userFromNotifyRecord(record)
	settings := WebhookReminderSettingsFromNotifyRecord(record)
	decided := DecideDueReminders(&user, settings, nil, now, time.UTC)
	if len(decided) != 1 || decided[0].Type != DueReminderTypePeriod {
		t.Fatalf("expected exactly one period reminder from the webhook decision with zero logs, got %+v", decided)
	}

	fullUser := models.User{
		ID:              record.ID,
		Role:            models.RoleOwner,
		CycleLength:     record.CycleLength,
		PeriodLength:    record.PeriodLength,
		LutealPhase:     record.LutealPhase,
		LastPeriodStart: record.LastPeriodStart,
	}
	stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(&fullUser, nil, now, time.UTC)
	today := DateAtLocation(now, time.UTC)
	cycleLength := DashboardCycleReferenceLength(&fullUser, stats)
	prediction := DashboardUpcomingPredictions(stats, &fullUser, today, cycleLength)

	if prediction.NextPeriodStart.IsZero() {
		t.Fatal("test setup: dashboard path should still predict a next period from LastPeriodStart alone")
	}
	if got, want := decided[0].EventDate.Format("2006-01-02"), prediction.NextPeriodStart.Format("2006-01-02"); got != want {
		t.Fatalf("webhook decision period date %q must equal dashboard path date %q", got, want)
	}
}

// TestNotifyDecisionMatchesDashboardWithInferredLutealPhase is a regression test
// for the same userFromNotifyRecord Role gap, this time proving the ovulation
// date matches too when the owner's logs infer a non-default luteal phase (11
// days here, vs. the record's stored default of 14). Without Role set, the
// webhook decision never calls InferUserLutealPhase (ApplyUserCycleBaseline
// bails out before reaching it) and predicts ovulation off the wrong luteal
// phase — off by 3 days from what the dashboard/.ics feed show, and anchored to
// a different cycle start, which could also produce a spurious duplicate
// reminder against an already-sent watermark.
func TestNotifyDecisionMatchesDashboardWithInferredLutealPhase(t *testing.T) {
	day := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02", s, time.UTC)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		return v
	}

	// Three period clusters (Jan1, Jan29, Feb26; 28-day cycles) with BBT rises
	// placed so each completed cycle's observed luteal length is 11 days (rise
	// starts 11 days before the next cycle's start), inferring luteal=11 overall.
	// The current (third) cycle has no BBT yet, matching a real in-progress cycle.
	logs := []models.DailyLog{
		{Date: day("2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: day("2025-02-26"), IsPeriod: true, Flow: models.FlowMedium},

		// Cycle 1 (Jan1→Jan29): coverline window Jan1-6, rise Jan19-21 →
		// ovulation Jan18 (day before first high), luteal = Jan29-Jan18 = 11.
		{Date: day("2025-01-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-04"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-05"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-06"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-19"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-20"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-01-21"), BBT: models.NewBBT(36.50)},

		// Cycle 2 (Jan29→Feb26): coverline window Jan29-Feb3, rise Feb16-18 →
		// ovulation Feb15, luteal = Feb26-Feb15 = 11.
		{Date: day("2025-01-29"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-30"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-01-31"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-01"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-02"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-03"), BBT: models.NewBBT(36.20)},
		{Date: day("2025-02-16"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-17"), BBT: models.NewBBT(36.50)},
		{Date: day("2025-02-18"), BBT: models.NewBBT(36.50)},
	}

	// now = Feb26 + 15 days: with a 28-day cycle and the inferred 11-day luteal
	// phase, ovulation is projected at Feb26+17 = Mar15 — 2 days out, inside a
	// 3-day lead window. The record's own stored LutealPhase (14) is
	// deliberately different from the inferred value (11), so the assertion
	// only passes if the inference actually ran.
	now := day("2025-02-26").AddDate(0, 0, 15)
	last := day("2025-02-26")
	record := models.WebhookNotifyRecord{
		ID:                     2,
		CycleLength:            28,
		PeriodLength:           5,
		LutealPhase:            14,
		LastPeriodStart:        &last,
		WebhookEnabled:         true,
		WebhookURL:             "https://a.example/hook",
		WebhookNotifyOvulation: true,
		ReminderLeadDays:       3,
	}

	user := userFromNotifyRecord(record)
	settings := WebhookReminderSettingsFromNotifyRecord(record)
	decided := DecideDueReminders(&user, settings, logs, now, time.UTC)
	if len(decided) != 1 || decided[0].Type != DueReminderTypeOvulation {
		t.Fatalf("expected exactly one ovulation reminder from the webhook decision, got %+v", decided)
	}

	fullUser := models.User{
		ID:              record.ID,
		Role:            models.RoleOwner,
		CycleLength:     record.CycleLength,
		PeriodLength:    record.PeriodLength,
		LutealPhase:     record.LutealPhase,
		LastPeriodStart: record.LastPeriodStart,
	}
	stats := NewStatsService(nil, nil).BuildCycleStatsFromLogs(&fullUser, logs, now, time.UTC)
	if stats.LutealPhase != 11 {
		t.Fatalf("test setup: expected dashboard path to infer luteal phase 11, got %d", stats.LutealPhase)
	}
	today := DateAtLocation(now, time.UTC)
	cycleLength := DashboardCycleReferenceLength(&fullUser, stats)
	prediction := DashboardUpcomingPredictions(stats, &fullUser, today, cycleLength)

	if prediction.OvulationDate.IsZero() {
		t.Fatal("test setup: dashboard path should predict an ovulation date")
	}
	if got, want := decided[0].EventDate.Format("2006-01-02"), prediction.OvulationDate.Format("2006-01-02"); got != want {
		t.Fatalf("webhook decision ovulation date %q must equal dashboard path date %q", got, want)
	}
}

// TestNotifyUsesOwnerPersistedTimezone drives the persisted-timezone path end to
// end: an owner with a persisted zone is scanned and its decision runs against
// that zone (the pass completes without error), covering resolveOwnerLocation's
// success return from within RunOnce.
func TestNotifyUsesOwnerPersistedTimezone(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "https://a.example/hook", now, 26)
	record.Timezone = "America/New_York"
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, *record.LastPeriodStart)}}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce with a persisted owner timezone: %v", err)
	}
	if report.OwnersScanned != 1 {
		t.Fatalf("expected 1 owner scanned, got %d", report.OwnersScanned)
	}
}
