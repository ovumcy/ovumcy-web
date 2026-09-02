package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The privacy ledger will say "last delivery accepted <T>" from
// webhook_last_delivered_at, and the whole value of that sentence is that no
// other column in this schema could carry it truthfully. The two watermarks are
// claimed BEFORE the POST, hold a cycle anchor rather than a clock reading, and
// stand for a send that was never accepted until the failure path puts them
// back. These tests pin the difference at the only place it can be lost: the
// notify pass.

// readingDeliverer runs a hook while the outbound request is notionally in
// flight — the one moment at which a claim and a delivery record are
// distinguishable — and can then fail the delivery.
type readingDeliverer struct {
	duringDelivery func()
	fail           bool
}

func (d readingDeliverer) Deliver(context.Context, string, WebhookPayload) error {
	if d.duringDelivery != nil {
		d.duringDelivery()
	}
	if d.fail {
		return errors.New("stub delivery failure")
	}
	return nil
}

// TestWebhookDeliveryRecordsAcceptedTimestampOnlyAfterSuccess is the pass-level
// contract of the delivery mark: it appears only for a delivery the endpoint
// accepted, it does not exist yet while the request is in flight, and a failed
// delivery leaves it absent while still handing the claim back.
func TestWebhookDeliveryRecordsAcceptedTimestampOnlyAfterSuccess(t *testing.T) {
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)

	t.Run("an accepted delivery records the mark", func(t *testing.T) {
		repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{dueRecord(1, "url-1", now, 26)}}
		logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: completedCycleStartLogs(1, now.AddDate(0, 0, -26))}}
		service := newTestNotifyService(repo, logs, stubDecryptor{}, readingDeliverer{})

		report, err := service.RunOnce(context.Background(), now, time.UTC, false)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if report.Sent != 1 {
			t.Fatalf("expected one delivered reminder, got %+v", report)
		}
		at, marked := repo.markedAt(1)
		if !marked {
			t.Fatal("expected webhook_last_delivered_at to be set after an accepted delivery")
		}
		if !at.Equal(now) {
			t.Fatalf("expected the mark to carry the pass clock %s, got %s", now, at)
		}
	})

	t.Run("the mark is still NULL while the request is in flight", func(t *testing.T) {
		repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{dueRecord(1, "url-1", now, 26)}}
		logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: completedCycleStartLogs(1, now.AddDate(0, 0, -26))}}

		// The arming assertion this whole PR turns on. An implementation that
		// stamped the mark at the claim — beside the watermark, before the POST —
		// would pass every other test in this file and make the ledger claim a
		// delivery for a request that never left.
		var markedDuringDelivery bool
		var watermarkClaimedDuringDelivery bool
		deliverer := readingDeliverer{duringDelivery: func() {
			_, markedDuringDelivery = repo.markedAt(1)
			watermarkClaimedDuringDelivery = len(repo.writes()) > 0
		}}
		service := newTestNotifyService(repo, logs, stubDecryptor{}, deliverer)

		if _, err := service.RunOnce(context.Background(), now, time.UTC, false); err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if !watermarkClaimedDuringDelivery {
			t.Fatal("the claim is supposed to be taken before the request leaves: this test is no longer measuring the window it claims to")
		}
		if markedDuringDelivery {
			t.Fatal("webhook_last_delivered_at was already set while the request was in flight: the mark is being written at the claim, not after a 2xx")
		}
		if _, marked := repo.markedAt(1); !marked {
			t.Fatal("expected the mark to be set once the delivery was accepted")
		}
	})

	t.Run("a failed delivery leaves the mark absent and still releases the claim", func(t *testing.T) {
		repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{dueRecord(1, "url-1", now, 26)}}
		logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: completedCycleStartLogs(1, now.AddDate(0, 0, -26))}}
		service := newTestNotifyService(repo, logs, stubDecryptor{}, readingDeliverer{fail: true})

		report, err := service.RunOnce(context.Background(), now, time.UTC, false)
		if err != nil {
			t.Fatalf("RunOnce: %v", err)
		}
		if report.Sent != 0 || report.Failed != 1 {
			t.Fatalf("expected the failed delivery to be counted as failed, got %+v", report)
		}
		if calls := repo.marks(); len(calls) != 0 {
			t.Fatalf("expected no delivery mark write for a failed delivery, got %+v", calls)
		}
		if _, marked := repo.markedAt(1); marked {
			t.Fatal("a failed delivery moved webhook_last_delivered_at")
		}
		if released := repo.released(); len(released) != 1 {
			t.Fatalf("expected the claim to be released after a failed delivery, got %d releases", len(released))
		}
	})
}

// TestWebhookDeliveryMarkPinsTheClaimedConfigVersion proves the second write
// asks the same question the claim did. Between the claim and the 2xx the owner
// can revoke the configuration; a mark written without the pinned epoch would
// credit a delivery to a configuration that no longer exists.
func TestWebhookDeliveryMarkPinsTheClaimedConfigVersion(t *testing.T) {
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "url-1", now, 26)
	record.WebhookConfigVersion = 7

	repo := &stubNotifyRepo{
		records: []models.WebhookNotifyRecord{record},
		// The stored epoch has moved on since the snapshot: a settings save landed
		// while the request was on the wire.
		epochs: map[uint]int{1: 8},
	}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: completedCycleStartLogs(1, now.AddDate(0, 0, -26))}}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, readingDeliverer{})

	if _, err := service.RunOnce(context.Background(), now, time.UTC, false); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	calls := repo.marks()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one delivery mark attempt, got %d", len(calls))
	}
	if calls[0].configVersion != 7 {
		t.Fatalf("expected the mark to pin the epoch the pass's own snapshot carried (7), got %d", calls[0].configVersion)
	}
	if _, marked := repo.markedAt(1); marked {
		t.Fatal("the mark was stored although the owner's configuration epoch had moved since the claim")
	}
}

// TestWebhookDeliveryMarkFailureDoesNotChangeTheReport pins the priority between
// the delivery and its record: the POST was accepted, so the reminder is sent
// however the mark write goes. Counting it as failed would be the expensive
// error — nothing would stop the next pass sending the same reminder again.
func TestWebhookDeliveryMarkFailureDoesNotChangeTheReport(t *testing.T) {
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	repo := &stubNotifyRepo{
		records: []models.WebhookNotifyRecord{dueRecord(1, "url-1", now, 26)},
		markErr: errors.New("stub mark write failure"),
	}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: completedCycleStartLogs(1, now.AddDate(0, 0, -26))}}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, readingDeliverer{})

	report, err := service.RunOnce(context.Background(), now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if report.Sent != 1 || report.Failed != 0 {
		t.Fatalf("expected Sent=1, Failed=0 for one accepted delivery whose mark write failed, got %+v", report)
	}
	if writes := repo.writes(); len(writes) != 1 {
		t.Fatalf("expected the watermark claim to stand after a failed mark write, got %d net writes", len(writes))
	}
	if released := repo.released(); len(released) != 0 {
		t.Fatalf("a failed mark write released the claim, which would let the next pass re-send a delivered reminder: %+v", released)
	}
}

// TestWebhookDeliveryMarkIsNotDerivedFromAWatermark is the ledger's first
// constraint, asserted where it can actually be violated. The recorded value
// must be a clock reading, never either watermark and nothing computed from
// them: those are UTC-midnight cycle anchors, and rendering one as a delivery
// time is the falsehood this column exists to replace.
func TestWebhookDeliveryMarkIsNotDerivedFromAWatermark(t *testing.T) {
	now := time.Date(2026, 4, 10, 9, 0, 0, 0, time.UTC)
	record := dueRecord(1, "url-1", now, 26)
	// Both watermarks carry a value, so a mark derived from max() of them would
	// have something to be derived from.
	periodWatermark := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	ovulationWatermark := time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC)
	record.WebhookPeriodLastSentCycleStart = &periodWatermark
	record.WebhookOvulationLastSentCycleStart = &ovulationWatermark

	repo := &stubNotifyRepo{records: []models.WebhookNotifyRecord{record}}
	logs := stubLogReader{byUser: map[uint][]models.DailyLog{1: completedCycleStartLogs(1, now.AddDate(0, 0, -26))}}
	service := newTestNotifyService(repo, logs, stubDecryptor{}, readingDeliverer{})

	if _, err := service.RunOnce(context.Background(), now, time.UTC, false); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}

	at, marked := repo.markedAt(1)
	if !marked {
		t.Fatal("expected a delivery mark for the accepted delivery")
	}
	for _, watermark := range []time.Time{periodWatermark, ovulationWatermark} {
		if at.Equal(watermark) {
			t.Fatalf("the delivery mark was written as a watermark value (%s): a claim anchor is not a delivery time", watermark)
		}
	}
	if at.Hour() == 0 && at.Minute() == 0 && at.Second() == 0 {
		t.Fatalf("the delivery mark landed on UTC midnight (%s), the shape of a cycle anchor rather than of a clock reading", at)
	}
}
