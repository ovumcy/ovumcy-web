package services

import (
	"testing"
	"time"
)

func TestResolveAuthErrorSource(t *testing.T) {
	if got := ResolveAuthErrorSource(" invalid credentials "); got != "invalid credentials" {
		t.Fatalf("expected normalized flash error source, got %q", got)
	}
	if got := ResolveAuthErrorSource(" "); got != "" {
		t.Fatalf("expected empty result without flash error, got %q", got)
	}
	if got := ResolveAuthErrorSource(""); got != "" {
		t.Fatalf("expected empty result, got %q", got)
	}
}

func TestResolveAuthPageEmail(t *testing.T) {
	if got := ResolveAuthPageEmail(" Flash@Example.com "); got != "flash@example.com" {
		t.Fatalf("expected normalized flash email, got %q", got)
	}
	if got := ResolveAuthPageEmail("not-email"); got != "" {
		t.Fatalf("expected empty value for invalid flash email, got %q", got)
	}
	if got := ResolveAuthPageEmail(""); got != "" {
		t.Fatalf("expected empty when both invalid, got %q", got)
	}
}

func TestIsResetPasswordTokenValid(t *testing.T) {
	t.Parallel()

	secret := []byte("test-reset-view-secret")
	now := time.Now().UTC()

	validToken, err := BuildPasswordResetToken(secret, 7, "$2a$10$testhashvaluefortokenclaims", 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken returned error: %v", err)
	}

	expiredToken, err := BuildPasswordResetToken(secret, 7, "$2a$10$testhashvaluefortokenclaims", time.Minute, now.Add(-2*time.Minute))
	if err != nil {
		t.Fatalf("BuildPasswordResetToken returned error for expired token: %v", err)
	}

	tests := []struct {
		name  string
		token string
		when  time.Time
		want  bool
	}{
		{name: "valid token", token: validToken, when: now, want: true},
		{name: "invalid token format", token: "invalid-token", when: now, want: false},
		{name: "expired token", token: expiredToken, when: now, want: false},
		{name: "empty token", token: " ", when: now, want: false},
	}

	for _, testCase := range tests {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := IsResetPasswordTokenValid(secret, testCase.token, testCase.when); got != testCase.want {
				t.Fatalf("IsResetPasswordTokenValid(...) = %v, want %v", got, testCase.want)
			}
		})
	}
}
