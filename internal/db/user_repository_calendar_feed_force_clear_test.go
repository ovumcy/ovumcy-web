package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Force-rotate-on-recovery, DB layer (slice 4). The calendar-feed token is a
// long-lived bearer capability that outlives login sessions, so the approved
// governance rule requires that every recovery/compromise op which bumps
// auth_session_version ALSO clears the feed token — IN THE SAME atomic Updates()
// so a partial failure can never revoke sessions while leaving the feed armed
// (or vice versa). These tests pin that atomicity for the three DB methods:
//   - UpdateRecoveryCodeHashAndRevokeSessions        (recovery-code regen)
//   - UpdatePasswordRecoveryCodeAndRevokeSessionsCAS (password reset via code)
//   - ForceResetPasswordAndRevokeSessions            (forced operator reset)
// Clear-data (ClearAllDataAndResetSettings) already clears the feed and is
// covered by its own settings-reset test.

// createArmedFeedUserForForceClear seeds an owner with an ARMED feed token and a
// known auth_session_version, so a recovery op can be shown to clear the feed
// and bump the version together.
func createArmedFeedUserForForceClear(t *testing.T, email string) (*UserRepository, uint) {
	t.Helper()
	database, err := OpenSQLite(filepath.Join(t.TempDir(), "feed-force-clear.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repo := NewUserRepository(database)

	user := &models.User{
		Email:                    email,
		PasswordHash:             "old-hash",
		RecoveryCodeHash:         "old-recovery",
		LocalAuthEnabled:         true,
		AuthSessionVersion:       1,
		Role:                     models.RoleOwner,
		CycleLength:              models.DefaultCycleLength,
		PeriodLength:             models.DefaultPeriodLength,
		AutoPeriodFill:           true,
		CreatedAt:                time.Now().UTC(),
		CalendarFeedSelector:     "ARMEDSELECTOR16X",
		CalendarFeedVerifierHash: "armed-verifier-hash",
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("create armed-feed user: %v", err)
	}
	// Confirm the seed is actually armed and resolvable by selector.
	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "ARMEDSELECTOR16X"); err != nil || !ok {
		t.Fatalf("expected seeded feed to resolve (ok=%v err=%v)", ok, err)
	}
	return repo, user.ID
}

func assertFeedClearedAndVersionBumped(t *testing.T, repo *UserRepository, userID uint, wantVersion int) {
	t.Helper()
	got, err := repo.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got.CalendarFeedSelector != "" || got.CalendarFeedVerifierHash != "" {
		t.Fatalf("expected feed token force-cleared, got selector=%q hash=%q", got.CalendarFeedSelector, got.CalendarFeedVerifierHash)
	}
	if got.AuthSessionVersion != wantVersion {
		t.Fatalf("expected auth_session_version %d, got %d", wantVersion, got.AuthSessionVersion)
	}
	// The old selector must no longer resolve — the feed URL would now 404.
	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "ARMEDSELECTOR16X"); err != nil || ok {
		t.Fatalf("expected cleared selector to be not-found (ok=%v err=%v)", ok, err)
	}
}

// TestRecoveryCodeRegenForceClearsFeedAtomically proves recovery-code
// regeneration clears the feed token AND bumps the version in one update.
func TestRecoveryCodeRegenForceClearsFeedAtomically(t *testing.T) {
	repo, userID := createArmedFeedUserForForceClear(t, "regen-feed-clear@example.com")

	if err := repo.UpdateRecoveryCodeHashAndRevokeSessions(context.Background(), userID, "new-recovery-hash"); err != nil {
		t.Fatalf("UpdateRecoveryCodeHashAndRevokeSessions: %v", err)
	}

	assertFeedClearedAndVersionBumped(t, repo, userID, 2)
	// The credential rotation itself still landed.
	got, _ := repo.FindByID(context.Background(), userID)
	if got.RecoveryCodeHash != "new-recovery-hash" {
		t.Fatalf("expected recovery_code_hash rotated, got %q", got.RecoveryCodeHash)
	}
}

// TestPasswordResetCASForceClearsFeedAtomically proves a successful password
// reset via recovery code (the CAS path) clears the feed AND bumps the version.
func TestPasswordResetCASForceClearsFeedAtomically(t *testing.T) {
	repo, userID := createArmedFeedUserForForceClear(t, "reset-feed-clear@example.com")

	if err := repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(context.Background(), userID, "old-hash", "new-hash", "new-recovery"); err != nil {
		t.Fatalf("CAS reset: %v", err)
	}

	assertFeedClearedAndVersionBumped(t, repo, userID, 2)
	got, _ := repo.FindByID(context.Background(), userID)
	if got.PasswordHash != "new-hash" {
		t.Fatalf("expected password_hash rotated, got %q", got.PasswordHash)
	}
}

// TestPasswordResetCASRollbackLeavesFeedArmedAndVersionUnchanged proves the
// atomicity guarantee under a FAILING sub-step: when the CAS predicate does not
// match (a replayed/concurrent redeem, RowsAffected == 0), NOTHING changes —
// the feed stays armed AND the version is unchanged. The clear cannot happen
// without the version bump because both ride the same single UPDATE.
func TestPasswordResetCASRollbackLeavesFeedArmedAndVersionUnchanged(t *testing.T) {
	repo, userID := createArmedFeedUserForForceClear(t, "reset-feed-rollback@example.com")

	// Wrong oldPasswordHash → CAS matches 0 rows → ErrResetTokenAlreadyConsumed.
	err := repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(context.Background(), userID, "WRONG-OLD-HASH", "new-hash", "new-recovery")
	if !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("expected ErrResetTokenAlreadyConsumed on CAS miss, got %v", err)
	}

	got, err := repo.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	// Feed still armed, version unchanged, credential untouched — the whole
	// UPDATE rolled back as one unit.
	if got.CalendarFeedSelector != "ARMEDSELECTOR16X" || got.CalendarFeedVerifierHash != "armed-verifier-hash" {
		t.Fatalf("expected feed still armed after CAS miss, got selector=%q hash=%q", got.CalendarFeedSelector, got.CalendarFeedVerifierHash)
	}
	if got.AuthSessionVersion != 1 {
		t.Fatalf("expected auth_session_version unchanged (1) after CAS miss, got %d", got.AuthSessionVersion)
	}
	if got.PasswordHash != "old-hash" {
		t.Fatalf("expected password_hash unchanged after CAS miss, got %q", got.PasswordHash)
	}
	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "ARMEDSELECTOR16X"); err != nil || !ok {
		t.Fatalf("expected armed selector to still resolve after CAS miss (ok=%v err=%v)", ok, err)
	}
}

// TestForceOperatorResetForceClearsFeedAtomically proves the forced operator
// reset clears the feed AND bumps the version in one update, and forces a
// password change on next login.
func TestForceOperatorResetForceClearsFeedAtomically(t *testing.T) {
	repo, userID := createArmedFeedUserForForceClear(t, "operator-feed-clear@example.com")

	if err := repo.ForceResetPasswordAndRevokeSessions(context.Background(), userID, "operator-new-hash"); err != nil {
		t.Fatalf("ForceResetPasswordAndRevokeSessions: %v", err)
	}

	assertFeedClearedAndVersionBumped(t, repo, userID, 2)
	got, _ := repo.FindByID(context.Background(), userID)
	if got.PasswordHash != "operator-new-hash" {
		t.Fatalf("expected password_hash rotated, got %q", got.PasswordHash)
	}
	if !got.MustChangePassword {
		t.Fatal("expected MustChangePassword=true after operator reset")
	}
}

// TestRoutinePasswordChangeDoesNotClearFeed proves the NEGATIVE contract: a
// ROUTINE authenticated password change (UpdatePasswordAndRevokeSessions) bumps
// the version but LEAVES the feed armed — a routine change is not a compromise
// event, so the owner keeps their working feed and uses the manual rotate
// control instead.
func TestRoutinePasswordChangeDoesNotClearFeed(t *testing.T) {
	repo, userID := createArmedFeedUserForForceClear(t, "routine-change-keeps-feed@example.com")

	if err := repo.UpdatePasswordAndRevokeSessions(context.Background(), userID, "routine-new-hash", false); err != nil {
		t.Fatalf("UpdatePasswordAndRevokeSessions: %v", err)
	}

	got, err := repo.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if got.AuthSessionVersion != 2 {
		t.Fatalf("expected auth_session_version bumped to 2, got %d", got.AuthSessionVersion)
	}
	// Feed MUST survive a routine change.
	if got.CalendarFeedSelector != "ARMEDSELECTOR16X" || got.CalendarFeedVerifierHash != "armed-verifier-hash" {
		t.Fatalf("expected feed still armed after routine password change, got selector=%q hash=%q", got.CalendarFeedSelector, got.CalendarFeedVerifierHash)
	}
	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "ARMEDSELECTOR16X"); err != nil || !ok {
		t.Fatalf("expected armed selector to still resolve after routine change (ok=%v err=%v)", ok, err)
	}
}
