package db

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestUpgradeTOTPSecretCiphertextCAS(t *testing.T) {
	runCASCasesOnEachDriver(t, []casDriverCase{
		{"preserves the session and the factor state", testUpgradeTOTPSecretCiphertextCASPreservesSessionAndFactor},
		{"loses to a re-enrollment", testUpgradeTOTPSecretCiphertextCASLosesToAReEnrollment},
		{"loses to a disable", testUpgradeTOTPSecretCiphertextCASLosesToADisable},
	})
}

func createUpgradeTOTPSecretCiphertextCASUser(t *testing.T, repo *UserRepository, email string) *models.User {
	t.Helper()

	user := &models.User{
		Email:              email,
		PasswordHash:       "password-hash",
		LocalAuthEnabled:   true,
		TOTPSecret:         "legacy-ciphertext",
		TOTPEnabled:        true,
		TOTPLastUsedStep:   7,
		AuthSessionVersion: 4,
		Role:               models.RoleOwner,
		CycleLength:        models.DefaultCycleLength,
		PeriodLength:       models.DefaultPeriodLength,
		AutoPeriodFill:     true,
		CreatedAt:          time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

// testUpgradeTOTPSecretCiphertextCASPreservesSessionAndFactor asserts the
// re-encryption rewrites totp_secret alone: no session is revoked, the factor
// stays enrolled, and the consumed step is not rewound (a rewind would reopen
// the replay window of the code that just passed).
func testUpgradeTOTPSecretCiphertextCASPreservesSessionAndFactor(t *testing.T, repo *UserRepository) {
	user := createUpgradeTOTPSecretCiphertextCASUser(t, repo, "totp-applied@example.com")

	applied, err := repo.UpgradeTOTPSecretCiphertextCAS(context.Background(), user.ID, "legacy-ciphertext", "resealed-ciphertext")
	if err != nil {
		t.Fatalf("UpgradeTOTPSecretCiphertextCAS: %v", err)
	}
	if !applied {
		t.Fatal("the upgrade against the current ciphertext reported not applied")
	}

	got, err := repo.FindByID(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("find after re-encryption: %v", err)
	}
	if got.TOTPSecret != "resealed-ciphertext" {
		t.Fatalf("totp_secret = %q, want the resealed ciphertext", got.TOTPSecret)
	}
	if got.AuthSessionVersion != 4 {
		t.Fatalf("auth_session_version = %d, want 4 unchanged (no revoke)", got.AuthSessionVersion)
	}
	if !got.TOTPEnabled {
		t.Fatal("totp_enabled was cleared by a storage upgrade")
	}
	if got.TOTPLastUsedStep != 7 {
		t.Fatalf("totp_last_used_step = %d, want 7 untouched", got.TOTPLastUsedStep)
	}
}

// testUpgradeTOTPSecretCiphertextCASLosesToAReEnrollment is the predicate
// itself: a re-enrollment that lands after the 2FA check read the ciphertext
// must survive the re-encryption that check then attempts. Without the
// predicate the upgrade would put the replaced secret back, with no
// session-version bump.
func testUpgradeTOTPSecretCiphertextCASLosesToAReEnrollment(t *testing.T, repo *UserRepository) {
	user := createUpgradeTOTPSecretCiphertextCASUser(t, repo, "totp-reenrolled@example.com")
	if err := repo.UpdateTOTPFieldsAndRevokeSessions(context.Background(), user.ID, storedSessionVersionForTest(t, repo, user.ID), "reenrolled-ciphertext", true); err != nil {
		t.Fatalf("UpdateTOTPFieldsAndRevokeSessions: %v", err)
	}

	requireLostTOTPUpgrade(t, repo, user.ID, "reenrolled-ciphertext", true)
}

// testUpgradeTOTPSecretCiphertextCASLosesToADisable is the same race against
// the other writer: a re-encryption landing after a disable must not put a
// secret back into a row whose factor was just turned off.
func testUpgradeTOTPSecretCiphertextCASLosesToADisable(t *testing.T, repo *UserRepository) {
	user := createUpgradeTOTPSecretCiphertextCASUser(t, repo, "totp-disabled@example.com")
	if err := repo.UpdateTOTPFieldsAndRevokeSessions(context.Background(), user.ID, storedSessionVersionForTest(t, repo, user.ID), "", false); err != nil {
		t.Fatalf("UpdateTOTPFieldsAndRevokeSessions: %v", err)
	}

	requireLostTOTPUpgrade(t, repo, user.ID, "", false)
}

func requireLostTOTPUpgrade(t *testing.T, repo *UserRepository, userID uint, wantSecret string, wantEnabled bool) {
	t.Helper()

	applied, err := repo.UpgradeTOTPSecretCiphertextCAS(context.Background(), userID, "legacy-ciphertext", "resealed-legacy-ciphertext")
	if err != nil {
		t.Fatalf("a lost race is not a database failure, got %v", err)
	}
	if applied {
		t.Fatal("the upgrade reported applied against a ciphertext the row no longer holds")
	}

	got, err := repo.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("find after lost upgrade: %v", err)
	}
	if got.TOTPSecret != wantSecret || got.TOTPEnabled != wantEnabled {
		t.Fatalf("totp_secret = %q, totp_enabled = %v after the lost upgrade, want %q, %v kept",
			got.TOTPSecret, got.TOTPEnabled, wantSecret, wantEnabled)
	}
	if got.AuthSessionVersion != 5 {
		t.Fatalf("auth_session_version = %d, want 5: bumped once by the competing write, never by the upgrade", got.AuthSessionVersion)
	}
}

// TestUpgradeTOTPSecretCiphertextCASFailsClosedWhenTheUpdateErrors covers the
// error arm: an upgrade that could not run must report the failure AND read as
// not applied.
func TestUpgradeTOTPSecretCiphertextCASFailsClosedWhenTheUpdateErrors(t *testing.T) {
	repo := openRevealMarkRepoForTest(t)
	user := createUpgradeTOTPSecretCiphertextCASUser(t, repo, "totp-closed@example.com")
	closeRevealMarkRepoHandle(t, repo)

	applied, err := repo.UpgradeTOTPSecretCiphertextCAS(context.Background(), user.ID, "legacy-ciphertext", "resealed-ciphertext")
	if err == nil {
		t.Fatal("expected UpgradeTOTPSecretCiphertextCAS against a closed database to surface an error")
	}
	if applied {
		t.Fatal("an upgrade that errored must never read as applied")
	}
}
