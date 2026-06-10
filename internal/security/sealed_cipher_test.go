package security

import "testing"

func TestSealedCipherRoundTrip(t *testing.T) {
	t.Parallel()

	c, err := NewSecureCookieCipher([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("NewSecureCookieCipher: %v", err)
	}

	plaintext := []byte(`{"hello":"world"}`)
	aad := []byte("ovumcy.cookie.ovumcy_auth")

	payload, err := c.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if len(payload) <= c.NonceSize() {
		t.Fatalf("Seal produced %d bytes, want more than the %d-byte nonce", len(payload), c.NonceSize())
	}

	opened, err := c.Open(payload, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if string(opened) != string(plaintext) {
		t.Fatalf("Open returned %q, want %q", opened, plaintext)
	}
}

func TestSealedCipherRejectsWrongAAD(t *testing.T) {
	t.Parallel()

	c, err := NewSecureCookieCipher([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("NewSecureCookieCipher: %v", err)
	}

	payload, err := c.Seal([]byte("payload"), []byte("aad-a"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if _, err := c.Open(payload, []byte("aad-b")); err == nil {
		t.Fatal("Open with a different aad must fail")
	}
}

// TestSealedCipherPurposeKeySeparation pins the reason the two HKDF label
// pairs exist: the cookie cipher and the field cipher derive DIFFERENT keys
// from the same SECRET_KEY, so a value sealed for one purpose can never be
// opened as the other even when the attacker controls the aad.
func TestSealedCipherPurposeKeySeparation(t *testing.T) {
	t.Parallel()

	secretKey := []byte("test-secret-key")
	cookieCipher, err := NewSecureCookieCipher(secretKey)
	if err != nil {
		t.Fatalf("NewSecureCookieCipher: %v", err)
	}
	fieldCipher, err := newFieldCipher(secretKey)
	if err != nil {
		t.Fatalf("newFieldCipher: %v", err)
	}

	aad := []byte("shared-aad")
	sealedByCookie, err := cookieCipher.Seal([]byte("payload"), aad)
	if err != nil {
		t.Fatalf("cookie Seal: %v", err)
	}

	if _, err := fieldCipher.Open(sealedByCookie, aad); err == nil {
		t.Fatal("field cipher opened a cookie-purpose payload: HKDF label sets no longer separate the derived keys")
	}
}

func TestSealedCipherRejectsTooShortPayload(t *testing.T) {
	t.Parallel()

	c, err := NewSecureCookieCipher([]byte("test-secret-key"))
	if err != nil {
		t.Fatalf("NewSecureCookieCipher: %v", err)
	}

	for size := 0; size <= c.NonceSize(); size++ {
		if _, err := c.Open(make([]byte, size), []byte("aad")); err == nil {
			t.Fatalf("Open must reject a %d-byte payload (nonce size %d)", size, c.NonceSize())
		}
	}
}

func TestSealedCipherRequiresSecretKey(t *testing.T) {
	t.Parallel()

	if _, err := NewSecureCookieCipher(nil); err == nil {
		t.Fatal("NewSecureCookieCipher with empty key must fail")
	}
	if _, err := newFieldCipher([]byte{}); err == nil {
		t.Fatal("newFieldCipher with empty key must fail")
	}
}

func TestSealedCipherNilReceiverFailsClosed(t *testing.T) {
	t.Parallel()

	var c *SealedCipher
	if _, err := c.Seal([]byte("payload"), []byte("aad")); err == nil {
		t.Fatal("Seal on a nil cipher must fail")
	}
	if _, err := c.Open([]byte("payload"), []byte("aad")); err == nil {
		t.Fatal("Open on a nil cipher must fail")
	}
}
