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
