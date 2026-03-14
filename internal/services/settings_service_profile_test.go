package services

import (
	"errors"
	"strings"
	"testing"
)

func TestNormalizeDisplayName_TrimAndEmptyAllowed(t *testing.T) {
	service := NewSettingsService(nil)

	displayName, err := service.NormalizeDisplayName("  Maya  ")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if displayName != "Maya" {
		t.Fatalf("expected Maya, got %q", displayName)
	}

	emptyName, err := service.NormalizeDisplayName("   ")
	if err != nil {
		t.Fatalf("expected nil error for empty name, got %v", err)
	}
	if emptyName != "" {
		t.Fatalf("expected empty normalized name, got %q", emptyName)
	}
}

func TestNormalizeDisplayName_RejectsTooLongName(t *testing.T) {
	service := NewSettingsService(nil)
	tooLong := strings.Repeat("a", 65)

	_, err := service.NormalizeDisplayName(tooLong)
	if !errors.Is(err, ErrSettingsDisplayNameTooLong) {
		t.Fatalf("expected ErrSettingsDisplayNameTooLong, got %v", err)
	}
}

func TestNormalizeDisplayName_RejectsMarkupLikeName(t *testing.T) {
	service := NewSettingsService(nil)

	_, err := service.NormalizeDisplayName("<script>alert('xss')</script>")
	if !errors.Is(err, ErrSettingsDisplayNameInvalidCharacters) {
		t.Fatalf("expected ErrSettingsDisplayNameInvalidCharacters, got %v", err)
	}
}

func TestResolveProfileUpdateStatus(t *testing.T) {
	service := NewSettingsService(nil)

	if got := service.ResolveProfileUpdateStatus("", "Maya"); got != "profile_updated" {
		t.Fatalf("expected profile_updated, got %q", got)
	}
	if got := service.ResolveProfileUpdateStatus("Maya", ""); got != "profile_name_cleared" {
		t.Fatalf("expected profile_name_cleared, got %q", got)
	}
}
