package db

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// TestUpgradePasswordHashCASRace's sequential-hook siblings (TestRehashRace in
// internal/services, TestUpgradePasswordHashCAS above) commit the competing
// credential write before the CAS starts, or run it from a hook fired inline —
// never a genuinely concurrent transaction. Two paths are therefore never
// exercised: the Postgres lock-wait / EvalPlanQual re-check, and the SQLite
// busy_timeout handler. Both cases below drive the REAL UpgradePasswordHashCAS
// while a SECOND, real, uncommitted transaction holds userID's row locked —
// the shape of runBeforeCommit's commit window in
// UpdatePasswordRecoveryCodeAndRevokeSessions and, via
// updateFromAuthSessionVersionTx, UpdatePasswordAndRevokeSessions — so the
// blocking each driver reports is the database's own, not anything this file
// orchestrates in Go.
//
// holdFor is short (well under SQLite's 5s busy_timeout, see sqlite.go) on
// purpose: a hold that exceeds busy_timeout would make SQLite surface
// SQLITE_BUSY instead of queueing transparently, which is a slower and
// separate case this file does not also carry — the invariant under test is
// "the CAS loses and no stale hash lands", and both drivers reach that
// outcome via a wait, not an error, once the holder is guaranteed to unlock
// well inside the timeout.
func TestUpgradePasswordHashCASLosesUnderARealHeldRowLock(t *testing.T) {
	runCASCasesOnEachDriver(t, []casDriverCase{
		{"the CAS blocked on a held row lock loses to the concurrent change", testUpgradePasswordHashCASLosesUnderARealHeldRowLock},
		{"negative control: an unconditional upgrade under lock contention restores the replaced password", testUpgradePasswordHashCASUnderLockContentionDetectsAnUnconditionalUpgrade},
	})
}

const lockContentionHoldFor = 150 * time.Millisecond

// holdRowLockThenCommit opens its OWN transaction, writes newPasswordHash to
// userID's row through the same updateFromAuthSessionVersionTx helper the
// real credential writers use, and holds that transaction open — uncommitted,
// so the row stays locked at the database level — for holdFor before
// committing. It returns once the write has landed inside the holder's own
// transaction (so the caller knows the row is now locked) and a channel that
// reports the eventual commit's error.
func holdRowLockThenCommit(database *gorm.DB, userID uint, expectedSessionVersion int, newPasswordHash string, holdFor time.Duration) (locked <-chan struct{}, done <-chan error) {
	lockedCh := make(chan struct{})
	doneCh := make(chan error, 1)
	go func() {
		tx := database.Begin()
		if tx.Error != nil {
			close(lockedCh)
			doneCh <- tx.Error
			return
		}
		_, err := updateFromAuthSessionVersionTx(tx, userID, expectedSessionVersion, map[string]any{
			"password_hash":        newPasswordHash,
			"must_change_password": false,
			"local_auth_enabled":   true,
		})
		close(lockedCh)
		if err != nil {
			_ = tx.Rollback()
			doneCh <- err
			return
		}
		time.Sleep(holdFor)
		doneCh <- tx.Commit().Error
	}()
	return lockedCh, doneCh
}

// testUpgradePasswordHashCASLosesUnderARealHeldRowLock is the race itself: the
// login has already verified "legacy-hash", and a real credential-changing
// transaction holds the row locked, uncommitted, when the CAS UPDATE reaches
// the database. The CAS statement blocks on that lock — SQLite's
// busy_timeout, Postgres's row-lock wait — and once the holder commits, its
// own WHERE password_hash = "legacy-hash" no longer matches the row, so it
// must lose without writing anything back.
func testUpgradePasswordHashCASLosesUnderARealHeldRowLock(t *testing.T, repo *UserRepository) {
	user := createUpgradePasswordHashCASUser(t, repo, "rehash-lock-contention@example.com")

	locked, done := holdRowLockThenCommit(repo.database, user.ID, user.AuthSessionVersion, "changed-hash", lockContentionHoldFor)
	<-locked // the holder's write has landed inside its own uncommitted transaction; the row is locked from here on

	applied, err := repo.UpgradePasswordHashCAS(context.Background(), user.ID, "legacy-hash", "upgraded-legacy-hash")
	if err != nil {
		t.Fatalf("a lost race under real lock contention is not a database failure, got %v", err)
	}
	if applied {
		t.Fatal("the upgrade reported applied against a hash the concurrent change had already replaced")
	}

	if err := <-done; err != nil {
		t.Fatalf("the concurrent transaction holding the row lock failed: %v", err)
	}

	got, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find after the lost upgrade: %v", err)
	}
	if got.PasswordHash != "changed-hash" {
		t.Fatalf("password_hash = %q after the lost upgrade under lock contention, want the concurrently-written hash kept", got.PasswordHash)
	}
	if got.AuthSessionVersion != user.AuthSessionVersion+1 {
		t.Fatalf("auth_session_version = %d, want %d: bumped once by the concurrent change, never by the upgrade",
			got.AuthSessionVersion, user.AuthSessionVersion+1)
	}
}

// testUpgradePasswordHashCASUnderLockContentionDetectsAnUnconditionalUpgrade
// is the negative control: the SAME lock contention, but the write that
// follows the holder's commit carries no password_hash predicate at all —
// exactly the write UpgradePasswordHashCAS's CAS exists to refuse. It still
// blocks on the row lock like any other writer, and once the holder commits
// it still lands, overwriting the concurrent change. Without this control, a
// CAS statement that merely queued on the lock and then always affected 0
// rows for some unrelated reason would pass the race case above just as
// convincingly.
func testUpgradePasswordHashCASUnderLockContentionDetectsAnUnconditionalUpgrade(t *testing.T, repo *UserRepository) {
	user := createUpgradePasswordHashCASUser(t, repo, "rehash-lock-contention-control@example.com")

	locked, done := holdRowLockThenCommit(repo.database, user.ID, user.AuthSessionVersion, "changed-hash", lockContentionHoldFor)
	<-locked

	result := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Update("password_hash", "upgraded-legacy-hash")
	if result.Error != nil {
		t.Fatalf("unconditional update: %v", result.Error)
	}
	if result.RowsAffected != 1 {
		t.Fatalf("unconditional update affected %d row(s), want exactly 1", result.RowsAffected)
	}

	if err := <-done; err != nil {
		t.Fatalf("the concurrent transaction holding the row lock failed: %v", err)
	}

	got, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find after the unconditional upgrade: %v", err)
	}
	if got.PasswordHash != "upgraded-legacy-hash" {
		t.Fatalf("an unconditional upgrade under lock contention did not restore the replaced password (got %q): "+
			"the scenario does not reach the write it guards", got.PasswordHash)
	}
}
