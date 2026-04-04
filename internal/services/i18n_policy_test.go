package services

import "testing"

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
