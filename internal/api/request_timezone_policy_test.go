package api

import (
	"strings"
	"testing"
	"time"
)

func TestResolveRequestLocationPrefersValidHeader(t *testing.T) {
	location, cookieValue := resolveRequestLocation("Europe/Moscow", "UTC", time.UTC)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "Europe/Moscow" {
		t.Fatalf("expected header timezone Europe/Moscow, got %q", location.String())
	}
	if cookieValue != "Europe/Moscow" {
		t.Fatalf("expected timezone cookie value Europe/Moscow, got %q", cookieValue)
	}
}

func TestResolveRequestLocationFallsBackToCookieWhenHeaderInvalid(t *testing.T) {
	location, cookieValue := resolveRequestLocation("Europe/Moscow\nInjected", "UTC", time.FixedZone("UTC+3", 3*60*60))
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "UTC" {
		t.Fatalf("expected cookie timezone UTC, got %q", location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no timezone cookie update from invalid header, got %q", cookieValue)
	}
}

func TestResolveRequestLocationAcceptsEncodedCookieTimezone(t *testing.T) {
	location, cookieValue := resolveRequestLocation("", "Europe%2FMoscow", time.UTC)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "Europe/Moscow" {
		t.Fatalf("expected decoded cookie timezone Europe/Moscow, got %q", location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no cookie rewrite for valid cookie fallback, got %q", cookieValue)
	}
}

func TestResolveRequestLocationFallsBackToDefaultWhenNoValidInput(t *testing.T) {
	fallback := time.FixedZone("UTC+4", 4*60*60)
	location, cookieValue := resolveRequestLocation("bad timezone", "also bad", fallback)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != fallback.String() {
		t.Fatalf("expected fallback location %q, got %q", fallback.String(), location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no cookie update on fallback, got %q", cookieValue)
	}
}

func TestResolveRequestLocationUsesUTCFallbackWhenNil(t *testing.T) {
	location, cookieValue := resolveRequestLocation("", "", nil)
	if location == nil {
		t.Fatal("expected non-nil location")
	}
	if location.String() != "UTC" {
		t.Fatalf("expected UTC fallback, got %q", location.String())
	}
	if cookieValue != "" {
		t.Fatalf("expected no cookie update for empty inputs, got %q", cookieValue)
	}
}

func TestParseRequestTimezoneRejectsUnsafeInputs(t *testing.T) {
	if _, _, ok := parseRequestTimezone("Local"); ok {
		t.Fatal("expected Local token to be rejected")
	}
	if _, _, ok := parseRequestTimezone("Europe/Moscow\nInjected"); ok {
		t.Fatal("expected newline-containing timezone to be rejected")
	}

	tooLong := strings.Repeat("A", maxRequestTimezoneLength+1)
	if _, _, ok := parseRequestTimezone(tooLong); ok {
		t.Fatal("expected oversized timezone to be rejected")
	}
}
