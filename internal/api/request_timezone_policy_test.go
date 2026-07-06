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

// TestIsSafeTimezoneIdentifierCharacterClasses pins the allow-list guarding
// which characters may reach time.LoadLocation. Each row isolates one boundary
// of the switch in isSafeTimezoneIdentifier: a character on a range boundary
// ('a','z','A','Z','0','9') or one of the four literal separators
// ('/','_','+','-') is safe, and a character just outside every allowed set is
// rejected. The mutation report flags every range comparison and separator
// equality here as not-covered, so a shifted boundary or a negated equality
// flips at least one expectation. This is a security guard: it keeps control
// characters and stray separators out of a client-supplied timezone before it
// reaches the stdlib loader.
func TestIsSafeTimezoneIdentifierCharacterClasses(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty is trivially safe", "", true},
		{"lowercase boundaries a and z", "az", true},
		{"uppercase boundaries A and Z", "AZ", true},
		{"digit boundaries 0 and 9", "09", true},
		{"slash separator", "Europe/Moscow", true},
		{"underscore separator", "America/New_York", true},
		{"plus separator", "Etc/GMT+3", true},
		{"minus separator", "Etc/GMT-3", true},
		{"space rejected", "Europe Moscow", false},
		{"dot rejected", "Europe.Moscow", false},
		{"colon just above nine rejected", "GMT:0", false},
		{"at just below uppercase A rejected", "@GMT", false},
		{"bracket just above uppercase Z rejected", "GMT[", false},
		{"backtick just below lowercase a rejected", "gmt`", false},
		{"brace just above lowercase z rejected", "gmt{", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isSafeTimezoneIdentifier(tc.value); got != tc.want {
				t.Fatalf("isSafeTimezoneIdentifier(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
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
