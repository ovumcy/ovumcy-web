package security

import (
	"errors"
	"strings"
	"testing"
)

const (
	macTestSelector = "ABCDEFGHJKLMNPQR"
	macTestVerifier = "STUVWXYZ23456789ABCDEFGHJKLMNPQR"
)

func TestCalendarFeedVerifierMACRoundTrips(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	mac, err := CalendarFeedVerifierMAC(key, macTestSelector, macTestVerifier)
	if err != nil {
		t.Fatalf("derive mac: %v", err)
	}
	if mac == "" {
		t.Fatal("expected a non-empty mac")
	}
	if strings.Contains(mac, macTestVerifier) {
		t.Fatal("mac must not embed the verifier plaintext")
	}
	if !VerifyCalendarFeedVerifierMAC(key, macTestSelector, macTestVerifier, mac) {
		t.Fatal("expected the freshly derived mac to verify")
	}
}

func TestCalendarFeedVerifierMACIsDeterministic(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")

	first, err := CalendarFeedVerifierMAC(key, macTestSelector, macTestVerifier)
	if err != nil {
		t.Fatalf("derive first mac: %v", err)
	}
	second, err := CalendarFeedVerifierMAC(key, macTestSelector, macTestVerifier)
	if err != nil {
		t.Fatalf("derive second mac: %v", err)
	}
	// Unlike bcrypt this is deterministic by design: the lookup is by selector,
	// so no per-row salt is needed, and determinism is what makes verification a
	// constant-time compare instead of a work-factor computation.
	if first != second {
		t.Fatalf("expected a deterministic mac, got %q and %q", first, second)
	}
}

// TestCalendarFeedVerifierMACRejectsWrongInputs is the core security property:
// the MAC authenticates the (selector, verifier) PAIR, so neither half can be
// swapped for another row's value.
func TestCalendarFeedVerifierMACRejectsWrongInputs(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	mac, err := CalendarFeedVerifierMAC(key, macTestSelector, macTestVerifier)
	if err != nil {
		t.Fatalf("derive mac: %v", err)
	}

	tests := map[string]struct {
		key      []byte
		selector string
		verifier string
	}{
		"wrong verifier":   {key: key, selector: macTestSelector, verifier: "WRONGWXYZ23456789ABCDEFGHJKLMNPQ"},
		"wrong selector":   {key: key, selector: "ZZZZZZZZZZZZZZZZ", verifier: macTestVerifier},
		"rotated key":      {key: []byte("fedcba9876543210fedcba9876543210"), selector: macTestSelector, verifier: macTestVerifier},
		"empty verifier":   {key: key, selector: macTestSelector, verifier: ""},
		"empty selector":   {key: key, selector: "", verifier: macTestVerifier},
		"halves swapped":   {key: key, selector: macTestVerifier, verifier: macTestSelector},
		"missing key":      {key: nil, selector: macTestSelector, verifier: macTestVerifier},
		"shifted boundary": {key: key, selector: macTestSelector + "S", verifier: macTestVerifier[1:]},
	}

	for name, testCase := range tests {
		t.Run(name, func(t *testing.T) {
			if VerifyCalendarFeedVerifierMAC(testCase.key, testCase.selector, testCase.verifier, mac) {
				t.Fatalf("%s: expected verification to fail", name)
			}
		})
	}
}

// TestCalendarFeedVerifierMACRejectsEmptyStoredValue guards the migration path:
// a row that has not been upgraded yet carries an empty MAC column, and that
// must never verify — otherwise an un-migrated row would accept any token.
func TestCalendarFeedVerifierMACRejectsEmptyStoredValue(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	if VerifyCalendarFeedVerifierMAC(key, macTestSelector, macTestVerifier, "") {
		t.Fatal("an empty stored mac must never verify")
	}
}

func TestCalendarFeedVerifierMACRequiresKey(t *testing.T) {
	if _, err := CalendarFeedVerifierMAC(nil, macTestSelector, macTestVerifier); !errors.Is(err, ErrCalendarFeedMACKeyMissing) {
		t.Fatalf("expected ErrCalendarFeedMACKeyMissing, got %v", err)
	}
	if _, err := CalendarFeedVerifierMAC([]byte{}, macTestSelector, macTestVerifier); !errors.Is(err, ErrCalendarFeedMACKeyMissing) {
		t.Fatalf("expected ErrCalendarFeedMACKeyMissing for an empty key, got %v", err)
	}
}
