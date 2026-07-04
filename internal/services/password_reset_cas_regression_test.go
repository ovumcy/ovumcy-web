package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// casStubAuthUserRepo extends stubAuthUserRepo with the
// passwordResetCASRepository surface so ResetPasswordAndRotateRecoveryCodeCAS
// exercises the production CAS path rather than the fallback.
type casStubAuthUserRepo struct {
	stubAuthUserRepo
	// casConsumed tracks whether the CAS UPDATE has already been applied.
	casConsumed bool
	// casOldHashSeen is the oldPasswordHash value the CAS UPDATE received.
	casOldHashSeen string
	// casErr, if set, is returned on the next CAS call.
	casErr error
}

func (s *casStubAuthUserRepo) UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(
	_ context.Context, userID uint, oldPasswordHash, newPasswordHash, recoveryHash string,
) error {
	if s.casErr != nil {
		return s.casErr
	}
	s.casOldHashSeen = oldPasswordHash
	// Simulate the DB CAS: if the stored password already differs from
	// oldPasswordHash, return ErrResetTokenAlreadyConsumed (0 rows affected).
	if s.user.PasswordHash != oldPasswordHash {
		return ErrResetTokenAlreadyConsumed
	}
	// First winner: apply the write.
	s.casConsumed = true
	s.user.PasswordHash = newPasswordHash
	s.user.RecoveryCodeHash = recoveryHash
	s.user.LocalAuthEnabled = true
	s.user.MustChangePassword = false
	s.user.AuthSessionVersion = NormalizeAuthSessionVersion(s.user.AuthSessionVersion) + 1
	return nil
}

// TestResetPasswordAndRotateRecoveryCodeCASRejectsReplay verifies the
// single-use contract at the service layer: a second call with the same
// oldPasswordHash returns ErrResetTokenAlreadyConsumed and leaves the
// auth_session_version incremented exactly once (not twice).
func TestResetPasswordAndRotateRecoveryCodeCASRejectsReplay(t *testing.T) {
	originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash original password: %v", err)
	}

	repo := &casStubAuthUserRepo{}
	repo.user = models.User{
		ID:                 7,
		PasswordHash:       string(originalHash),
		RecoveryCodeHash:   "old-recovery",
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
	}

	service := NewAuthService(repo)

	// First redeem — must succeed.
	userSnap := repo.user // copy for the CAS call
	_, err = service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &userSnap, string(originalHash), "EvenStronger2")
	if err != nil {
		t.Fatalf("first redeem: unexpected error: %v", err)
	}
	if !repo.casConsumed {
		t.Fatal("expected CAS UPDATE to be called on first redeem")
	}
	if repo.user.AuthSessionVersion != 2 {
		t.Fatalf("expected auth_session_version 2 after first redeem, got %d", repo.user.AuthSessionVersion)
	}

	// Second redeem with the SAME oldPasswordHash — must fail.
	userSnap2 := repo.user // state after first write
	_, err = service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &userSnap2, string(originalHash), "AnotherPass3")
	if !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("second redeem: expected ErrResetTokenAlreadyConsumed, got %v", err)
	}

	// auth_session_version must still be 2 (incremented exactly once).
	if repo.user.AuthSessionVersion != 2 {
		t.Fatalf("expected auth_session_version to remain 2 after rejected replay, got %d", repo.user.AuthSessionVersion)
	}
}

// TestCompleteResetSingleUseViaCAS exercises the full CompleteReset path
// (PasswordResetService → AuthService → CAS repo) to confirm that a second
// call with the same token is rejected at the service layer when the
// underlying repo implements the CAS interface.
func TestCompleteResetSingleUseViaCAS(t *testing.T) {
	secret := []byte("test-secret-cas")
	now := time.Date(2026, time.June, 11, 10, 0, 0, 0, time.UTC)

	originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash original password: %v", err)
	}

	repo := &casStubAuthUserRepo{}
	repo.user = models.User{
		ID:                 42,
		PasswordHash:       string(originalHash),
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
	}

	authSvc := NewAuthService(repo)
	resetSvc := NewPasswordResetService(authSvc, nil)

	token, err := authSvc.BuildPasswordResetToken(secret, 42, string(originalHash), 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken: %v", err)
	}

	// First CompleteReset — must succeed.
	user, recoveryCode, err := resetSvc.CompleteReset(context.Background(), secret, token, "EvenStronger2", "EvenStronger2", now.Add(time.Minute))
	if err != nil {
		t.Fatalf("first CompleteReset: unexpected error: %v", err)
	}
	if user == nil || user.ID != 42 {
		t.Fatalf("expected user id 42, got %#v", user)
	}
	if recoveryCode == "" {
		t.Fatal("expected non-empty rotated recovery code")
	}
	if repo.user.AuthSessionVersion != 2 {
		t.Fatalf("expected auth_session_version 2 after first reset, got %d", repo.user.AuthSessionVersion)
	}

	// Second CompleteReset with the same token — token fingerprint still
	// matches (password fingerprint in token == original hash); CAS predicate
	// will reject because the stored hash changed after the first redeem.
	// ResolveUserByResetToken returns the current user state (updated hash),
	// which no longer matches the token fingerprint → ErrInvalidResetToken.
	_, _, err = resetSvc.CompleteReset(context.Background(), secret, token, "EvenStronger2", "EvenStronger2", now.Add(2*time.Minute))
	if err == nil {
		t.Fatal("second CompleteReset: expected error for replayed token, got nil")
	}
	// Either the token fingerprint check or the CAS predicate rejects the replay.
	if !errors.Is(err, ErrInvalidResetToken) && !errors.Is(err, ErrResetTokenAlreadyConsumed) {
		t.Fatalf("second CompleteReset: expected ErrInvalidResetToken or ErrResetTokenAlreadyConsumed, got %v", err)
	}

	// auth_session_version must still be 2 — bumped at most once.
	if repo.user.AuthSessionVersion != 2 {
		t.Fatalf("expected auth_session_version to remain 2 after rejected replay, got %d", repo.user.AuthSessionVersion)
	}
}
