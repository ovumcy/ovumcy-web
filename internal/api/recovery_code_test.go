package api

import (
	"strings"
	"testing"

	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestGenerateRecoveryCodeFormat(t *testing.T) {
	t.Parallel()

	code, err := services.GenerateRecoveryCode()
	if err != nil {
		t.Fatalf("GenerateRecoveryCode returned error: %v", err)
	}

	if err := services.ValidateRecoveryCodeFormat(code); err != nil {
		t.Fatalf("generated code %q does not match required format: %v", code, err)
	}

	randomPart := strings.TrimPrefix(strings.ReplaceAll(code, "-", ""), recoveryCodePrefix)
	if strings.ContainsAny(randomPart, "IO10") {
		t.Fatalf("generated code %q contains ambiguous characters", code)
	}
}

func TestGenerateRecoveryCodeHash(t *testing.T) {
	t.Parallel()

	code, hash, err := generateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("generateRecoveryCodeHash returned error: %v", err)
	}

	if err := services.ValidateRecoveryCodeFormat(code); err != nil {
		t.Fatalf("generated code %q does not match required format: %v", code, err)
	}
	if strings.TrimSpace(hash) == "" {
		t.Fatal("expected non-empty hash")
	}
}
