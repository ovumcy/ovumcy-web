package security

import (
	"testing"
)

// encryptFieldLegacyForTest seals a value in the legacy no-aad format that
// earlier Ovumcy versions wrote to disk, so the fallback path in DecryptField
// has a fixture to open. It goes through the same sealFieldCiphertext the
// shipping EncryptField calls, with the aad EncryptField itself refuses —
// this package's tests can reach it directly, and no production door for the
// unbound format exists or should exist.
func encryptFieldLegacyForTest(t *testing.T, secretKey []byte, plaintext string) string {
	t.Helper()
	encoded, err := sealFieldCiphertext(plaintext, secretKey, nil)
	if err != nil {
		t.Fatalf("sealFieldCiphertext with nil aad: %v", err)
	}
	return encoded
}
