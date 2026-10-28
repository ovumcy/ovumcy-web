package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// With local sign-in off nothing redeems a recovery code, so the forced reset
// writes an empty recovery_code_hash and hands deliver an empty code; the CAS
// and the session-version bump stay exactly as in hybrid (WEB-342).
func TestResetPasswordOnAnOIDCOnlyInstanceMintsNoRecoveryCode(t *testing.T) {
	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-reset-oidc-only-no-code")
	service := NewAuthService(db.NewUserRepository(database))
	owner := createTwoOwnerUser(t, database, "reset-oidc-only@example.com", withLocalCredentials(t, "OwnerPass1"))
	target := readTwoOwnerUser(t, database, owner.ID)

	delivered := "unset"
	deliveredVersion := 0
	recoveryCode, err := service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &target, target.PasswordHash, "EvenStronger2", false, func(user *models.User, code string) error {
		delivered = code
		deliveredVersion = user.AuthSessionVersion
		return nil
	})
	if err != nil {
		t.Fatalf("ResetPasswordAndRotateRecoveryCodeCAS: %v", err)
	}
	if recoveryCode != "" || delivered != "" {
		t.Fatalf("expected no code minted, got returned %q delivered %q", recoveryCode, delivered)
	}

	stored := readTwoOwnerUser(t, database, owner.ID)
	if stored.RecoveryCodeHash != "" || target.RecoveryCodeHash != "" {
		t.Fatal("expected an empty recovery_code_hash on an oidc_only instance")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(stored.PasswordHash), []byte("EvenStronger2")); err != nil {
		t.Fatalf("expected the new password to verify: %v", err)
	}
	if stored.AuthSessionVersion != owner.AuthSessionVersion+1 || deliveredVersion != stored.AuthSessionVersion {
		t.Fatalf("expected the session sealed at the bumped version: owner=%d stored=%d delivered=%d", owner.AuthSessionVersion, stored.AuthSessionVersion, deliveredVersion)
	}

	if _, err := service.ResetPasswordAndRotateRecoveryCodeCAS(context.Background(), &target, owner.PasswordHash, "AnotherPass3", false, noopRecoveryCodeDelivery); err == nil {
		t.Fatal("expected a replay of the consumed reset to lose the CAS")
	}
}

func TestBuildOwnerUserOnAnOIDCOnlyInstanceMintsNoRecoveryCode(t *testing.T) {
	t.Parallel()

	user, recoveryCode, err := NewAuthService(nil).BuildOwnerUserWithRecovery("owner@example.com", "StrongPass1", false, time.Now())
	if err != nil {
		t.Fatalf("BuildOwnerUserWithRecovery: %v", err)
	}
	if recoveryCode != "" || user.RecoveryCodeHash != "" {
		t.Fatalf("expected no recovery code, got %q / hash %q", recoveryCode, user.RecoveryCodeHash)
	}
	if !user.LocalAuthEnabled || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("StrongPass1")) != nil {
		t.Fatal("expected a verifying local password")
	}
}

func TestOperatorUserServiceCreateOwnerPassesTheModeToTheBuilder(t *testing.T) {
	t.Parallel()

	for _, localSignInEnabled := range []bool{true, false} {
		builder := &stubOwnerBuilder{user: models.User{Role: models.RoleOwner}, localSignInEnabled: !localSignInEnabled}
		service := NewOperatorUserService(&stubOperatorUserRepo{}, builder)
		if _, _, err := service.CreateOwner(context.Background(), "owner@example.com", "StrongPass1", localSignInEnabled, time.Now().UTC()); err != nil {
			t.Fatalf("CreateOwner: %v", err)
		}
		if builder.localSignInEnabled != localSignInEnabled {
			t.Fatalf("expected the builder to receive localSignInEnabled=%v", localSignInEnabled)
		}
	}
}
