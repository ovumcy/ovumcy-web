package services

import (
	"errors"
	"testing"
)

func TestNormalizeAuthEmail(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "normalizes case and spaces", raw: " USER@EXAMPLE.COM ", want: "user@example.com"},
		{name: "invalid email returns empty", raw: "not-email", want: ""},
		{name: "empty returns empty", raw: "   ", want: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := NormalizeAuthEmail(testCase.raw); got != testCase.want {
				t.Fatalf("NormalizeAuthEmail(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

func TestNormalizeCredentialsInput(t *testing.T) {
	email, password, err := NormalizeCredentialsInput(" USER@EXAMPLE.COM ", "  StrongPass1  ")
	if err != nil {
		t.Fatalf("expected valid credentials input, got %v", err)
	}
	if email != "user@example.com" {
		t.Fatalf("expected normalized email, got %q", email)
	}
	if password != "StrongPass1" {
		t.Fatalf("expected trimmed password, got %q", password)
	}

	_, _, err = NormalizeCredentialsInput("not-email", "StrongPass1")
	if !errors.Is(err, ErrAuthCredentialsInvalid) {
		t.Fatalf("expected ErrAuthCredentialsInvalid for invalid email, got %v", err)
	}

	_, _, err = NormalizeCredentialsInput("user@example.com", " ")
	if !errors.Is(err, ErrAuthCredentialsInvalid) {
		t.Fatalf("expected ErrAuthCredentialsInvalid for empty password, got %v", err)
	}
}

func TestValidateRecoveryCodeFormat(t *testing.T) {
	if err := ValidateRecoveryCodeFormat("OVUM-ABCD-2345-EFGH"); err != nil {
		t.Fatalf("expected valid recovery code format, got %v", err)
	}
	if err := ValidateRecoveryCodeFormat("OVUM-INVALID"); !errors.Is(err, ErrAuthRecoveryCodeInvalid) {
		t.Fatalf("expected ErrAuthRecoveryCodeInvalid, got %v", err)
	}
}

func TestNormalizeForgotPasswordCode(t *testing.T) {
	code, err := NormalizeForgotPasswordCode("  OVUM-ABCD-2345-EFGH ")
	if err != nil {
		t.Fatalf("expected valid code, got %v", err)
	}
	if code != "OVUM-ABCD-2345-EFGH" {
		t.Fatalf("expected trimmed code, got %q", code)
	}

	_, err = NormalizeForgotPasswordCode("   ")
	if !errors.Is(err, ErrAuthRecoveryCodeInvalid) {
		t.Fatalf("expected ErrAuthRecoveryCodeInvalid for empty input, got %v", err)
	}
}

func TestNormalizeResetPasswordInput(t *testing.T) {
	password, confirm, err := NormalizeResetPasswordInput("  StrongPass1 ", " StrongPass1  ")
	if err != nil {
		t.Fatalf("expected valid reset input, got %v", err)
	}
	if password != "StrongPass1" || confirm != "StrongPass1" {
		t.Fatalf("expected trimmed password pair, got %q/%q", password, confirm)
	}

	_, _, err = NormalizeResetPasswordInput(" ", "StrongPass1")
	if !errors.Is(err, ErrAuthResetInputInvalid) {
		t.Fatalf("expected ErrAuthResetInputInvalid for empty password, got %v", err)
	}
	_, _, err = NormalizeResetPasswordInput("StrongPass1", " ")
	if !errors.Is(err, ErrAuthResetInputInvalid) {
		t.Fatalf("expected ErrAuthResetInputInvalid for empty confirm, got %v", err)
	}
}
