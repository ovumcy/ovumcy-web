package cli

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestParseResetPasswordArgsAcceptsFull(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"--full", "owner@example.com"}, {"owner@example.com", "--full"}, {"--id", "7", "--full"}} {
		opts, err := parseResetPasswordArgs(args)
		if err != nil {
			t.Fatalf("parseResetPasswordArgs(%q): %v", args, err)
		}
		if !opts.full {
			t.Fatalf("parseResetPasswordArgs(%q) did not set full", args)
		}
	}

	opts, err := parseResetPasswordArgs([]string{"owner@example.com"})
	if err != nil || opts.full {
		t.Fatalf("expected the plain form to leave full unset, got %+v, %v", opts, err)
	}
	if _, err := parseResetPasswordArgs([]string{"--full", "--full", "owner@example.com"}); err == nil {
		t.Fatal("expected a repeated --full to be refused")
	}
	if _, err := parseResetPasswordArgs([]string{"--full"}); err == nil {
		t.Fatal("expected --full without an address to be refused")
	}
}

// seedSecondFactors gives the account everything a compromise could have
// enrolled or linked: TOTP, a recovery code and an OIDC identity.
func seedSecondFactors(t *testing.T, databasePath string, userID uint, subject string) {
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

	revealedAt := time.Now().UTC()
	if err := database.Model(&models.User{}).Where("id = ?", userID).Updates(map[string]any{
		"totp_secret":               "sealed-totp-secret",
		"totp_enabled":              true,
		"totp_last_used_step":       42,
		"recovery_code_hash":        "recovery-code-hash",
		"recovery_code_revealed_at": revealedAt,
	}).Error; err != nil {
		t.Fatalf("seed second factors: %v", err)
	}
	identity := models.OIDCIdentity{UserID: userID, Issuer: "https://idp.example.com", Subject: subject, CreatedAt: revealedAt}
	if err := database.Create(&identity).Error; err != nil {
		t.Fatalf("seed oidc identity: %v", err)
	}
}

func countCLIOIDCIdentities(t *testing.T, databasePath string, userID uint) int64 {
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

	var count int64
	if err := database.Model(&models.OIDCIdentity{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count oidc identities: %v", err)
	}
	return count
}

// TestResetPasswordFullClearsEveryFactorAnAttackerCouldHaveAdded is the
// compromise-recovery row: `reset-password --full` clears TOTP and the
// recovery code and unlinks every OIDC identity of the target, together with
// the password reset and the auth_session_version bump, and touches no other
// account. The plain form keeps all three, as it always has.
func TestResetPasswordFullClearsEveryFactorAnAttackerCouldHaveAdded(t *testing.T) {
	for _, full := range []bool{false, true} {
		databasePath := createCLIResetDatabase(t)
		createCLIResetUser(t, databasePath, "target@example.com", "StrongPass1")
		createCLIResetUser(t, databasePath, "bystander@example.com", "StrongPass1")
		target := loadCLIResetUser(t, databasePath, "target@example.com")
		bystander := loadCLIResetUser(t, databasePath, "bystander@example.com")
		seedSecondFactors(t, databasePath, target.ID, "target-subject")
		seedSecondFactors(t, databasePath, bystander.ID, "bystander-subject")
		fencePath := armOperatorFence(t, databasePath)

		args := []string{"target@example.com"}
		if full {
			args = append(args, "--full")
		}
		var output bytes.Buffer
		if err := runResetPasswordCommand(
			db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
			args,
			fencePath,
			func() ([]byte, error) { return []byte("EvenStronger2"), nil },
			&output,
		); err != nil {
			t.Fatalf("runResetPasswordCommand(full=%v): %v", full, err)
		}

		reset := loadCLIResetUser(t, databasePath, "target@example.com")
		if !reset.MustChangePassword || reset.AuthSessionVersion <= target.AuthSessionVersion {
			t.Fatalf("full=%v: expected must_change_password and a bumped auth_session_version, got %v, %d", full, reset.MustChangePassword, reset.AuthSessionVersion)
		}
		cleared := reset.TOTPSecret == "" && !reset.TOTPEnabled && reset.TOTPLastUsedStep == 0 &&
			reset.RecoveryCodeHash == "" && reset.RecoveryCodeRevealedAt == nil &&
			countCLIOIDCIdentities(t, databasePath, target.ID) == 0
		kept := reset.TOTPSecret == "sealed-totp-secret" && reset.TOTPEnabled && reset.TOTPLastUsedStep == 42 &&
			reset.RecoveryCodeHash == "recovery-code-hash" && reset.RecoveryCodeRevealedAt != nil &&
			countCLIOIDCIdentities(t, databasePath, target.ID) == 1
		if full && !cleared {
			t.Fatalf("--full left a factor standing: totp=%q/%v recovery=%q identities=%d", reset.TOTPSecret, reset.TOTPEnabled, reset.RecoveryCodeHash, countCLIOIDCIdentities(t, databasePath, target.ID))
		}
		if !full && !kept {
			t.Fatalf("the plain reset changed a second factor: totp=%q/%v recovery=%q identities=%d", reset.TOTPSecret, reset.TOTPEnabled, reset.RecoveryCodeHash, countCLIOIDCIdentities(t, databasePath, target.ID))
		}
		if mentioned := strings.Contains(output.String(), "any linked OIDC identities were removed"); mentioned != full {
			t.Fatalf("full=%v: output mentions the factor wipe=%v: %q", full, mentioned, output.String())
		}

		other := loadCLIResetUser(t, databasePath, "bystander@example.com")
		if other.TOTPSecret != "sealed-totp-secret" || other.RecoveryCodeHash != "recovery-code-hash" ||
			other.AuthSessionVersion != bystander.AuthSessionVersion || countCLIOIDCIdentities(t, databasePath, bystander.ID) != 1 {
			t.Fatalf("full=%v: the reset reached another account: %+v", full, other)
		}
	}
}
