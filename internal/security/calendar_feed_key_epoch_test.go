package security

import (
	"encoding/hex"
	"errors"
	"testing"
)

// TestCalendarFeedKeyEpochDeterministicAndKeySeparated pins the two properties
// the boot sentinel relies on: the epoch is a pure function of the secret key
// (same key → same value across boots), and two different keys can never share
// an epoch (a rotation is always visible as a mismatch).
func TestCalendarFeedKeyEpochDeterministicAndKeySeparated(t *testing.T) {
	keyA := []byte("epoch-test-secret-key-A-0123456789")
	keyB := []byte("epoch-test-secret-key-B-0123456789")

	first, err := CalendarFeedKeyEpoch(keyA)
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch(keyA): %v", err)
	}
	second, err := CalendarFeedKeyEpoch(keyA)
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch(keyA) second call: %v", err)
	}
	if first != second {
		t.Fatalf("epoch must be deterministic for one key: %q vs %q", first, second)
	}

	other, err := CalendarFeedKeyEpoch(keyB)
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch(keyB): %v", err)
	}
	if other == first {
		t.Fatal("two different secret keys derived the same epoch — a rotation would go undetected")
	}

	raw, err := hex.DecodeString(first)
	if err != nil {
		t.Fatalf("epoch is not lowercase hex: %v", err)
	}
	if len(raw) != 32 {
		t.Fatalf("epoch must be a 32-byte HMAC-SHA256 output, got %d bytes", len(raw))
	}
}

// TestCalendarFeedKeyEpochGoldenPinsLabelSet pins the exact derivation — the
// epoch HKDF label pair AND the feed verifier-MAC label pair bound into the
// value — under a fixed key. Any change to any of the four labels (including a
// deliberate feed-MAC version bump, which MUST change the epoch so the sentinel
// disarms legacy bcrypt-only rows) shows up here as an explicit golden-value
// diff instead of sliding through silently.
func TestCalendarFeedKeyEpochGoldenPinsLabelSet(t *testing.T) {
	const goldenKey = "0123456789abcdef0123456789abcdef"
	const goldenEpoch = "d41d3ee5e3b20fde7293658198db88b29e5f00d11b1414c505afee15eb63b7e1"

	epoch, err := CalendarFeedKeyEpoch([]byte(goldenKey))
	if err != nil {
		t.Fatalf("CalendarFeedKeyEpoch: %v", err)
	}
	if epoch != goldenEpoch {
		t.Fatalf("epoch derivation changed for the pinned key.\n got:    %s\n golden: %s\nIf this is a deliberate label change, update the golden AND release-note the consequence: every stored epoch mismatches on next boot, so the sentinel disarms all legacy calendar feeds once.", epoch, goldenEpoch)
	}
}

// TestCalendarFeedKeyEpochRequiresSecretKey pins the hard-failure contract: no
// key can never mean "empty epoch" (an empty stored epoch would compare equal
// to an empty derived one and mask a rotation).
func TestCalendarFeedKeyEpochRequiresSecretKey(t *testing.T) {
	if _, err := CalendarFeedKeyEpoch(nil); !errors.Is(err, ErrCalendarFeedKeyEpochKeyMissing) {
		t.Fatalf("expected ErrCalendarFeedKeyEpochKeyMissing for a nil key, got %v", err)
	}
	if _, err := CalendarFeedKeyEpoch([]byte{}); !errors.Is(err, ErrCalendarFeedKeyEpochKeyMissing) {
		t.Fatalf("expected ErrCalendarFeedKeyEpochKeyMissing for an empty key, got %v", err)
	}
}
