package cli

import (
	"bytes"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/db"
	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestRunResetPasswordCommandUpdatesPasswordFromSecurePromptWithoutLeakingPlaintext(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset@example.com", "StrongPass1")
	plaintextPassword := "EvenStronger2"
	var output bytes.Buffer

	err := runResetPasswordCommand(
		databasePath,
		"cli-reset@example.com",
		func() ([]byte, error) {
			return []byte(plaintextPassword), nil
		},
		&output,
	)
	if err != nil {
		t.Fatalf("runResetPasswordCommand returned error: %v", err)
	}

	logged := output.String()
	if strings.Contains(logged, plaintextPassword) {
		t.Fatalf("did not expect plaintext password in command output: %q", logged)
	}
	if strings.Contains(strings.ToLower(logged), "temporary password") {
		t.Fatalf("did not expect temporary-password message in command output: %q", logged)
	}
	if !strings.Contains(logged, "Existing auth sessions were invalidated.") {
		t.Fatalf("expected output to mention auth session invalidation, got %q", logged)
	}
	if !strings.Contains(logged, "User must sign in again and reset the password before continuing.") {
		t.Fatalf("expected output to describe follow-up reset flow, got %q", logged)
	}

	updatedUser := loadCLIResetUser(t, databasePath, "cli-reset@example.com")
	if !updatedUser.MustChangePassword {
		t.Fatalf("expected MustChangePassword=true after cli reset")
	}
	if bcrypt.CompareHashAndPassword([]byte(updatedUser.PasswordHash), []byte(plaintextPassword)) != nil {
		t.Fatalf("expected cli reset to store hash for prompted password")
	}
}

func TestRunResetPasswordCommandReturnsPromptError(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-prompt-error@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		databasePath,
		"cli-reset-prompt-error@example.com",
		func() ([]byte, error) {
			return nil, errors.New("prompt failed")
		},
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "read new password") {
		t.Fatalf("expected prompt error from runResetPasswordCommand, got %v", err)
	}
}

func createCLIResetDatabase(t *testing.T) string {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "cli-reset-test.db")
	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	return databasePath
}

func createCLIResetUser(t *testing.T, databasePath string, email string, password string) {
	t.Helper()

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer sqlDB.Close()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := models.User{
		Email:               strings.ToLower(strings.TrimSpace(email)),
		PasswordHash:        string(passwordHash),
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func loadCLIResetUser(t *testing.T, databasePath string, email string) models.User {
	t.Helper()

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer sqlDB.Close()

	var user models.User
	if err := database.Where("email = ?", strings.ToLower(strings.TrimSpace(email))).First(&user).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	return user
}
