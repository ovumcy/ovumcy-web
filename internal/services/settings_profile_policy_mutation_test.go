package services

import (
	"strings"
	"testing"
)

func TestNormalizeDisplayName_AcceptsNameAtMaxLengthBoundary(t *testing.T) {
	service := NewSettingsService(nil)
	atMax := strings.Repeat("a", 64)

	displayName, err := service.NormalizeDisplayName(atMax)
	if err != nil {
		t.Fatalf("expected nil error for 64-rune name, got %v", err)
	}
	if displayName != atMax {
		t.Fatalf("expected 64-rune name to be returned unchanged, got %q", displayName)
	}
}
