package db

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// refusingCalendarFeedFence fails every advance, standing in for the one state
// the repository cannot produce on its own: a fence whose app_state write is
// refused while the database serving these rows still answers.
type refusingCalendarFeedFence struct {
	err   error
	calls int
}

func (fence *refusingCalendarFeedFence) Advance(context.Context) error {
	fence.calls++
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

// TestACredentialRotationSurfacesItsOwnWriteFailure covers the arm the fence
// changes did NOT alter and could have. Both rotations grew an error check so
// the advance could follow their write; that check has to keep reporting the
// write's own failure, or a rotation that never happened would be reported as
// done and the account would answer to a password nobody set.
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

	if err := repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, user.ID, "recovery-hash"); err == nil {
		t.Fatal("UpdateRecoveryCodeHashAndRevokeSessions must report a failed write rather than fall through to the fence")
	}
	if err := repo.ForceResetPasswordAndRevokeSessions(ctx, user.ID, "password-hash"); err == nil {
		t.Fatal("ForceResetPasswordAndRevokeSessions must report a failed write rather than fall through to the fence")
	}
}

// TestPostOperationFenceFailureNeverFailsAnAlreadyCommittedWrite covers the
// three callers that advance the fence AFTER their own Updates() has already
// committed: UpdateRecoveryCodeHashAndRevokeSessions,
// ForceResetPasswordAndRevokeSessions, and
// UpdatePasswordRecoveryCodeAndRevokeSessionsCAS. Unlike
// TestARevocationIsRefusedWhenTheFenceCannotRecordIt above — whose two
// methods advance BEFORE their write and must refuse when the fence cannot
// keep up — these three must report SUCCESS once their own write has landed,
// because undoing the report at that point would not undo the write: a
// caller told "reset password: app_state is unavailable" after the new hash
// is already stored would retry a reset that had already happened, or an
// operator would tell an owner their forced reset failed when the account
// already answers to the new password. The fence's own failure is bounded
// elsewhere — Enforce disarms every armed feed on the next boot — never by
// silently pretending the credential write did not happen.
func TestPostOperationFenceFailureNeverFailsAnAlreadyCommittedWrite(t *testing.T) {
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
				return repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, userID, "recovery-hash")
			},
		},
		{
			name: "ForceResetPasswordAndRevokeSessions",
			run: func(userID uint) error {
				return repo.ForceResetPasswordAndRevokeSessions(ctx, userID, "password-hash")
			},
		},
		{
			name: "UpdatePasswordRecoveryCodeAndRevokeSessionsCAS",
			run: func(userID uint) error {
				// createUserForTimezoneTest below always seeds PasswordHash
				// "hash", so the CAS predicate is known without a reload.
				return repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, userID, "hash", "new-password-hash", "new-recovery-hash")
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			user := createUserForTimezoneTest(t, repo, "post-op-fence-"+testCase.name+"@example.com")
			repo.calendarFeedFence = fence
			before := fence.calls

			if err := testCase.run(user.ID); err != nil {
				t.Fatalf("%s must report success once its own write has committed, got %v", testCase.name, err)
			}
			if fence.calls != before+1 {
				t.Fatalf("expected the fence to be consulted exactly once, got %d call(s)", fence.calls-before)
			}
		})
	}
}
