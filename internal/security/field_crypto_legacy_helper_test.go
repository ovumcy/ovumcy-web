package security

import (
	"testing"
)

// encryptFieldLegacyForTest is a thin same-package wrapper around the
// cross-package test helper EncryptFieldNoAADForTest. It exists so the
// other test functions in this file can stay readable without the longer
// public-API name.
func encryptFieldLegacyForTest(t *testing.T, secretKey []byte, plaintext string) string {
	t.Helper()
	encoded, err := EncryptFieldNoAADForTest(plaintext, secretKey)
	if err != nil {
		t.Fatalf("EncryptFieldNoAADForTest: %v", err)
	}
	return encoded
}
