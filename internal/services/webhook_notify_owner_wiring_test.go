package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// This file is WEB-16 TS-M04 + TS-M10: it pins the exact owner → URL →
// payload wiring the notify pass promises (notifications-egress.md) and the
// reminder-timing due/not-due/watermark contract (webhook_reminder.go), at a
// level of exactness the existing suite left implicit. Every expected date
// below is HAND-COMPUTED from the fixture's own LastPeriodStart/CycleLength —
// never derived by calling CalendarDay/AddCalendarDays/CalendarDaysBetween
// (the production helpers under test) — so a test can never agree with a
// mutated production computation by construction.

// TestNotifyOwnerZoneProducesADifferentPayloadDateThanTheFallbackZone pins
// resolveOwnerLocation's promise (timezone-calendar.md: "the single
// owner-timezone resolver for every request-free pass") at the one point
// where getting it wrong is invisible without an exact-date assertion: a
// moment picked so the owner's persisted zone and the injected fallback zone
// disagree about "today" ACROSS a cycle-length boundary, so the two zones
// don't just differ by a day in EventDate — one of them proves due at all.
//
// Fixture (UTC, hand-computed, matches probes run against the real tzdata
// entries independently of this test): LastPeriodStart = 2026-02-12,
// CycleLength = 28. at instant now = 2026-03-12T05:00:00Z:
//   - owner zone Pacific/Midway (UTC-11): local wall clock is
//     2026-03-11T18:00, so owner-local "today" = 2026-03-11. Elapsed days
//     since LastPeriodStart = 27 (< 28 cycle length) ⇒ zero cycles have
//     rolled ⇒ the projected next period stays LastPeriodStart+28 days =
//     2026-03-12, one day out — inside the 3-day lead window.
//   - fallback zone UTC: "today" = 2026-03-12 (the same instant, 11 hours
//     later in the calendar). Elapsed days = 28 = exactly one cycle length ⇒
//     ONE cycle has rolled, so the projected cycle start itself jumps to
//     2026-03-12 and the next period after THAT projects to 2026-04-09 — 28
//     days out, outside the lead window entirely.
//
// So a pass that reads the fallback zone instead of the owner's persisted
// one doesn't merely date the reminder wrong — it fails to send one at all,
// which is what makes this assertion self-checking: a correct pass reports
// exactly one delivery, dated 2026-03-12.
func TestNotifyOwnerZoneProducesADifferentPayloadDateThanTheFallbackZone(t *testing.T) {
	now := time.Date(2026, 3, 12, 5, 0, 0, 0, time.UTC)
	lastPeriodStart := time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC)

	record := models.WebhookNotifyRecord{
		ID:                  1,
		CycleLength:         28,
		PeriodLength:        5,
		LutealPhase:         14,
		LastPeriodStart:     &lastPeriodStart,
		Timezone:            "Pacific/Midway",
		WebhookEnabled:      true,
		WebhookURL:          "https://a.example/hook",
		WebhookNotifyPeriod: true,
		ReminderLeadDays:    3,
	}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, lastPeriodStart)}}}
	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	deliverer := &stubDeliverer{}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

	// The injected fallback location is UTC — deliberately the OTHER zone in
	// this pair, so any code path that used it instead of the owner's
	// persisted timezone is exercised, not merely unreached.
	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Sent != 1 {
		t.Fatalf("expected exactly one reminder sent (owner-zone 'today' is one day out, inside the lead window), got sent=%d due=%d", report.Sent, report.Due)
	}
	deliveries := deliverer.deliveries()
	if len(deliveries) != 1 {
		t.Fatalf("expected exactly one delivery, got %d", len(deliveries))
	}
	const expectedOwnerZoneDate = "2026-03-12"
	if got := deliveries[0].payload.EventDate; got != expectedOwnerZoneDate {
		t.Fatalf("payload date must follow the owner's persisted zone: expected %q, got %q", expectedOwnerZoneDate, got)
	}
}

// TestNotifySameOwnerDueAtOneMomentAndNotDueAtAnother pins the timing
// contract with one owner across two independent passes: at a moment outside
// the lead window nothing fires, and at a moment inside it the SAME owner's
// SAME reminder fires — proving "due" is a property of the moment, not a
// standing fact about the owner once computed.
//
// Fixture (UTC, hand-computed): LastPeriodStart = 2026-02-12, CycleLength =
// 28 ⇒ the next period always projects to 2026-03-12 for any "today" before
// one full cycle has elapsed (see the boundary test above for what happens
// past it). leadDays = 3.
//   - moment1 = 2026-02-20: today is 8 days after LastPeriodStart, 20 days
//     before the projected 2026-03-12 ⇒ outside [0,3] ⇒ not due.
//   - moment2 = 2026-03-10: today is 2 days before 2026-03-12 ⇒ inside
//     [0,3] ⇒ due, dated 2026-03-12.
func TestNotifySameOwnerDueAtOneMomentAndNotDueAtAnother(t *testing.T) {
	lastPeriodStart := time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC)
	baseRecord := models.WebhookNotifyRecord{
		ID:                  1,
		CycleLength:         28,
		PeriodLength:        5,
		LutealPhase:         14,
		LastPeriodStart:     &lastPeriodStart,
		WebhookEnabled:      true,
		WebhookURL:          "https://a.example/hook",
		WebhookNotifyPeriod: true,
		ReminderLeadDays:    3,
	}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, lastPeriodStart)}}}

	notDueMoment := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
	repoNotDue := &stubNotifyRepo{records: []models.WebhookNotifyRecord{baseRecord}}
	delivererNotDue := &stubDeliverer{}
	serviceNotDue := newTestNotifyService(repoNotDue, logs, stubDecryptor{}, delivererNotDue)
	reportNotDue, err := serviceNotDue.RunOnce(context.Background(), notDueMoment, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce at the not-due moment: %v", err)
	}
	if reportNotDue.Due != 0 || reportNotDue.Sent != 0 {
		t.Fatalf("expected nothing due 20 days ahead of the window, got due=%d sent=%d", reportNotDue.Due, reportNotDue.Sent)
	}
	if len(delivererNotDue.deliveries()) != 0 {
		t.Fatalf("expected no outbound request at the not-due moment, got %d", len(delivererNotDue.deliveries()))
	}

	dueMoment := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	repoDue := &stubNotifyRepo{records: []models.WebhookNotifyRecord{baseRecord}}
	delivererDue := &stubDeliverer{}
	serviceDue := newTestNotifyService(repoDue, logs, stubDecryptor{}, delivererDue)
	reportDue, err := serviceDue.RunOnce(context.Background(), dueMoment, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce at the due moment: %v", err)
	}
	if reportDue.Sent != 1 {
		t.Fatalf("expected exactly one reminder sent 2 days ahead of the window, got sent=%d due=%d", reportDue.Sent, reportDue.Due)
	}
	deliveries := delivererDue.deliveries()
	if len(deliveries) != 1 {
		t.Fatalf("expected exactly one delivery at the due moment, got %d", len(deliveries))
	}
	const expectedDueDate = "2026-03-12"
	if got := deliveries[0].payload.EventDate; got != expectedDueDate {
		t.Fatalf("expected the same owner's reminder dated %q, got %q", expectedDueDate, got)
	}
}

// TestNotifyWatermarkAdvancesToTheExactExpectedAnchorThenSuppresses pins the
// watermark's VALUE, not just its presence: after a successful send it must
// advance to the exact hand-computed cycle anchor, and a second pass inside
// the same window — fed that exact stored watermark, precisely as the real
// repository would return it on a re-read — must send nothing.
//
// Fixture identical to the due/not-due test above: LastPeriodStart =
// 2026-02-12, CycleLength = 28 ⇒ the watermark a successful send at
// 2026-03-10 must write is 2026-03-12 (the projected next period, which is
// definitionally the cycle anchor for a period reminder).
func TestNotifyWatermarkAdvancesToTheExactExpectedAnchorThenSuppresses(t *testing.T) {
	lastPeriodStart := time.Date(2026, 2, 12, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 3, 10, 9, 0, 0, 0, time.UTC)
	record := models.WebhookNotifyRecord{
		ID:                  1,
		CycleLength:         28,
		PeriodLength:        5,
		LutealPhase:         14,
		LastPeriodStart:     &lastPeriodStart,
		WebhookEnabled:      true,
		WebhookURL:          "https://a.example/hook",
		WebhookNotifyPeriod: true,
		ReminderLeadDays:    3,
	}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: {periodStartLog(1, lastPeriodStart)}}}

	repo1 := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	deliverer1 := &stubDeliverer{}
	service1 := newTestNotifyService(repo1, logs, stubDecryptor{}, deliverer1)
	report1, err := service1.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("first RunOnce: %v", err)
	}
	if report1.Sent != 1 {
		t.Fatalf("expected the first pass to send exactly once, got sent=%d", report1.Sent)
	}
	writes := repo1.writes()
	if len(writes) != 1 {
		t.Fatalf("expected exactly one watermark write, got %d", len(writes))
	}
	expectedAnchor := time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC)
	if !writes[0].anchor.Equal(expectedAnchor) {
		t.Fatalf("watermark must advance to the exact projected cycle anchor %s, got %s", expectedAnchor.Format("2006-01-02"), writes[0].anchor.Format("2006-01-02"))
	}

	// Second pass in the SAME window (a few hours later, still 2026-03-10),
	// fed the exact stored watermark the first pass wrote — precisely what a
	// real re-read of the row would return.
	record2 := record
	record2.WebhookPeriodLastSentCycleStart = &expectedAnchor
	repo2 := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record2}}
	deliverer2 := &stubDeliverer{}
	service2 := newTestNotifyService(repo2, logs, stubDecryptor{}, deliverer2)
	laterSameWindow := now.Add(6 * time.Hour)
	report2, err := service2.RunOnce(context.Background(), laterSameWindow, time.UTC, false)
	if err != nil {
		t.Fatalf("second RunOnce: %v", err)
	}
	if report2.Sent != 0 {
		t.Fatalf("a second pass in the same window must not resend, got sent=%d", report2.Sent)
	}
	if len(deliverer2.deliveries()) != 0 {
		t.Fatalf("a second pass in the same window made an outbound request: %#v", deliverer2.deliveries())
	}
	if report2.SkippedIdempotent != 1 {
		t.Fatalf("expected the resend to be reported as watermark-suppressed, got skipped=%d", report2.SkippedIdempotent)
	}
}

// TestNotifyCrossOwnerWiringHoldsAcrossBothDeliveryFormats drives the notify
// pass with the REAL hardened deliverer (not the in-process stub every other
// test in this package uses) against two httptest servers, so it exercises
// the actual wire format alongside owner scoping. The repo carries two
// delivery formats (webhook_delivery.go: the default JSON envelope, and
// ntfy's plain-text form selected by a `?format=ntfy` query param on the
// STORED url — see webhookFormatNtfy) — WEB-25/PR#757 covered format
// selection in isolation; this proves owner→URL→payload identity holds
// under EITHER format, not only the one the in-process stub bypasses
// entirely.
//
//   - Owner A's URL carries no format param ⇒ JSON envelope; the assertion
//     decodes A's server body as WebhookPayload and checks EventDate.
//   - Owner B's URL carries ?format=ntfy ⇒ plain-text body (Message +
//     disclaimer) and an X-Title header carrying the localized title; the
//     assertion checks the body contains B's own date and never A's.
//
// Fixture dates (UTC, hand-computed from dueRecord's own arithmetic, not
// from a helper call): now = 2026-03-12. Owner A last period 26 days ago
// (2026-02-14) ⇒ next period 2026-02-14+28 = 2026-03-14. Owner B last period
// 27 days ago (2026-02-13) ⇒ next period 2026-02-13+28 = 2026-03-13. Both
// fall inside the 3-day lead window (2 and 1 days out) and are distinct
// dates, so a cross-owner leak is observable either direction.
func TestNotifyCrossOwnerWiringHoldsAcrossBothDeliveryFormats(t *testing.T) {
	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)

	var gotA struct {
		body        []byte
		contentType string
	}
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotA.body, _ = io.ReadAll(r.Body)
		gotA.contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer serverA.Close()

	var gotB struct {
		body        []byte
		contentType string
		title       string
	}
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotB.body, _ = io.ReadAll(r.Body)
		gotB.contentType = r.Header.Get("Content-Type")
		gotB.title = r.Header.Get("X-Title")
		w.WriteHeader(http.StatusOK)
	}))
	defer serverB.Close()

	recordA := dueRecord(1, serverA.URL, now, 26)                // default JSON format
	recordB := dueRecord(2, serverB.URL+"?format=ntfy", now, 27) // ntfy plain-text format

	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{recordA, recordB}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{
		1: {periodStartLog(1, *recordA.LastPeriodStart)},
		2: {periodStartLog(2, *recordB.LastPeriodStart)},
	}}
	// The REAL hardened deliverer, not stubDeliverer: this is the point of
	// the test — the wire format is produced by webhook_delivery.go itself.
	service := NewWebhookNotifyService(repo, logs, stubDecryptor{}, NewWebhookDeliverer(false), stubDisclaimer{text: "Predictions are estimates, not medical advice or a method of contraception."})

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Sent != 2 {
		t.Fatalf("expected both owners delivered, got sent=%d failed=%d", report.Sent, report.Failed)
	}

	const expectedDateA = "2026-03-14"
	const expectedDateB = "2026-03-13"

	if gotA.contentType != "application/json" {
		t.Fatalf("owner A (no format param) must receive the default JSON envelope, got Content-Type %q", gotA.contentType)
	}
	var decodedA WebhookPayload
	if err := json.Unmarshal(gotA.body, &decodedA); err != nil {
		t.Fatalf("owner A body was not valid JSON: %v (body=%q)", err, gotA.body)
	}
	if decodedA.EventDate != expectedDateA {
		t.Fatalf("owner A's JSON payload date: expected %q, got %q", expectedDateA, decodedA.EventDate)
	}
	if strings.Contains(string(gotA.body), expectedDateB) {
		t.Fatalf("cross-owner leak: owner B's date %q reached owner A's (JSON) request body: %q", expectedDateB, gotA.body)
	}

	if gotB.contentType != "text/plain" {
		t.Fatalf("owner B (?format=ntfy) must receive the ntfy plain-text form, got Content-Type %q", gotB.contentType)
	}
	if !strings.Contains(string(gotB.body), expectedDateB) {
		t.Fatalf("owner B's ntfy body must carry B's own date %q, got %q", expectedDateB, gotB.body)
	}
	if strings.Contains(string(gotB.body), expectedDateA) {
		t.Fatalf("cross-owner leak: owner A's date %q reached owner B's (ntfy) request body: %q", expectedDateA, gotB.body)
	}
	if gotB.title == "" {
		t.Fatal("owner B's ntfy request must carry the X-Title header")
	}
}
