package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// Finding PRIV-1 / SEC-01, driven end to end: the REAL notify pass over the REAL
// user repository, with a revocation landing in the window the pass actually
// has — after it snapshotted the owner's configuration and before it claims the
// send.
//
// The unit tests either side of this one each hold half of the claim. The db
// cases prove the SQL predicate refuses a stale epoch; the service case proves
// the pass hands the claim the epoch its own snapshot carried. Neither can show
// what the two do together on a live engine, which is the thing the finding was
// about: an owner is told the revocation succeeded, and a pass already in flight
// still POSTs their predicted health data to the endpoint they just removed.
//
// The interleaving is deterministic, not raced. The log read is the pass's own
// seam between the snapshot and the claim, so performing the revocation there
// puts it exactly where a real one would land, with no goroutines and no timing
// assumption to go flaky.

// revokingLogReader serves one owner's day logs and performs a revocation the
// first time it is asked, so the pass is already holding its snapshot when the
// owner's configuration changes underneath it. revoke is called before the logs
// are returned, which models a pass that had already read everything it needs
// and is now on its way to the claim.
// The mutex matches its neighbours in this package (stubDeliverer,
// stubNotifyRepo): the pass is sequential today, so nothing races, but a stub
// that mutates state without one is the odd one out here and would race
// silently the first time a case drives two passes at once — which is exactly
// what internal/reminders already does to the other stubs.
type revokingLogReader struct {
	mu     sync.Mutex
	logs   []models.DailyLog
	revoke func()
	done   bool
}

func (reader *revokingLogReader) ListByUser(_ context.Context, _ uint) ([]models.DailyLog, error) {
	reader.mu.Lock()
	fire := !reader.done && reader.revoke != nil
	if fire {
		reader.done = true
	}
	logs := reader.logs
	reader.mu.Unlock()

	// The revocation runs OUTSIDE the lock: it writes to the same database the
	// pass is reading, and holding a test stub's mutex across that would invent a
	// serialization the production path does not have.
	if fire {
		reader.revoke()
	}
	return logs, nil
}

// webhookRevocationFixture is one armed owner on a real database, at a moment
// when a period reminder is due: last period 26 days ago on a 28-day cycle, with
// a 3-day lead window. ownerEndpoint is the URL the pass would POST to — the
// stub decryptor echoes the stored ciphertext, so the stored value IS the
// destination and a delivery is unambiguous about which configuration produced
// it.
type webhookRevocationFixture struct {
	database *gorm.DB
	repo     *db.UserRepository
	ownerID  uint
	logs     []models.DailyLog
	now      time.Time
}

const ownerEndpoint = "https://owner.example/hook"

func newWebhookRevocationFixture(t *testing.T, name string) webhookRevocationFixture {
	t.Helper()

	database := newTwoOwnerIntegrationDatabase(t, name)
	repo := db.NewUserRepository(database)

	now := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	last := time.Date(2026, 2, 14, 0, 0, 0, 0, time.UTC) // 26 days before now

	owner := createTwoOwnerUser(t, database, name+"@example.com", func(user *models.User) {
		user.LutealPhase = 14
		user.LastPeriodStart = &last
	})

	if err := repo.SaveWebhookSettings(context.Background(), owner.ID, models.WebhookSettingsColumns{
		Enabled:          true,
		EncryptedURL:     ownerEndpoint,
		NotifyPeriod:     true,
		NotifyOvulation:  false, // one deterministic reminder, not two
		ReminderLeadDays: 3,
	}); err != nil {
		t.Fatalf("arm webhook delivery: %v", err)
	}

	return webhookRevocationFixture{
		database: database,
		repo:     repo,
		ownerID:  owner.ID,
		logs:     []models.DailyLog{periodStartLog(owner.ID, last)},
		now:      now,
	}
}

// runPass runs one real notify pass over the fixture, performing revoke in the
// window between the snapshot and the claim. A nil revoke is the control run.
func (fixture webhookRevocationFixture) runPass(t *testing.T, revoke func()) (NotifyReport, *stubDeliverer) {
	t.Helper()

	deliverer := &stubDeliverer{}
	logs := &revokingLogReader{logs: fixture.logs, revoke: revoke}
	service := NewWebhookNotifyService(fixture.repo, logs, stubDecryptor{}, deliverer,
		stubDisclaimer{text: "Predictions are estimates, not medical advice or a method of contraception."})

	report, err := service.RunOnce(context.Background(), fixture.now, time.UTC, false)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	return report, deliverer
}

// TestNotifyPassDeliversWhenTheConfigurationStands is the positive control for
// the three revocation cases below. Without it each of them would pass just as
// well against a fixture that was never due, a decryptor that never resolved, or
// a claim predicate that refuses everything.
func TestNotifyPassDeliversWhenTheConfigurationStands(t *testing.T) {
	fixture := newWebhookRevocationFixture(t, "wh-revocation-control")

	report, deliverer := fixture.runPass(t, nil)

	if report.Sent != 1 {
		t.Fatalf("expected the armed owner's reminder to be delivered, got sent=%d due=%d skipped=%d", report.Sent, report.Due, report.SkippedIdempotent)
	}
	deliveries := deliverer.deliveries()
	if len(deliveries) != 1 || deliveries[0].url != ownerEndpoint {
		t.Fatalf("expected exactly one delivery to %s, got %+v", ownerEndpoint, deliveries)
	}
}

// TestNotifyPassCannotDeliverAfterTheOwnerRevoked is the finding itself, one
// subtest per revocation the owner can perform. In each the pass has already
// snapshotted an armed configuration; the owner then revokes and is told it
// succeeded; the pass must deliver NOTHING.
//
// Before the revocation epoch every one of these delivered. A disable, a
// replacement and a removal all go through the settings save, which deliberately
// leaves the watermarks alone, so the stale snapshot's compare-and-set still
// held; a clear-data wipe NULLs them, which re-opened the first-ever-send branch
// outright.
func TestNotifyPassCannotDeliverAfterTheOwnerRevoked(t *testing.T) {
	cases := []struct {
		name   string
		revoke func(t *testing.T, fixture webhookRevocationFixture)
	}{
		{
			name: "delivery disabled",
			revoke: func(t *testing.T, fixture webhookRevocationFixture) {
				if err := fixture.repo.SaveWebhookSettings(context.Background(), fixture.ownerID, models.WebhookSettingsColumns{
					Enabled:          false,
					EncryptedURL:     ownerEndpoint,
					NotifyPeriod:     true,
					NotifyOvulation:  false,
					ReminderLeadDays: 3,
				}); err != nil {
					t.Fatalf("disable delivery: %v", err)
				}
			},
		},
		{
			name: "endpoint replaced",
			revoke: func(t *testing.T, fixture webhookRevocationFixture) {
				if err := fixture.repo.SaveWebhookSettings(context.Background(), fixture.ownerID, models.WebhookSettingsColumns{
					Enabled:          true,
					EncryptedURL:     "https://somewhere-else.example/hook",
					NotifyPeriod:     true,
					NotifyOvulation:  false,
					ReminderLeadDays: 3,
				}); err != nil {
					t.Fatalf("replace the endpoint: %v", err)
				}
			},
		},
		{
			name: "all data cleared",
			revoke: func(t *testing.T, fixture webhookRevocationFixture) {
				if err := fixture.repo.ClearAllDataAndResetSettings(context.Background(), fixture.ownerID); err != nil {
					t.Fatalf("clear all data: %v", err)
				}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newWebhookRevocationFixture(t, "wh-revoked")

			report, deliverer := fixture.runPass(t, func() { testCase.revoke(t, fixture) })

			if delivered := deliverer.deliveries(); len(delivered) != 0 {
				t.Fatalf("the pass POSTed to a revoked configuration: %+v", delivered)
			}
			if report.Sent != 0 {
				t.Fatalf("expected nothing sent after the revocation, got sent=%d", report.Sent)
			}
			if report.Failed != 0 {
				t.Fatalf("a revoked send is a skip, not a failure the operator must chase: failed=%d", report.Failed)
			}

			// "Nothing was delivered" is the assertion this case is about, and on
			// its own it is also what a pass that decided nothing was due would
			// report. Pin that the reminder WAS due and that the claim is what
			// withheld it, so a fixture decaying under the revoking reader — a
			// drifted anchor, a lead window that stops covering it — turns this
			// case red instead of green for the wrong reason.
			if report.Due != 1 {
				t.Fatalf("expected the reminder to be due and refused at the claim, got due=%d", report.Due)
			}
			if report.SkippedIdempotent < 1 {
				t.Fatalf("expected the lost claim to be counted as a skip, got skipped=%d", report.SkippedIdempotent)
			}

			// The claim must also have left no trace: a watermark standing for a
			// send that never happened would suppress the reminder for good if the
			// owner re-armed delivery on the same cycle.
			var after models.User
			if err := fixture.database.First(&after, fixture.ownerID).Error; err != nil {
				t.Fatalf("reload owner: %v", err)
			}
			if after.WebhookPeriodLastSentCycleStart != nil {
				t.Fatalf("a lost claim must write no watermark, got %v", after.WebhookPeriodLastSentCycleStart)
			}
		})
	}
}
