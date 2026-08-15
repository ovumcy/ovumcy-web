package services

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/pquerna/otp/totp"
)

func nowForTOTPTest() time.Time { return time.Now() }

// A users.totp_secret ciphertext as a pre-aad Ovumcy revision actually wrote
// it: sealed with nil aad, base64url-framed, recorded rather than re-derived.
// The same bytes are the legacy half of the back-compat fixture in
// internal/security/field_crypto_golden_test.go, which pins the HKDF labels
// and the nonce||ciphertext layout; they are repeated here because a test
// fixture in one package's _test.go is invisible to another package. If this
// value stops opening, key derivation or framing has changed and every
// 2FA-enabled account upgraded from that revision would lose their second
// factor — fix the regression, do not regenerate the constant.
const (
	legacyTOTPSecretKey  = "golden-backcompat-secret-key-0123456789"
	legacyTOTPRawSecret  = "JBSWY3DPEHPK3PXP"
	legacyTOTPCiphertext = "qqd8hcE4OAzAXieRHELQMKshSBcewFcPLCu4Lb0CCOv_BV1UQuUNHSJyTLE"
	legacyTOTPOwnerID    = 42
)

// TestTOTPService_ValidateCode_RejectsCiphertextFromAnotherUser is the
// runtime contract test for Finding #2 (encrypted TOTP secret was not
// bound to the user_id). Before aad binding, an attacker with database
// write privilege could substitute user A's totp_secret ciphertext into
// user B's row and pass 2FA for B using their own authenticator. After
// the fix, EncryptField binds the ciphertext to aad="ovumcy.field.totp_secret:<userID>"
// and DecryptField under a different aad fails to open.
func TestTOTPService_ValidateCode_RejectsCiphertextFromAnotherUser(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	svc := NewTOTPService(repo, secretKey, nil)

	// Enroll user 1 and capture their persisted ciphertext.
	keyOne, err := svc.GenerateSetupKey("Ovumcy", "user1@example.com")
	if err != nil {
		t.Fatalf("GenerateSetupKey user 1: %v", err)
	}
	if err := svc.EnableTOTP(context.Background(), 1, keyOne.Secret()); err != nil {
		t.Fatalf("EnableTOTP user 1: %v", err)
	}
	user1Ciphertext := repo.updatedSecret

	// A code derived from user 1's authenticator is what the attacker has.
	user1Code, err := totp.GenerateCode(keyOne.Secret(), nowForTOTPTest())
	if err != nil {
		t.Fatalf("GenerateCode user 1: %v", err)
	}

	// Validating user 1 with user 1's ciphertext + user 1's code succeeds.
	if valid, err := svc.ValidateCode(context.Background(), 1, user1Ciphertext, user1Code); err != nil || !valid {
		t.Fatalf("baseline: ValidateCode(1, user1Ciphertext, user1Code) = (%v, %v), want (true, nil)", valid, err)
	}

	// Now simulate the cross-row swap: user 2's row was rewritten by an
	// attacker to carry user 1's ciphertext. With aad binding, DecryptField
	// fails to open the ciphertext under aad="ovumcy.field.totp_secret:2",
	// so ValidateCode for user 2 must surface a decrypt error and refuse
	// the login. The attacker's TOTP code, which would have been valid for
	// user 1, must not be acceptable for user 2.
	user1FreshCode, err := totp.GenerateCode(keyOne.Secret(), nowForTOTPTest())
	if err != nil {
		t.Fatalf("GenerateCode user 1 fresh: %v", err)
	}
	valid, err := svc.ValidateCode(context.Background(), 2, user1Ciphertext, user1FreshCode)
	if err == nil {
		t.Fatal("ValidateCode(2, user1Ciphertext, ...) must return a decrypt error after the aad swap is rejected")
	}
	if valid {
		t.Error("ValidateCode accepted a cross-user ciphertext swap; aad binding is not enforced")
	}
}

// TestTOTPService_ValidateCode_LegacyCiphertextReencrypts captures the
// migration contract: a TOTP secret stored before aad binding (i.e. with
// aead.Seal(..., nil)) must still let the user log in, AND the service
// must transparently re-encrypt the secret under the new aad-bound format
// without bumping auth_session_version (the user did not just change
// their security posture — they just logged in).
func TestTOTPService_ValidateCode_LegacyCiphertextReencrypts(t *testing.T) {
	repo := &stubTOTPUserRepo{}
	secretKey := []byte(legacyTOTPSecretKey)
	svc := NewTOTPService(repo, secretKey, nil)

	code, err := totp.GenerateCode(legacyTOTPRawSecret, nowForTOTPTest())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	valid, err := svc.ValidateCode(context.Background(), legacyTOTPOwnerID, legacyTOTPCiphertext, code)
	if err != nil {
		t.Fatalf("ValidateCode on legacy ciphertext: %v", err)
	}
	if !valid {
		t.Fatal("ValidateCode rejected a legitimate legacy ciphertext")
	}

	if !repo.reencryptCalled {
		t.Fatal("ValidateCode succeeded on legacy ciphertext but did not invoke UpdateTOTPSecretCiphertext for re-encryption")
	}
	if repo.reencryptedUserID != legacyTOTPOwnerID {
		t.Errorf("re-encrypt called for userID=%d, want %d", repo.reencryptedUserID, legacyTOTPOwnerID)
	}
	// The re-encrypted value MUST be aad-bound. The simplest way to assert
	// this is to verify it opens cleanly under the expected aad and reports
	// isLegacy=false.
	got, isLegacy, err := security.DecryptField(repo.reencryptedCiphertext, secretKey, aadForTOTPSecret(legacyTOTPOwnerID))
	if err != nil {
		t.Fatalf("re-encrypted ciphertext failed to decrypt under new aad: %v", err)
	}
	if isLegacy {
		t.Fatal("re-encrypted ciphertext is still in legacy (no-aad) form")
	}
	if got != legacyTOTPRawSecret {
		t.Fatalf("re-encrypted plaintext mismatch: got %q, want %q", got, legacyTOTPRawSecret)
	}

	if repo.updateTOTPCalled {
		t.Fatal("lazy re-encryption must not call UpdateTOTPFieldsAndRevokeSessions (would bump auth_session_version and sign the user out)")
	}
}
