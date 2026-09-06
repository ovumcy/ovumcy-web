package services

import (
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/crypto/bcrypt"
)

// mustGenerateFeedToken mints a token under the package test key and fails the
// test on error, so each case below reads as the invariant it pins.
func mustGenerateFeedToken(t *testing.T) (string, models.CalendarFeedTokenColumns) {
	t.Helper()
	fullToken, columns, err := GenerateCalendarFeedToken([]byte(calendarFeedTestSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken() unexpected error: %v", err)
	}
	return fullToken, columns
}

// TestGenerateCalendarFeedTokenRoundTrip proves a freshly generated token
// verifies against the storables it returns, and pins the shape of the returned
// values: the full token is selector+verifier at the fixed width, the selector
// is exactly the first calendarFeedSelectorLength characters of the full token,
// and the verifier hash is a real bcrypt hash at passwordHashCost (not the
// verifier plaintext).
func TestGenerateCalendarFeedTokenRoundTrip(t *testing.T) {
	fullToken, columns := mustGenerateFeedToken(t)

	if len(fullToken) != calendarFeedTokenLength {
		t.Fatalf("expected full token length %d, got %d (%q)", calendarFeedTokenLength, len(fullToken), fullToken)
	}
	if len(columns.Selector) != calendarFeedSelectorLength {
		t.Fatalf("expected selector length %d, got %d (%q)", calendarFeedSelectorLength, len(columns.Selector), columns.Selector)
	}
	if !strings.HasPrefix(fullToken, columns.Selector) {
		t.Fatalf("expected full token %q to start with the selector %q", fullToken, columns.Selector)
	}
	// Every character must come from the URL/path-safe token alphabet.
	if strings.Trim(fullToken, calendarFeedAlphabet) != "" {
		t.Fatalf("full token %q contains characters outside the token alphabet", fullToken)
	}
	if got := mustBcryptCost(t, columns.VerifierHash); got != passwordHashCost {
		t.Fatalf("verifier hash cost = %d, want passwordHashCost (%d)", got, passwordHashCost)
	}

	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), fullToken, columns) {
		t.Fatal("expected a freshly generated token to verify against its own storables")
	}
}

// TestGenerateCalendarFeedTokenMintsBothVerifierColumns pins the dual write. The
// MAC is what verification compares; the bcrypt hash is what a binary predating
// migration 032 reads after a rollback. Minting only one of them would either
// pin a fresh subscription to the slow path forever or make a rollback 404 every
// token minted since the upgrade.
func TestGenerateCalendarFeedTokenMintsBothVerifierColumns(t *testing.T) {
	fullToken, columns := mustGenerateFeedToken(t)

	if columns.VerifierMAC == "" {
		t.Fatal("a minted token must carry a keyed verifier MAC — an empty MAC means 'pre-032 row, verify via bcrypt'")
	}
	if _, err := hex.DecodeString(columns.VerifierMAC); err != nil {
		t.Fatalf("verifier MAC %q is not hex: %v", columns.VerifierMAC, err)
	}
	if !strings.HasPrefix(columns.VerifierHash, "$2") {
		t.Fatalf("a minted token must carry a real bcrypt hash for the rollback path, got %q", columns.VerifierHash)
	}

	// The stored MAC is exactly the MAC the verify path recomputes from the
	// presented halves — the two derivations must not drift.
	selector, verifier := mustSplitFeedToken(t, fullToken)
	recomputed, err := security.CalendarFeedVerifierMAC([]byte(calendarFeedTestSecretKey), selector, verifier)
	if err != nil {
		t.Fatalf("CalendarFeedVerifierMAC: %v", err)
	}
	if columns.VerifierMAC != recomputed {
		t.Fatalf("stored MAC %q differs from the MAC recomputed over the presented halves %q", columns.VerifierMAC, recomputed)
	}
}

// TestGenerateCalendarFeedTokenFailsWithoutSecretKey pins the fail-closed rule:
// with no secret key there is no MAC to store, and an empty MAC is not a neutral
// value — it is the marker for "pre-032 row, verify via bcrypt". Minting one
// would silently pin a fresh subscription to the ~265 ms path forever, so
// generation must refuse instead.
func TestGenerateCalendarFeedTokenFailsWithoutSecretKey(t *testing.T) {
	for name, key := range map[string][]byte{"nil": nil, "empty": {}} {
		fullToken, columns, err := GenerateCalendarFeedToken(key)
		if !errors.Is(err, security.ErrCalendarFeedMACKeyMissing) {
			t.Fatalf("%s key: expected ErrCalendarFeedMACKeyMissing, got %v", name, err)
		}
		if fullToken != "" || columns != (models.CalendarFeedTokenColumns{}) {
			t.Fatalf("%s key: expected no token and no storables on failure, got %q / %+v", name, fullToken, columns)
		}
	}
}

// TestVerifyCalendarFeedTokenRejectsWrongVerifier proves that a token carrying
// the correct selector but the wrong verifier half fails verification: the
// verifier is the secret, so a valid selector alone must not authenticate.
func TestVerifyCalendarFeedTokenRejectsWrongVerifier(t *testing.T) {
	_, columns := mustGenerateFeedToken(t)

	// Same selector, a different (wrong) verifier of the correct width.
	wrongVerifier := strings.Repeat("A", calendarFeedVerifierLength)
	tampered := columns.Selector + wrongVerifier

	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), tampered, columns) {
		t.Fatal("expected verification to fail for a token with the wrong verifier half")
	}
}

// TestVerifyCalendarFeedTokenRejectsWrongSelector proves that a token whose
// verifier matches the stored columns but whose selector does not equal the
// stored selector fails verification. The intended call site looks the row up by
// selector first, so in practice a wrong selector resolves no row; this pins the
// service's own selector check as defense in depth.
//
// It holds on both verify paths, and for different reasons: the constant-time
// selector compare fails either way, AND the MAC — which binds the selector into
// its input — cannot match when recomputed over a different selector.
func TestVerifyCalendarFeedTokenRejectsWrongSelector(t *testing.T) {
	fullToken, columns := mustGenerateFeedToken(t)

	_, presentedVerifier := mustSplitFeedToken(t, fullToken)
	// A different selector than the one whose columns we stored, same real verifier.
	otherSelector := strings.Repeat("B", calendarFeedSelectorLength)
	if otherSelector == columns.Selector {
		t.Fatal("test setup: other selector must differ from the real selector")
	}
	tampered := otherSelector + presentedVerifier

	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), tampered, columns) {
		t.Fatal("expected verification to fail when the presented selector does not match the stored selector")
	}
	legacy := columns
	legacy.VerifierMAC = ""
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), tampered, legacy) {
		t.Fatal("expected the same refusal on the pre-032 bcrypt path")
	}
}

// TestVerifyCalendarFeedTokenRejectsMalformedToken proves a token that is not
// exactly the fixed width is rejected outright (no panic, no partial match), so
// a truncated or padded feed URL never reaches a verifier compare.
func TestVerifyCalendarFeedTokenRejectsMalformedToken(t *testing.T) {
	_, columns := mustGenerateFeedToken(t)

	for _, malformed := range []string{
		"",
		"short",
		columns.Selector, // selector only, no verifier
		strings.Repeat("A", calendarFeedTokenLength-1), // one short
		strings.Repeat("A", calendarFeedTokenLength+1), // one long
	} {
		if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), malformed, columns) {
			t.Fatalf("expected malformed token %q (len %d) to fail verification", malformed, len(malformed))
		}
	}
}

// TestVerifyCalendarFeedTokenRejectsEmptyStoredColumns proves that when the
// stored columns are empty (feed off — every column NULL/empty), no presented
// token verifies. This guards the "feed off" state against a token whose halves
// happen to be empty-ish, and pins that an empty MAC alone never means "accept":
// with no bcrypt hash to fall back to, verification refuses.
func TestVerifyCalendarFeedTokenRejectsEmptyStoredColumns(t *testing.T) {
	fullToken, columns := mustGenerateFeedToken(t)

	noSelector := columns
	noSelector.Selector = ""
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), fullToken, noSelector) {
		t.Fatal("expected verification to fail when the stored selector is empty")
	}

	noVerifier := columns
	noVerifier.VerifierHash = ""
	noVerifier.VerifierMAC = ""
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), fullToken, noVerifier) {
		t.Fatal("expected verification to fail when both stored verifier columns are empty")
	}
}

// TestVerifyCalendarFeedTokenPrefersMACOverBcrypt pins the strict precedence at
// the primitive level: with a MAC present, the bcrypt column is not consulted at
// all — neither to accept a token the MAC rejects nor to reject one it accepts.
//
// The first half is the security-relevant one: a stale MAC (the shape a
// SECRET_KEY rotation takes) must be a hard refusal even though the bcrypt hash
// beside it is perfectly valid. Healing from bcrypt there would keep a bearer
// capability alive across a key rotation and put every wrong verifier back on
// the ~265 ms path.
func TestVerifyCalendarFeedTokenPrefersMACOverBcrypt(t *testing.T) {
	fullToken, columns := mustGenerateFeedToken(t)
	selector, verifier := mustSplitFeedToken(t, fullToken)

	// Valid bcrypt hash, MAC minted under a different (rotated) key.
	staleMAC, err := security.CalendarFeedVerifierMAC([]byte(calendarFeedRotatedTestKey), selector, verifier)
	if err != nil {
		t.Fatalf("CalendarFeedVerifierMAC: %v", err)
	}
	stale := columns
	stale.VerifierMAC = staleMAC
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), fullToken, stale) {
		t.Fatal("a MAC minted under a rotated key must be refused, never healed from the bcrypt hash beside it")
	}

	// Valid MAC, unusable bcrypt hash: the MAC decides, so this verifies.
	macOnly := columns
	macOnly.VerifierHash = "$2a$12$not.a.hash.that.could.ever.match.this.verifier.value.xxxxx"
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), fullToken, macOnly) {
		t.Fatal("a valid MAC must verify without consulting the bcrypt column")
	}
}

// TestVerifyCalendarFeedTokenFallsBackToBcryptOnlyForPre032Rows proves the
// bcrypt path is reachable exactly when the MAC column is empty — the state of
// every row minted before migration 032, whose MAC cannot be derived from
// storage.
func TestVerifyCalendarFeedTokenFallsBackToBcryptOnlyForPre032Rows(t *testing.T) {
	fullToken, columns := mustGenerateFeedToken(t)

	legacy := models.CalendarFeedTokenColumns{Selector: columns.Selector, VerifierHash: columns.VerifierHash}
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), fullToken, legacy) {
		t.Fatal("a pre-032 row (hash, no MAC) must still verify through bcrypt")
	}
	// A pre-032 row verifies with no secret key at all: bcrypt needs none, which is
	// why a rotation cannot break a row that has not migrated yet.
	if !VerifyCalendarFeedToken(nil, fullToken, legacy) {
		t.Fatal("the pre-032 bcrypt path must not depend on the secret key")
	}
}

// TestGenerateCalendarFeedTokenHashAtRest proves neither stored verifier column
// is the verifier plaintext: the stored values neither equal nor contain the
// secret verifier substring, the bcrypt hash opens only with the verifier, and
// the MAC is key-dependent (so a DB leak alone does not let an attacker verify
// guessed verifiers offline). This is the hash-at-rest invariant — the secret
// half never lands in a storable field in the clear.
func TestGenerateCalendarFeedTokenHashAtRest(t *testing.T) {
	fullToken, columns := mustGenerateFeedToken(t)

	selector, verifier := mustSplitFeedToken(t, fullToken)

	for name, stored := range map[string]string{"hash": columns.VerifierHash, "mac": columns.VerifierMAC} {
		if stored == verifier {
			t.Fatalf("stored %s must not equal the verifier plaintext", name)
		}
		if strings.Contains(stored, verifier) {
			t.Fatalf("stored %s %q must not contain the verifier plaintext", name, stored)
		}
		if strings.Contains(stored, fullToken) {
			t.Fatalf("stored %s must not contain the full token", name)
		}
	}

	// The stored hash is a valid bcrypt hash that opens only with the verifier,
	// never with the selector.
	if bcrypt.CompareHashAndPassword([]byte(columns.VerifierHash), []byte(verifier)) != nil {
		t.Fatal("expected the stored hash to verify against the real verifier")
	}
	if bcrypt.CompareHashAndPassword([]byte(columns.VerifierHash), []byte(selector)) == nil {
		t.Fatal("stored hash must not verify against the selector")
	}

	// The MAC is keyed: the same (selector, verifier) under a different key yields
	// a different value, so the stored MAC is useless without SECRET_KEY.
	underOtherKey, err := security.CalendarFeedVerifierMAC([]byte(calendarFeedRotatedTestKey), selector, verifier)
	if err != nil {
		t.Fatalf("CalendarFeedVerifierMAC: %v", err)
	}
	if underOtherKey == columns.VerifierMAC {
		t.Fatal("the verifier MAC must depend on the secret key")
	}
}

// TestGenerateCalendarFeedTokenUniquePerCall proves two generations produce
// distinct selectors and distinct full tokens, so rotating always changes the
// lookup id (no collision, no reuse of the previous URL).
func TestGenerateCalendarFeedTokenUniquePerCall(t *testing.T) {
	firstToken, first := mustGenerateFeedToken(t)
	secondToken, second := mustGenerateFeedToken(t)

	if first.Selector == second.Selector {
		t.Fatal("expected two generations to yield distinct selectors")
	}
	if firstToken == secondToken {
		t.Fatal("expected two generations to yield distinct full tokens")
	}
	if first.VerifierMAC == second.VerifierMAC {
		t.Fatal("expected two generations to yield distinct verifier MACs")
	}
}

// TestRotateCalendarFeedTokenInvalidatesOldToken proves that after a rotation
// (a fresh generate whose storables replace the previous triple) the OLD full
// token no longer verifies against the NEW storables — the old verifier matches
// neither the new MAC nor the new hash, and the old selector does not match the
// new selector. This is the security core of "rotate revokes the previous URL".
func TestRotateCalendarFeedTokenInvalidatesOldToken(t *testing.T) {
	oldToken, oldColumns := mustGenerateFeedToken(t)
	// Sanity: the old token verifies against the old storables before rotation.
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), oldToken, oldColumns) {
		t.Fatal("old token should verify against the old storables before rotation")
	}

	newToken, newColumns := mustGenerateFeedToken(t)

	// After rotation, only the new token verifies against the new storables.
	if VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), oldToken, newColumns) {
		t.Fatal("old token must NOT verify against the rotated (new) storables")
	}
	if !VerifyCalendarFeedToken([]byte(calendarFeedTestSecretKey), newToken, newColumns) {
		t.Fatal("new token must verify against the rotated (new) storables")
	}
}
