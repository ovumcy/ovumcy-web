package bootstrap

import (
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// TestI18nDisclaimerProviderReturnsLocalizedString proves the adapter the notify
// pass uses resolves the medical-safety disclaimer for a supported language, and
// falls back to the default language for an unknown one (Messages merges the
// default over the target).
func TestI18nDisclaimerProviderReturnsLocalizedString(t *testing.T) {
	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("new i18n manager: %v", err)
	}
	provider := i18nDisclaimerProvider{manager: manager}

	english := provider.Disclaimer("en")
	if !strings.Contains(english, "not medical advice or a method of contraception") {
		t.Fatalf("English disclaimer not resolved, got %q", english)
	}

	// An unknown language falls back to the default (non-empty) disclaimer.
	fallback := provider.Disclaimer("zz")
	if fallback == "" {
		t.Fatal("unknown language should fall back to the default disclaimer, got empty")
	}

	// A supported non-default language resolves its own localized string.
	russian := provider.Disclaimer("ru")
	if russian == "" {
		t.Fatal("Russian disclaimer should resolve to a non-empty string")
	}
}

// TestI18nDisclaimerProviderNilManager proves the nil-manager guard: with no
// manager the adapter returns an empty string rather than panicking.
func TestI18nDisclaimerProviderNilManager(t *testing.T) {
	provider := i18nDisclaimerProvider{manager: nil}
	if got := provider.Disclaimer("en"); got != "" {
		t.Fatalf("nil-manager adapter should return empty, got %q", got)
	}
}
