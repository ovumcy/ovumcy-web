package services

import "testing"

func TestResolveAuthErrorSource(t *testing.T) {
	if got := ResolveAuthErrorSource(" invalid credentials ", "weak password"); got != "invalid credentials" {
		t.Fatalf("expected flash error precedence, got %q", got)
	}
	if got := ResolveAuthErrorSource(" ", " weak password "); got != "weak password" {
		t.Fatalf("expected query fallback, got %q", got)
	}
	if got := ResolveAuthErrorSource("", ""); got != "" {
		t.Fatalf("expected empty result, got %q", got)
	}
}

func TestResolveAuthPageEmail(t *testing.T) {
	if got := ResolveAuthPageEmail(" Flash@Example.com ", "query@example.com"); got != "flash@example.com" {
		t.Fatalf("expected normalized flash email precedence, got %q", got)
	}
	if got := ResolveAuthPageEmail("not-email", " Query@Example.com "); got != "query@example.com" {
		t.Fatalf("expected normalized query fallback, got %q", got)
	}
	if got := ResolveAuthPageEmail("", "not-email"); got != "" {
		t.Fatalf("expected empty when both invalid, got %q", got)
	}
}
