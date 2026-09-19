package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// recordingRealPassSender is the sender double for this file's single claim: a
// webhook send is observable ONLY through WebhookNotifyService.RunOnce's real
// (dryRun=false) pass. It records every (url, payload) it is asked to deliver so
// the test can pin the exact arguments the real pass sent, and it fails the test
// outright the moment it is invoked while dryRunPhase is armed — RunOnce's own
// contract is that a dry run "makes NO outbound request", so any call reaching
// the sender during that phase is itself the defect being guarded against, not
// merely evidence to compare afterward.
type recordingRealPassSender struct {
	t           *testing.T
	dryRunPhase bool
	calls       []capturedDelivery
}

func (sender *recordingRealPassSender) Deliver(_ context.Context, decryptedURL string, payload WebhookPayload) error {
	if sender.dryRunPhase {
		sender.t.Fatalf("webhook send reached the sender while dryRunPhase was armed (dry run): url=%q", decryptedURL)
	}
	sender.calls = append(sender.calls, capturedDelivery{url: decryptedURL, payload: payload})
	return nil
}

// TestWebhookNotifyRealPassDeliversThroughRunOnce drives the public entry point,
// WebhookNotifyService.RunOnce, over one due reminder: first as a dry run, then
// as the real pass. The sender is never called directly — the only way a
// capture can appear is if RunOnce's own dryRun=false branch reached Deliver,
// which is the branch this test exists to pin. Running the dry-run half first,
// against the SAME fixture and the SAME sender, is what tells "delivered by the
// real pass" apart from "would have been delivered regardless of the flag": if
// the real pass's dryRun argument were flipped to true, the sender would again
// see zero calls and the assertions below on the captured URL/payload would
// fail outright.
func TestWebhookNotifyRealPassDeliversThroughRunOnce(t *testing.T) {
	now := time.Date(2026, 4, 5, 9, 0, 0, 0, time.UTC)
	record := dueRecord(41, "https://ntfy.example/real-pass-topic", now, 26)
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		41: {periodStartLog(41, *record.LastPeriodStart)},
	}}
	sender := &recordingRealPassSender{t: t, dryRunPhase: true}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, sender)

	dryReport, err := service.RunOnce(context.Background(), now, time.UTC, true)
	if err != nil {
		t.Fatalf("RunOnce (dry run): %v", err)
	}
	if dryReport.Sent != 0 {
		t.Fatalf("dry run must not report a send, got Sent=%d", dryReport.Sent)
	}
	if len(sender.calls) != 0 {
		t.Fatalf("dry run must not reach the sender, got %d call(s)", len(sender.calls))
	}

	// Arm the real (non-dry-run) pass over the same due reminder: the dry run
	// above wrote no watermark, so it is still due.
	sender.dryRunPhase = false
	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce (real pass): %v", err)
	}
	if report.Sent != 1 {
		t.Fatalf("real pass should report exactly one send, got Sent=%d (due=%d failed=%d)", report.Sent, report.Due, report.Failed)
	}
	if len(sender.calls) != 1 {
		t.Fatalf("real pass should reach the sender exactly once, got %d call(s)", len(sender.calls))
	}

	got := sender.calls[0]
	if got.url != "https://ntfy.example/real-pass-topic" {
		t.Fatalf("real pass delivered to the wrong URL: %q", got.url)
	}
	if got.payload.Type != DueReminderTypePeriod {
		t.Fatalf("real pass delivered the wrong reminder type: %q", got.payload.Type)
	}
}
