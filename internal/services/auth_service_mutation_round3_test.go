package services

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// mr3authCASRepo is a minimal AuthUserRepository implementing the CAS surface.
// Its CAS handler succeeds without touching the *models.User pointer the
// service mutates, so the only thing that bumps the passed-in userSnap's
// AuthSessionVersion is the production line auth_service.go:460. This isolates
// the `+ 1` arithmetic on the in-memory user the service returns to its caller.
type mr3authCASRepo struct {
	stubAuthUserRepo
}

func (r *mr3authCASRepo) UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(
	_ context.Context, _ uint, _, _, _ string,
) error {
	// Succeed; deliberately do NOT mutate any user state here so the
	// assertion below pins the service's own mutation of userSnap.
	return nil
}

// TestMR3Auth_CASBumpsPassedUserSessionVersion pins auth_service.go:460:81
// `NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1`. The existing CAS
// regression test asserts on the STUB-mutated user object, not the userSnap
// pointer the production line writes. Here the stub's CAS is a no-op on user
// state, so AuthSessionVersion on userSnap can only reach 2 via line 460.
//
// Mutant kill: +→- makes Normalize(1)-1 == 0 (fails want 2); *→Normalize(1)*1
// == 1 (fails); /→1/1 == 1 (fails).
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
		context.Background(), &userSnap, string(originalHash), "EvenStronger2",
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
