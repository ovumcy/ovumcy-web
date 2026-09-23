package services

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// mr3authCASRepo is a minimal AuthUserRepository implementing the CAS surface.
// Its CAS handler succeeds without touching the *models.User pointer the
// service mutates directly; the only channel that can move the passed-in
// userSnap's AuthSessionVersion is the beforeCommit callback the service wires
// into staged.AuthSessionVersion (auth_service.go, ResetPasswordAndRotateRecoveryCodeCAS).
// This isolates that wiring from any version arithmetic the repository itself
// performs.
type mr3authCASRepo struct {
	stubAuthUserRepo
}

func (r *mr3authCASRepo) UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(
	_ context.Context, _ uint, _ string, oldSessionVersion int, _, _ string, beforeCommit func(sessionVersion int) error,
) error {
	// Succeed; deliberately do NOT mutate any user state directly — mirror the
	// real UPDATE's raw `auth_session_version + 1` off the CAS predicate's own
	// version term and hand it to beforeCommit, which is the only thing the
	// assertion below can be pinning.
	if beforeCommit != nil {
		return beforeCommit(oldSessionVersion + 1)
	}
	return nil
}

// TestMR3Auth_CASBumpsPassedUserSessionVersion pins that
// ResetPasswordAndRotateRecoveryCodeCAS wires the repo-reported session
// version (via beforeCommit) into the userSnap pointer it returns to its
// caller. The existing CAS regression test asserts on the STUB-mutated user
// object, not the userSnap pointer the service itself writes. Here the stub's
// CAS never touches user state directly, so AuthSessionVersion on userSnap can
// only reach 2 via the service's beforeCommit wiring.
func TestMR3Auth_CASBumpsPassedUserSessionVersion(t *testing.T) {
	originalHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash original password: %v", err)
	}

	repo := &mr3authCASRepo{}
	service := NewAuthService(repo)

	userSnap := models.User{
		ID:                 7,
		PasswordHash:       string(originalHash),
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
	}

	recoveryCode, err := service.ResetPasswordAndRotateRecoveryCodeCAS(
		context.Background(), &userSnap, string(originalHash), "EvenStronger2", noopRecoveryCodeDelivery,
	)
	if err != nil {
		t.Fatalf("ResetPasswordAndRotateRecoveryCodeCAS: unexpected error: %v", err)
	}
	if recoveryCode == "" {
		t.Fatal("expected a non-empty rotated recovery code")
	}

	// The service mutates the passed-in userSnap (production line 460). With
	// the starting version 1, NormalizeAuthSessionVersion(1)+1 == 2.
	if userSnap.AuthSessionVersion != 2 {
		t.Fatalf("expected userSnap.AuthSessionVersion == 2 after CAS bump, got %d", userSnap.AuthSessionVersion)
	}
}
