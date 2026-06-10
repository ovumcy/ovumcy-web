package security

import "testing"

// These fixtures were produced by the pre-consolidation EncryptField (the
// revision with its own inline HKDF + AES-256-GCM) under the fixed key below.
// They pin the persisted users.totp_secret format end to end: the HKDF
// salt/info labels, the derived key, the per-row AAD binding, the
// nonce||ciphertext layout, and the base64url encoding.
//
// If either fixture stops decrypting, key derivation or framing has changed
// and every 2FA-enabled account's stored TOTP secret would fail to decrypt on
// the next challenge. Do not regenerate these values to make the test pass;
// fix the regression instead.
const (
	goldenFieldSecretKey = "golden-backcompat-secret-key-0123456789"
	goldenFieldPlaintext = "JBSWY3DPEHPK3PXP"
	goldenFieldAAD       = "ovumcy.field.totp_secret:42"

	// Sealed with aad=goldenFieldAAD (the current bound format).
	goldenFieldCiphertextAAD = "w-LF1h9uw_eMjPo9Gthl8tKXL0itkoZhOTktK-Q8RGWu9iXlGRiLbQUKixs"
	// Sealed with nil aad (the legacy unbound format still readable via the
	// DecryptField fallback path).
	goldenFieldCiphertextLegacy = "qqd8hcE4OAzAXieRHELQMKshSBcewFcPLCu4Lb0CCOv_BV1UQuUNHSJyTLE"
)

func TestDecryptFieldOpensPreConsolidationGoldenCiphertexts(t *testing.T) {
	t.Parallel()

	key := []byte(goldenFieldSecretKey)
	aad := []byte(goldenFieldAAD)

	plaintext, isLegacy, err := DecryptField(goldenFieldCiphertextAAD, key, aad)
	if err != nil {
		t.Fatalf("golden aad-bound ciphertext no longer decrypts: %v", err)
	}
	if plaintext != goldenFieldPlaintext {
		t.Fatalf("golden aad-bound ciphertext decrypted to %q, want %q", plaintext, goldenFieldPlaintext)
	}
	if isLegacy {
		t.Fatal("golden aad-bound ciphertext reported isLegacy=true, want false")
	}

	plaintext, isLegacy, err = DecryptField(goldenFieldCiphertextLegacy, key, aad)
	if err != nil {
		t.Fatalf("golden legacy ciphertext no longer decrypts via the fallback path: %v", err)
	}
	if plaintext != goldenFieldPlaintext {
		t.Fatalf("golden legacy ciphertext decrypted to %q, want %q", plaintext, goldenFieldPlaintext)
	}
	if !isLegacy {
		t.Fatal("golden legacy ciphertext reported isLegacy=false, want true")
	}
}
