package main

import (
	"path/filepath"
	"strings"
	"testing"
)

// `users create` mints no recovery code under OIDC_LOGIN_MODE=oidc_only
// (WEB-342). The mode is read the way the boot normalizes it, and from those
// two variables alone: a secret or CA file the CLI never uses cannot fail it.
func TestResolveLocalSignInEnabledFollowsTheOIDCLoginMode(t *testing.T) {
	cases := []struct {
		name    string
		enabled string
		mode    string
		want    bool
	}{
		{name: "oidc disabled ignores the mode", enabled: "false", mode: "oidc_only", want: true},
		{name: "oidc unset", enabled: "", mode: "oidc_only", want: true},
		{name: "hybrid default", enabled: "true", mode: "", want: true},
		{name: "blank mode is hybrid", enabled: "true", mode: "   ", want: true},
		{name: "hybrid", enabled: "true", mode: "hybrid", want: true},
		{name: "oidc_only", enabled: "true", mode: "oidc_only", want: false},
		{name: "oidc_only normalized like the boot", enabled: "on", mode: " OIDC_ONLY ", want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OIDC_ENABLED", testCase.enabled)
			t.Setenv("OIDC_LOGIN_MODE", testCase.mode)
			t.Setenv("OIDC_CLIENT_SECRET", "")
			t.Setenv("OIDC_CLIENT_SECRET_FILE", filepath.Join(t.TempDir(), "missing-secret"))

			got, err := resolveLocalSignInEnabled()
			if err != nil {
				t.Fatalf("resolveLocalSignInEnabled: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("expected local sign-in enabled=%v, got %v", testCase.want, got)
			}
		})
	}
}

func TestResolveLocalSignInEnabledRefusesWhatTheBootRefuses(t *testing.T) {
	cases := []struct {
		name    string
		enabled string
		mode    string
		want    string
	}{
		{name: "OIDC_ENABLED typo", enabled: "ture", mode: "oidc_only", want: "OIDC_ENABLED"},
		{name: "unknown mode", enabled: "true", mode: "sso_only", want: "OIDC_LOGIN_MODE"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("OIDC_ENABLED", testCase.enabled)
			t.Setenv("OIDC_LOGIN_MODE", testCase.mode)

			if _, err := resolveLocalSignInEnabled(); err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected a refusal naming %s, got %v", testCase.want, err)
			}
		})
	}
}
