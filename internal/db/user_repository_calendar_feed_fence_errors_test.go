package db

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// refusingCalendarFeedFence fails every advance, standing in for the one state
// the repository cannot produce on its own: a fence whose app_state write is
// refused while the database serving these rows still answers.
// calls counts Advance, the refusable form; diverged counts AdvanceOrDiverge,
// the form a side-effect writer must use instead (WEB-414).
type refusingCalendarFeedFence struct {
	err      error
	calls    int
	diverged int
}

func (fence *refusingCalendarFeedFence) Advance(context.Context) error {
	fence.calls++
	return fence.err
}

func (fence *refusingCalendarFeedFence) AdvanceOrDiverge(context.Context) error {
	fence.diverged++
	return fence.err
}

// TestARevocationIsRefusedWhenTheFenceCannotRecordIt pins the half of the
// ordering rule that only shows itself on failure. SaveCalendarFeedToken and
// ClearCalendarFeedToken advance BEFORE their row write precisely so the fence
// can never be behind the row; that guarantee is worth nothing unless a failed
// advance also stops the write. Were the error swallowed here, the row would
// go through with no record outside the database — the exact state a restore
// undoes, and the defect the fence exists to close.
func TestARevocationIsRefusedWhenTheFenceCannotRecordIt(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()
	key := []byte("fence-refusal-test-secret-key-01")

	user := createUserForTimezoneTest(t, repo, "fence-refuses@example.com")
	_, columns, err := services.GenerateCalendarFeedToken(key)
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}

	refused := errors.New("app_state is unavailable")
	fence := &refusingCalendarFeedFence{err: refused}
	repo.calendarFeedFence = fence

	if err := repo.SaveCalendarFeedToken(ctx, user.ID, columns); !errors.Is(err, refused) {
		t.Fatalf("SaveCalendarFeedToken must surface the fence failure, got %v", err)
	}
	if stored := reloadUserForCalendarFeed(t, repo, user.ID); stored.CalendarFeedSelector != "" {
		t.Fatalf("a refused advance must leave the row untouched, got selector %q", stored.CalendarFeedSelector)
	}

	// The same rule on the clear path, and it is the sharper of the two: here
	// the write IS the revocation, so a row cleared without a fence record is a
	// revocation a restore silently reverses.
	repo.calendarFeedFence = nil
	if err := repo.SaveCalendarFeedToken(ctx, user.ID, columns); err != nil {
		t.Fatalf("arm the feed: %v", err)
	}
	repo.calendarFeedFence = fence

	if err := repo.ClearCalendarFeedToken(ctx, user.ID); !errors.Is(err, refused) {
		t.Fatalf("ClearCalendarFeedToken must surface the fence failure, got %v", err)
	}
	if stored := reloadUserForCalendarFeed(t, repo, user.ID); stored.CalendarFeedSelector != columns.Selector {
		t.Fatalf("a refused advance must leave the armed row standing, got selector %q", stored.CalendarFeedSelector)
	}
	if fence.calls != 2 {
		t.Fatalf("both revocation paths must consult the fence, got %d call(s)", fence.calls)
	}
}

// TestAZeroOwnerRevocationNeverAdvancesTheFence pins the other half of the
// ordering: the fence is ahead of the row WRITE, not ahead of the zero-id
// refusal. A revocation for an id that names no owner retires no token, so a
// fence advanced for it records a revocation that never happened — and that
// record is not inert: every backup taken before it now reads as predating a
// revocation, so restoring one disarms every armed feed over an event no owner
// caused.
func TestAZeroOwnerRevocationNeverAdvancesTheFence(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()
	key := []byte("zero-owner-fence-test-secret-k01")

	_, columns, err := services.GenerateCalendarFeedToken(key)
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}

	// A fence that would succeed if consulted: the assertion is that it is
	// never reached, which a refusing fence could not tell apart from a refusal.
	fence := &refusingCalendarFeedFence{}
	repo.calendarFeedFence = fence

	if err := repo.SaveCalendarFeedToken(ctx, 0, columns); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("SaveCalendarFeedToken must refuse a zero owner, got %v", err)
	}
	if err := repo.ClearCalendarFeedToken(ctx, 0); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ClearCalendarFeedToken must refuse a zero owner, got %v", err)
	}
	// Account erasure and clear-data advance the fence ahead of their write, as
	// a removal of the owner's feed; a zero id removes no feed, so the refusal
	// has to come before the advance.
	if err := repo.DeleteAccountAndRelatedData(ctx, 0); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("DeleteAccountAndRelatedData must refuse a zero owner, got %v", err)
	}
	if err := repo.ClearAllDataAndResetSettings(ctx, 0, 1); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ClearAllDataAndResetSettings must refuse a zero owner, got %v", err)
	}
	// The three credential rotations advance ahead of their write as well.
	if err := repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, 0, 1, "recovery-hash", nil); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("UpdateRecoveryCodeHashAndRevokeSessions must refuse a zero owner, got %v", err)
	}
	if err := repo.ForceResetPasswordAndRevokeSessions(ctx, 0, "password-hash", false); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("ForceResetPasswordAndRevokeSessions must refuse a zero owner, got %v", err)
	}
	if err := repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, 0, "hash", 1, "new-password-hash", "new-recovery-hash", nil); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("UpdatePasswordRecoveryCodeAndRevokeSessionsCAS must refuse a zero owner, got %v", err)
	}
	if fence.calls != 0 || fence.diverged != 0 {
		t.Fatalf("a refused zero-owner revocation must not touch the fence, got %d+%d call(s)", fence.calls, fence.diverged)
	}
}

// TestACredentialRotationSurfacesItsOwnWriteFailure covers the arm the fence
// changes did NOT alter and could have. A best-effort advance runs ahead of
// both rotations; the rotation still has to report its own write's failure, or
// a rotation that never happened would be reported as done and the account
// would answer to a password nobody set.
func TestACredentialRotationSurfacesItsOwnWriteFailure(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()
	user := createUserForTimezoneTest(t, repo, "rotation-write-fails@example.com")

	// A closed pool is the one failure both statements share and neither can
	// mistake for an ordinary empty result.
	pool, err := repo.database.DB()
	if err != nil {
		t.Fatalf("database handle: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("close the pool: %v", err)
	}

	if err := repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, user.ID, user.AuthSessionVersion, "recovery-hash", nil); err == nil {
		t.Fatal("UpdateRecoveryCodeHashAndRevokeSessions must report a failed write rather than fall through to the fence")
	}
	if err := repo.ForceResetPasswordAndRevokeSessions(ctx, user.ID, "password-hash", false); err == nil {
		t.Fatal("ForceResetPasswordAndRevokeSessions must report a failed write rather than fall through to the fence")
	}
}

// TestAWebhookWithdrawalIsRefusedWhenTheFenceCannotRecordIt pins WEB-408's
// webhook half: a removal or a save can withdraw an endpoint, and the fence is
// the only thing a later boot can read a restore across it from. Both advance
// BEFORE their row write and stop on a failed advance, and a zero owner is
// refused before the fence is touched.
func TestAWebhookWithdrawalIsRefusedWhenTheFenceCannotRecordIt(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	ctx := context.Background()
	user := createUserForTimezoneTest(t, repo, "webhook-fence-refuses@example.com")
	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).
		Updates(map[string]any{"webhook_enabled": true, "webhook_url": "sealed-endpoint"}).Error; err != nil {
		t.Fatalf("store a webhook: %v", err)
	}
	before := reloadUserForWebhookRevocationTest(t, repo, user.ID)

	refused := errors.New("app_state is unavailable")
	fence := &refusingCalendarFeedFence{err: refused}
	repo.calendarFeedFence = fence

	if err := repo.RemoveWebhookDestination(ctx, user.ID); !errors.Is(err, refused) {
		t.Fatalf("RemoveWebhookDestination must surface the fence failure, got %v", err)
	}
	save := models.WebhookSettingsColumns{Enabled: false, EncryptedURL: "", ExpectedConfigVersion: before.WebhookConfigVersion}
	if err := repo.SaveWebhookSettings(ctx, user.ID, save); !errors.Is(err, refused) {
		t.Fatalf("SaveWebhookSettings must surface the fence failure, got %v", err)
	}
	if after := reloadUserForWebhookRevocationTest(t, repo, user.ID); after.WebhookURL != before.WebhookURL || !after.WebhookEnabled {
		t.Fatalf("a refused advance must leave the endpoint standing, got url %q enabled %t", after.WebhookURL, after.WebhookEnabled)
	}
	if fence.calls != 2 {
		t.Fatalf("both webhook writers must consult the fence, got %d call(s)", fence.calls)
	}

	if err := repo.RemoveWebhookDestination(ctx, 0); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("RemoveWebhookDestination must refuse a zero owner, got %v", err)
	}
	if err := repo.SaveWebhookSettings(ctx, 0, save); !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("SaveWebhookSettings must refuse a zero owner, got %v", err)
	}
	if fence.calls != 2 {
		t.Fatalf("a zero-owner webhook write must not touch the fence, got %d call(s)", fence.calls)
	}

	repo.calendarFeedFence = &refusingCalendarFeedFence{}
	if err := repo.RemoveWebhookDestination(ctx, user.ID); err != nil {
		t.Fatalf("RemoveWebhookDestination with a working fence: %v", err)
	}
	if after := reloadUserForWebhookRevocationTest(t, repo, user.ID); after.WebhookURL != "" {
		t.Fatalf("the removal should go through once the fence records it, got %q", after.WebhookURL)
	}
}

// TestASideEffectFenceFailureNeverRefusesTheWrite covers the writers whose feed
// clear is a side effect and which advance the fence best-effort ahead of their
// write (WEB-397). Unlike TestARevocationIsRefusedWhenTheFenceCannotRecordIt
// above — whose two methods must refuse when the fence cannot keep up — these
// proceed and report SUCCESS: refusing a password reset, a recovery-code
// rotation, a clear-data wipe or an erasure over a calendar-feed marker would
// trade a credential or an erasure for a feed record. The fence's own failure
// is bounded elsewhere — Enforce disarms every armed feed on the next boot.
func TestASideEffectFenceFailureNeverRefusesTheWrite(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()
	refused := errors.New("app_state is unavailable")
	fence := &refusingCalendarFeedFence{err: refused}

	cases := []struct {
		name string
		run  func(userID uint) error
	}{
		{
			name: "UpdateRecoveryCodeHashAndRevokeSessions",
			run: func(userID uint) error {
				return repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, userID, storedSessionVersionForTest(t, repo, userID), "recovery-hash", nil)
			},
		},
		{
			name: "ForceResetPasswordAndRevokeSessions",
			run: func(userID uint) error {
				return repo.ForceResetPasswordAndRevokeSessions(ctx, userID, "password-hash", false)
			},
		},
		{
			name: "UpdatePasswordRecoveryCodeAndRevokeSessionsCAS",
			run: func(userID uint) error {
				// createUserForTimezoneTest below always seeds PasswordHash
				// "hash", so the CAS predicate is known without a reload.
				return repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, userID, "hash", 1, "new-password-hash", "new-recovery-hash", nil)
			},
		},
		{
			name: "ClearAllDataAndResetSettings",
			run: func(userID uint) error {
				return repo.ClearAllDataAndResetSettings(ctx, userID, storedSessionVersionForTest(t, repo, userID))
			},
		},
		{
			name: "DeleteAccountAndRelatedData",
			run: func(userID uint) error {
				return repo.DeleteAccountAndRelatedData(ctx, userID)
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			user := createUserForTimezoneTest(t, repo, "post-op-fence-"+testCase.name+"@example.com")
			repo.calendarFeedFence = fence
			before := fence.diverged

			if err := testCase.run(user.ID); err != nil {
				t.Fatalf("%s must proceed past a failed fence advance, got %v", testCase.name, err)
			}
			if fence.diverged != before+1 {
				t.Fatalf("expected AdvanceOrDiverge exactly once, got %d call(s)", fence.diverged-before)
			}
			// Advance refuses an unwritable file and writes nothing; a writer that
			// proceeds anyway would leave both halves agreeing across it.
			if fence.calls != 0 {
				t.Fatalf("a side-effect writer must never use the refusing Advance, got %d call(s)", fence.calls)
			}
		})
	}
}
