package security

import (
	"strings"
	"testing"
)

func TestEncryptDecryptField_RoundTrip(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	aad := []byte("ovumcy.field.test:42")
	plaintext := "JBSWY3DPEHPK3PXP"

	encrypted, err := EncryptField(plaintext, key, aad)
	if err != nil {
		t.Fatalf("EncryptField() error: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("EncryptField() returned plaintext unchanged")
	}

	decrypted, isLegacy, err := DecryptField(encrypted, key, aad)
	if err != nil {
		t.Fatalf("DecryptField() error: %v", err)
	}
	if isLegacy {
		t.Fatal("freshly encrypted payload must not report isLegacy=true")
	}
	if decrypted != plaintext {
		t.Fatalf("round-trip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptField_ProducesDistinctCiphertexts(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	aad := []byte("ovumcy.field.test:42")
	plaintext := "JBSWY3DPEHPK3PXP"

	first, err := EncryptField(plaintext, key, aad)
	if err != nil {
		t.Fatalf("EncryptField() first error: %v", err)
	}
	second, err := EncryptField(plaintext, key, aad)
	if err != nil {
		t.Fatalf("EncryptField() second error: %v", err)
	}
	if first == second {
		t.Fatal("EncryptField() produced identical ciphertexts for same input (nonce re-use)")
	}
}

func TestDecryptField_WrongKey(t *testing.T) {
	aad := []byte("ovumcy.field.test:42")
	key1 := []byte("a-sufficiently-long-test-secret!")
	key2 := []byte("a-different-sufficiently-long-k!")

	encrypted, err := EncryptField("mysecret", key1, aad)
	if err != nil {
		t.Fatalf("EncryptField() error: %v", err)
	}

	_, _, err = DecryptField(encrypted, key2, aad)
	if err == nil {
		t.Fatal("DecryptField() with wrong key should return an error")
	}
}

func TestDecryptField_TamperedCiphertext(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	aad := []byte("ovumcy.field.test:42")

	encrypted, err := EncryptField("mysecret", key, aad)
	if err != nil {
		t.Fatalf("EncryptField() error: %v", err)
	}

	tampered := strings.ToUpper(encrypted)
	if tampered == encrypted {
		t.Skip("tampering had no effect on this input (coincidence)")
	}
	_, _, err = DecryptField(tampered, key, aad)
	if err == nil {
		t.Fatal("DecryptField() with tampered ciphertext should return an error")
	}
}

func TestEncryptField_EmptyKey(t *testing.T) {
	aad := []byte("ovumcy.field.test:42")
	_, err := EncryptField("plaintext", []byte{}, aad)
	if err == nil {
		t.Fatal("EncryptField() with empty key should return an error")
	}
}

// TestEncryptField_EmptyAAD captures the contract that EncryptField refuses
// to seal under nil/empty aad, since that would re-introduce the cross-row
// swap vector this revision exists to close.
func TestEncryptField_EmptyAAD(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	if _, err := EncryptField("plaintext", key, nil); err == nil {
		t.Fatal("EncryptField() with nil aad must fail")
	}
	if _, err := EncryptField("plaintext", key, []byte{}); err == nil {
		t.Fatal("EncryptField() with empty aad must fail")
	}
}

func TestDecryptField_EmptyKey(t *testing.T) {
	aad := []byte("ovumcy.field.test:42")
	_, _, err := DecryptField("somevalue", []byte{}, aad)
	if err == nil {
		t.Fatal("DecryptField() with empty key should return an error")
	}
}

func TestDecryptField_InvalidBase64(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	aad := []byte("ovumcy.field.test:42")
	_, _, err := DecryptField("not!valid!base64!!", key, aad)
	if err == nil {
		t.Fatal("DecryptField() with invalid base64 should return an error")
	}
}

// TestDecryptField_RejectsWrongAAD is the load-bearing contract test for
// Finding #2: a ciphertext sealed under one aad cannot be opened under a
// different aad, even with the correct key. Without aad binding, an
// attacker with database write privilege could lift one user's encrypted
// TOTP secret into another user's row and pass 2FA with their own code.
func TestDecryptField_RejectsWrongAAD(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	aadA := []byte("ovumcy.field.totp_secret:1")
	aadB := []byte("ovumcy.field.totp_secret:2")

	encrypted, err := EncryptField("JBSWY3DPEHPK3PXP", key, aadA)
	if err != nil {
		t.Fatalf("EncryptField() error: %v", err)
	}
	if _, _, err := DecryptField(encrypted, key, aadB); err == nil {
		t.Fatal("DecryptField() must reject a ciphertext sealed under a different aad")
	}
}

// TestDecryptField_LegacyFallback captures the migration contract: a
// ciphertext that was sealed BEFORE aad binding was added (i.e. with
// aead.Seal(..., nil)) must still open under the new function, and the
// isLegacy flag must be set so the caller knows to re-encrypt the value.
// Without this fallback, every account whose TOTP was enabled before the
// upgrade would lose their 2FA on the first post-upgrade login.
func TestDecryptField_LegacyFallback(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	legacyCiphertext := encryptFieldLegacyForTest(t, key, "JBSWY3DPEHPK3PXP")

	plaintext, isLegacy, err := DecryptField(legacyCiphertext, key, []byte("ovumcy.field.totp_secret:42"))
	if err != nil {
		t.Fatalf("DecryptField() error on legacy ciphertext: %v", err)
	}
	if plaintext != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("legacy plaintext mismatch: got %q", plaintext)
	}
	if !isLegacy {
		t.Fatal("isLegacy must be true when opening a pre-aad ciphertext via the fallback path")
	}
}

// TestDecryptField_RejectsTooShortCiphertext pins the length-boundary guard: a
// payload shorter than the GCM nonce can never carry a valid nonce+tag and must
// be rejected with an error rather than panicking on an out-of-range slice. The
// inputs are valid base64url so they reach the length check (not the decode
// guard).
func TestDecryptField_RejectsTooShortCiphertext(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	aad := []byte("ovumcy.field.test:42")
	for _, short := range []string{"", "AA", "AAAA"} {
		if _, _, err := DecryptField(short, key, aad); err == nil {
			t.Fatalf("DecryptField(%q) must reject a too-short ciphertext", short)
		}
	}
}
