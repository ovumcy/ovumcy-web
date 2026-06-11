package security

// crypto_validation_coverage_test.go — covers reachable error branches that no
// test exercised: the empty-key rejection in sealFieldCiphertext and the
// issuer-URL validation in validateDiscoveredJWKSURI (patch-coverage gaps).

import (
	"strings"
	"testing"
)

func TestSealFieldCiphertextRejectsEmptyKey(t *testing.T) {
	// newFieldCipher requires a secret key; an empty key surfaces as an error
	// from sealFieldCiphertext (field_crypto.go:70-71).
	if _, err := sealFieldCiphertext("plaintext", nil, []byte("aad")); err == nil {
		t.Fatal("sealFieldCiphertext with an empty key must return an error")
	}
}

func TestValidateDiscoveredJWKSURIRejectsInvalidIssuer(t *testing.T) {
	// A valid jwks_uri paired with a non-absolute issuer URL must be rejected
	// (oidc.go:568-569). The jwks_uri passes its own https/absolute check first,
	// so the issuer branch is the one under test.
	err := validateDiscoveredJWKSURI("https://id.example.com/jwks", "issuer-without-scheme")
	if err == nil {
		t.Fatal("validateDiscoveredJWKSURI must reject a non-absolute issuer URL")
	}
	if !strings.Contains(err.Error(), "issuer") {
		t.Fatalf("expected an issuer-URL error, got %v", err)
	}
}
