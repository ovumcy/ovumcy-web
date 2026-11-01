package cli

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestARestoreRevivesNoPasswordAForcedResetReplaced is WEB-408's credential
// fixture, end to end on a real database and a real fence file: an account is
// compromised, the operator force-resets it, and a backup taken before the
// reset is restored with the server stopped. The restored row carries the
// compromised hash again. The server's boot pass must leave that password —
// and every other local password — unable to reach a session until the
// operator sets a new one; must_change_password would not do it, because the
// forced-reset route accepts the same password and outranks TOTP.
func TestARestoreRevivesNoPasswordAForcedResetReplaced(t *testing.T) {
	t.Parallel()

	const compromised, replacement, operatorSet = "StrongPass1", "EvenStronger2", "FreshStart3"
	databasePath := filepath.Join(t.TempDir(), "live.db")
	config := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}
	owner := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	fencePath := armOperatorFence(t, databasePath)
	backupPath := backupNotifySQLite(t, databasePath)

	resetTo := func(password string) {
		t.Helper()
		prompt := func() ([]byte, error) { return []byte(password), nil }
		if err := runResetPasswordCommand(config, []string{owner.Email}, fencePath, prompt, &bytes.Buffer{}); err != nil {
			t.Fatalf("reset-password: %v", err)
		}
	}
	signIn := func(password string) (models.User, error) {
		t.Helper()
		var user models.User
		var err error
		withNotifyRepositories(t, databasePath, fencePath, func(repositories *db.Repositories, _ *services.CalendarFeedRestoreFence) {
			user, err = services.NewAuthService(repositories.Users).AuthenticateCredentials(context.Background(), owner.Email, password)
		})
		return user, err
	}

	resetTo(replacement)
	restoreNotifySQLite(t, backupPath, databasePath)
	if _, err := signIn(compromised); err != nil {
		t.Fatalf("fixture: before the server boots, the restored row should accept the compromised password, got %v", err)
	}

	withNotifyRepositories(t, databasePath, fencePath, func(_ *db.Repositories, fence *services.CalendarFeedRestoreFence) {
		outcome, err := fence.Enforce(context.Background())
		if err != nil || !outcome.ContinuityBroken || outcome.Revoked.LocalPasswords != 1 {
			t.Fatalf("server boot over the restore: outcome %+v, err %v", outcome, err)
		}
	})

	for _, password := range []string{compromised, replacement} {
		if _, err := signIn(password); !errors.Is(err, services.ErrAuthInvalidCreds) {
			t.Fatalf("after the boot pass, %q must not sign in, got %v", password, err)
		}
	}

	resetTo(operatorSet)
	user, err := signIn(operatorSet)
	if err != nil {
		t.Fatalf("the operator's new password should sign in, got %v", err)
	}
	if !user.MustChangePassword {
		t.Fatal("the operator's password must still be one the owner is forced to replace")
	}
}
