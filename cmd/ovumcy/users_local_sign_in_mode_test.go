package main

import (
	"strings"
	"testing"
)

// `users create` mints no recovery code under OIDC_LOGIN_MODE=oidc_only
// (WEB-342); the mode is read through the boot's own OIDC resolver.
func TestResolveLocalSignInEnabledFollowsTheOIDCLoginMode(t *testing.T) {
	t.Run("oidc disabled", func(t *testing.T) {
		t.Setenv("OIDC_ENABLED", "false")
		t.Setenv("OIDC_LOGIN_MODE", "oidc_only")
		assertLocalSignInEnabled(t, true)
	})
	t.Run("hybrid", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("COOKIE_SECURE", "true")
		t.Setenv("OIDC_LOGIN_MODE", "hybrid")
		assertLocalSignInEnabled(t, true)
	})
	t.Run("oidc_only", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("COOKIE_SECURE", "true")
		t.Setenv("OIDC_LOGIN_MODE", "oidc_only")
		assertLocalSignInEnabled(t, false)
	})
	t.Run("config the boot refuses", func(t *testing.T) {
		setValidOIDCTestEnv(t)
		t.Setenv("COOKIE_SECURE", "false")
		t.Setenv("OIDC_LOGIN_MODE", "oidc_only")
		if _, err := resolveLocalSignInEnabled(); err == nil || !strings.Contains(err.Error(), "COOKIE_SECURE") {
			t.Fatalf("expected the boot's COOKIE_SECURE refusal, got %v", err)
		}
	})
}

func assertLocalSignInEnabled(t *testing.T, want bool) {
	t.Helper()

	got, err := resolveLocalSignInEnabled()
	if err != nil {
		t.Fatalf("resolveLocalSignInEnabled: %v", err)
	}
	if got != want {
		t.Fatalf("expected local sign-in enabled=%v, got %v", want, got)
	}
}
