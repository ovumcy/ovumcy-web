package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// TestTOTPServiceFactorStatesPinsAllThree pins the three states
// TOTPFactorVerifier must distinguish, DERIVED by attempting decryption on
// every call rather than read from a stored column: not enrolled, enrolled
// and verifiable, and enrolled but unverifiable — the state a SECRET_KEY
// rotation leaves behind, where the stored ciphertext no longer opens under
// the key now in memory. A caller that read only the raw TOTPEnabled column
// cannot tell the second state from the third; this pins that Verifiable and
// Unverifiable do, and that they never both report true (or both false) for
// the same account.
func TestTOTPServiceFactorStatesPinsAllThree(t *testing.T) {
	secretKey := []byte("test-secret-key-32-bytes-padding!")
	rotatedKey := []byte("a-different-secret-key-32-bytes!")
	svc := NewTOTPService(&stubTOTPUserRepo{}, secretKey, nil)

	t.Run("not enrolled", func(t *testing.T) {
		user := models.User{ID: 1}
		if svc.Verifiable(user) {
			t.Fatal("expected an unenrolled account to report NOT verifiable")
		}
		if svc.Unverifiable(user) {
			t.Fatal("expected an unenrolled account to report NOT unverifiable — this is a third, distinct state, not the same as unverifiable")
		}
	})

	t.Run("enrolled and verifiable", func(t *testing.T) {
		encrypted, err := security.EncryptField("JBSWY3DPEHPK3PXP", secretKey, aadForTOTPSecret(2))
		if err != nil {
			t.Fatalf("EncryptField: %v", err)
		}
		user := models.User{ID: 2, TOTPEnabled: true, TOTPSecret: encrypted}
		if !svc.Verifiable(user) {
			t.Fatal("expected an enrolled account with a decryptable secret to report verifiable")
		}
		if svc.Unverifiable(user) {
			t.Fatal("did not expect a verifiable account to also report unverifiable")
		}
	})

	t.Run("enrolled but unverifiable after a SECRET_KEY rotation", func(t *testing.T) {
		rotatedCiphertext, err := security.EncryptField("JBSWY3DPEHPK3PXP", rotatedKey, aadForTOTPSecret(3))
		if err != nil {
			t.Fatalf("EncryptField: %v", err)
		}
		// svc still holds the ORIGINAL secretKey; rotatedCiphertext was sealed
		// under a different one, exactly what a SECRET_KEY rotation leaves in
		// the totp_secret column of every previously-enrolled account.
		user := models.User{ID: 3, TOTPEnabled: true, TOTPSecret: rotatedCiphertext}
		if svc.Verifiable(user) {
			t.Fatal("expected an account whose secret does not decrypt under the current SECRET_KEY to report NOT verifiable")
		}
		if !svc.Unverifiable(user) {
			t.Fatal("expected an account whose secret does not decrypt under the current SECRET_KEY to report unverifiable")
		}
	})

	t.Run("enrolled with an empty stored secret", func(t *testing.T) {
		// Should not happen through EnableTOTP/DisableTOTP, which always write
		// both columns together, but totpFactorState must not assume it is
		// impossible: treat it as unverifiable rather than panicking or
		// reporting verifiable.
		user := models.User{ID: 4, TOTPEnabled: true, TOTPSecret: ""}
		if svc.Verifiable(user) {
			t.Fatal("expected an enrolled account with an empty stored secret to report NOT verifiable")
		}
		if !svc.Unverifiable(user) {
			t.Fatal("expected an enrolled account with an empty stored secret to report unverifiable")
		}
	})
}
