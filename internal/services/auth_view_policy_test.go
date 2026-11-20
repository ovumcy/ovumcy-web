package services

import (
	"testing"
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
