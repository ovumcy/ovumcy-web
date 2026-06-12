package services

import (
	"errors"
	"strings"
	"testing"
)

func TestValidatePasswordStrength_RejectsWeakPasswords(t *testing.T) {
	testCases := []string{
		"Short1",
		"alllowercase1",
		"ALLUPPERCASE1",
		"NoDigitsHere",
	}

	for _, password := range testCases {
		if err := ValidatePasswordStrength(password); !errors.Is(err, ErrWeakPassword) {
			t.Fatalf("expected ErrWeakPassword for %q, got %v", password, err)
		}
	}
}

func TestValidatePasswordStrength_AcceptsStrongPassword(t *testing.T) {
	if err := ValidatePasswordStrength("StrongPass1"); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// TestValidatePasswordStrength_EnforcesBcryptByteLimit pins the 72-byte
// maximum: bcrypt rejects longer inputs, so the policy must catch them as a
// stable validation error before hashing. The boundary is bytes, not code
// points — a multi-byte passphrase can exceed it with far fewer characters.
func TestValidatePasswordStrength_EnforcesBcryptByteLimit(t *testing.T) {
	atLimit := "Aa1" + strings.Repeat("x", maxPasswordBytes-3)
	if len(atLimit) != maxPasswordBytes {
		t.Fatalf("test setup: at-limit password is %d bytes, want %d", len(atLimit), maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(atLimit); err != nil {
		t.Fatalf("expected exactly-%d-byte password to pass, got %v", maxPasswordBytes, err)
	}

	overLimit := atLimit + "x"
	if err := ValidatePasswordStrength(overLimit); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for %d-byte password, got %v", len(overLimit), err)
	}

	// 35 two-byte Cyrillic runes + ASCII classes = 38 runes but 73 bytes.
	multibyte := "Aa1" + strings.Repeat("ф", 35)
	if got := len(multibyte); got <= maxPasswordBytes {
		t.Fatalf("test setup: multibyte password is %d bytes, want > %d", got, maxPasswordBytes)
	}
	if err := ValidatePasswordStrength(multibyte); !errors.Is(err, ErrWeakPassword) {
		t.Fatalf("expected ErrWeakPassword for multibyte over-limit password, got %v", err)
	}
}
