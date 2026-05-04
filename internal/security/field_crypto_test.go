package security

import (
	"strings"
	"testing"
)

func TestEncryptDecryptField_RoundTrip(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	plaintext := "JBSWY3DPEHPK3PXP"

	encrypted, err := EncryptField(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptField() error: %v", err)
	}
	if encrypted == plaintext {
		t.Fatal("EncryptField() returned plaintext unchanged")
	}

	decrypted, err := DecryptField(encrypted, key)
	if err != nil {
		t.Fatalf("DecryptField() error: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("round-trip mismatch: got %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptField_ProducesDistinctCiphertexts(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	plaintext := "JBSWY3DPEHPK3PXP"

	first, err := EncryptField(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptField() first error: %v", err)
	}
	second, err := EncryptField(plaintext, key)
	if err != nil {
		t.Fatalf("EncryptField() second error: %v", err)
	}
	if first == second {
		t.Fatal("EncryptField() produced identical ciphertexts for same input (nonce re-use)")
	}
}

func TestDecryptField_WrongKey(t *testing.T) {
	key1 := []byte("a-sufficiently-long-test-secret!")
	key2 := []byte("a-different-sufficiently-long-k!")

	encrypted, err := EncryptField("mysecret", key1)
	if err != nil {
		t.Fatalf("EncryptField() error: %v", err)
	}

	_, err = DecryptField(encrypted, key2)
	if err == nil {
		t.Fatal("DecryptField() with wrong key should return an error")
	}
}

func TestDecryptField_TamperedCiphertext(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")

	encrypted, err := EncryptField("mysecret", key)
	if err != nil {
		t.Fatalf("EncryptField() error: %v", err)
	}

	tampered := strings.ToUpper(encrypted)
	if tampered == encrypted {
		t.Skip("tampering had no effect on this input (coincidence)")
	}
	_, err = DecryptField(tampered, key)
	if err == nil {
		t.Fatal("DecryptField() with tampered ciphertext should return an error")
	}
}

func TestEncryptField_EmptyKey(t *testing.T) {
	_, err := EncryptField("plaintext", []byte{})
	if err == nil {
		t.Fatal("EncryptField() with empty key should return an error")
	}
}

func TestDecryptField_EmptyKey(t *testing.T) {
	_, err := DecryptField("somevalue", []byte{})
	if err == nil {
		t.Fatal("DecryptField() with empty key should return an error")
	}
}

func TestDecryptField_InvalidBase64(t *testing.T) {
	key := []byte("a-sufficiently-long-test-secret!")
	_, err := DecryptField("not!valid!base64!!", key)
	if err == nil {
		t.Fatal("DecryptField() with invalid base64 should return an error")
	}
}
