package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestResetPasswordFullRecoversAnOIDCOnlyAccountWithUnverifiableTOTP walks the
// operator path documented for WEB-401 R-O1. An OIDC-provisioned account (no
// local hash) whose TOTP no longer decrypts cannot be routed through the
// forced-OIDC reset — the token is bound to a password fingerprint it does not
// have — so the provider sign-in is a dead end. `reset-password --full` is the
// way back: it clears the dead TOTP and the links, sets a temporary password,
// and the next local sign-in lands on the forced change with no TOTP challenge.
func TestResetPasswordFullRecoversAnOIDCOnlyAccountWithUnverifiableTOTP(t *testing.T) {
	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "oidc-only@example.com", "Unused1Pass")
	seeded := loadCLIResetUser(t, databasePath, "oidc-only@example.com")
	seedSecondFactors(t, databasePath, seeded.ID, "oidc-only-subject")
	makeCLIUserOIDCOnly(t, databasePath, seeded.ID)
	fencePath := armOperatorFence(t, databasePath)
	dbConfig := db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}
	secretKey := []byte("0123456789abcdef0123456789abcdef")

	repositories, _, closeDatabase, err := openOperatorRepositories(dbConfig, fencePath)
	if err != nil {
		t.Fatalf("open repositories: %v", err)
	}
	defer closeDatabase()
	authService := services.NewAuthService(repositories.Users)
	limiter := services.NewAttemptLimiter()
	resetService := services.NewPasswordResetService(authService, limiter)

	locked := loadCLIResetUser(t, databasePath, "oidc-only@example.com")
	if _, err := resetService.IssueResetTokenForUser(secretKey, &locked, services.PasswordResetTokenPurposeForcedOIDC, time.Hour, time.Now()); !errors.Is(err, services.ErrPasswordResetTokenInvalidPasswordState) {
		t.Fatalf("expected the forced-OIDC token to be unbuildable without a local hash, got %v", err)
	}

	var output bytes.Buffer
	if err := runResetPasswordCommand(dbConfig, []string{"oidc-only@example.com", "--full"}, fencePath,
		func() ([]byte, error) { return []byte("TempPass2Now"), nil }, &output); err != nil {
		t.Fatalf("reset-password --full: %v", err)
	}

	reset := loadCLIResetUser(t, databasePath, "oidc-only@example.com")
	if !reset.LocalAuthEnabled || reset.PasswordHash == "" || !reset.MustChangePassword || reset.TOTPEnabled ||
		reset.AuthSessionVersion <= locked.AuthSessionVersion || countCLIOIDCIdentities(t, databasePath, reset.ID) != 0 {
		t.Fatalf("--full did not leave a temporary-password account without TOTP or links: %+v", reset)
	}

	loginService := services.NewLoginService(authService, resetService, limiter)

	result, err := loginService.Authenticate(context.Background(), secretKey, "client", "oidc-only@example.com", "TempPass2Now", time.Hour, time.Now())
	if err != nil {
		t.Fatalf("sign-in with the temporary password: %v", err)
	}
	if !result.RequiresPasswordReset || result.ResetToken == "" || result.RequiresTOTP {
		t.Fatalf("expected the forced password change with no TOTP challenge, got reset=%v token=%t totp=%v", result.RequiresPasswordReset, result.ResetToken != "", result.RequiresTOTP)
	}
}

func makeCLIUserOIDCOnly(t *testing.T, databasePath string, userID uint) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"password_hash":      "",
		"local_auth_enabled": false,
	}).Error; err != nil {
		t.Fatalf("make oidc-only: %v", err)
	}
}
