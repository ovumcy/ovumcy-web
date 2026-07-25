package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// TestAuthErrorTranslationKeyCoversTOTPChallenge pins the four 2FA error specs
// onto their locale entries. They were absent from the map while the entries had
// existed in all six locales from the start, and translateMessage answers an
// unknown key with the key itself — so a wrong 2FA code showed every user, in
// every language, the literal English spec key "totp invalid code".
func TestAuthErrorTranslationKeyCoversTOTPChallenge(t *testing.T) {
	expected := map[string]string{
		"totp invalid code":      "error.totp_invalid_code",
		"totp session expired":   "error.totp_session_expired",
		"totp internal error":    "error.totp_internal_error",
		"totp too many attempts": "error.totp_too_many_attempts",
	}
	for source, want := range expected {
		if got := AuthErrorTranslationKey(source); got != want {
			t.Fatalf("AuthErrorTranslationKey(%q) = %q, want %q", source, got, want)
		}
	}
}

// TestAuthErrorTranslationKeysResolveInEveryLocale is the guard for the defect
// CLASS, not just the four entries above: a mapping is only worth anything if the
// key it points at actually exists in the locale files. A typo, a renamed locale
// entry, or a new mapping invented without its translations all degrade silently
// to the raw key on screen, which is precisely how the TOTP gap survived.
//
// internal/i18n is imported by this TEST only — the services layer itself keeps
// no dependency on it, and i18n depends on nothing inside the repo, so there is
// no cycle and no production coupling.
func TestAuthErrorTranslationKeysResolveInEveryLocale(t *testing.T) {
	manager, err := i18n.NewManager("en")
	if err != nil {
		t.Fatalf("init i18n manager: %v", err)
	}

	languages := manager.SupportedLanguages()
	if len(languages) == 0 {
		t.Fatal("expected the i18n manager to report supported languages")
	}

	for _, language := range languages {
		messages := manager.Messages(language)
		for source, key := range authErrorTranslationKeys {
			if value, ok := messages[key]; !ok || value == "" {
				t.Errorf("locale %q has no entry for %q (mapped from error spec %q): the message would render as the raw key", language, key, source)
			}
		}
	}
}

func TestAuthErrorTranslationKey(t *testing.T) {
	if got := AuthErrorTranslationKey("  TOO MANY LOGIN ATTEMPTS "); got != "auth.error.too_many_login_attempts" {
		t.Fatalf("expected normalized login attempts key, got %q", got)
	}
	if got := AuthErrorTranslationKey(" too_many_sso_attempts "); got != "auth.error.too_many_sso_attempts" {
		t.Fatalf("expected normalized sso attempts key, got %q", got)
	}
	if got := AuthErrorTranslationKey(" local sign-in unavailable "); got != "auth.error.local_sign_in_unavailable" {
		t.Fatalf("expected local sign-in unavailable key, got %q", got)
	}
	if got := AuthErrorTranslationKey(" local recovery unavailable "); got != "auth.error.local_recovery_unavailable" {
		t.Fatalf("expected local recovery unavailable key, got %q", got)
	}
	if got := AuthErrorTranslationKey(" local password required "); got != "settings.error.local_password_required" {
		t.Fatalf("expected local password required key, got %q", got)
	}
	if got := AuthErrorTranslationKey(" too many requests "); got != "common.error.too_many_requests" {
		t.Fatalf("expected generic rate-limit key, got %q", got)
	}
	if got := AuthErrorTranslationKey(" PERIOD LENGTH IS INCOMPATIBLE WITH CYCLE LENGTH "); got != "settings.cycle.error_incompatible" {
		t.Fatalf("expected settings cycle compatibility key, got %q", got)
	}
	if got := AuthErrorTranslationKey("unknown"); got != "" {
		t.Fatalf("expected empty key for unknown auth error, got %q", got)
	}
}

func TestSettingsStatusTranslationKey(t *testing.T) {
	if got := SettingsStatusTranslationKey("  CYCLE_UPDATED "); got != "settings.success.cycle_updated" {
		t.Fatalf("expected cycle_updated key, got %q", got)
	}
	if got := SettingsStatusTranslationKey(" interface_updated "); got != "settings.success.interface_updated" {
		t.Fatalf("expected interface_updated key, got %q", got)
	}
	if got := SettingsStatusTranslationKey("unknown"); got != "" {
		t.Fatalf("expected empty key for unknown status, got %q", got)
	}
}

func TestBuiltinSymptomTranslationKey(t *testing.T) {
	if got := BuiltinSymptomTranslationKey(" Mood Swings "); got != "symptoms.mood_swings" {
		t.Fatalf("expected mood swings key, got %q", got)
	}
	if got := BuiltinSymptomTranslationKey("Custom"); got != "" {
		t.Fatalf("expected empty key for custom symptom, got %q", got)
	}
}
