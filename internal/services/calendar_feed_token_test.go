package services

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// TestGenerateCalendarFeedTokenRoundTrip proves a freshly generated token
// verifies against the storables it returns, and pins the shape of the returned
// values: the full token is selector+verifier at the fixed width, the selector
// is exactly the first calendarFeedSelectorLength characters of the full token,
// and the verifier hash is a real bcrypt hash at passwordHashCost (not the
// verifier plaintext).
func TestGenerateCalendarFeedTokenRoundTrip(t *testing.T) {
	fullToken, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken() unexpected error: %v", err)
	}

	if len(fullToken) != calendarFeedTokenLength {
		t.Fatalf("expected full token length %d, got %d (%q)", calendarFeedTokenLength, len(fullToken), fullToken)
	}
	if len(selector) != calendarFeedSelectorLength {
		t.Fatalf("expected selector length %d, got %d (%q)", calendarFeedSelectorLength, len(selector), selector)
	}
	if !strings.HasPrefix(fullToken, selector) {
		t.Fatalf("expected full token %q to start with the selector %q", fullToken, selector)
	}
	// Every character must come from the URL/path-safe token alphabet.
	if strings.Trim(fullToken, calendarFeedAlphabet) != "" {
		t.Fatalf("full token %q contains characters outside the token alphabet", fullToken)
	}
	if got := mustBcryptCost(t, verifierHash); got != passwordHashCost {
		t.Fatalf("verifier hash cost = %d, want passwordHashCost (%d)", got, passwordHashCost)
	}

	if !VerifyCalendarFeedToken(fullToken, selector, verifierHash) {
		t.Fatal("expected a freshly generated token to verify against its own storables")
	}
}

// TestVerifyCalendarFeedTokenRejectsWrongVerifier proves that a token carrying
// the correct selector but the wrong verifier half fails verification: the
// verifier is the secret, so a valid selector alone must not authenticate.
func TestVerifyCalendarFeedTokenRejectsWrongVerifier(t *testing.T) {
	_, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken() unexpected error: %v", err)
	}

	// Same selector, a different (wrong) verifier of the correct width.
	wrongVerifier := strings.Repeat("A", calendarFeedVerifierLength)
	tampered := selector + wrongVerifier

	if VerifyCalendarFeedToken(tampered, selector, verifierHash) {
		t.Fatal("expected verification to fail for a token with the wrong verifier half")
	}
}

// TestVerifyCalendarFeedTokenRejectsWrongSelector proves that a token whose
// verifier matches the stored hash but whose selector does not equal the stored
// selector fails verification. The intended call site looks the row up by
// selector first, so in practice a wrong selector resolves no row; this pins the
// service's own selector check as defense in depth.
func TestVerifyCalendarFeedTokenRejectsWrongSelector(t *testing.T) {
	fullToken, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken() unexpected error: %v", err)
	}

	_, presentedVerifier, ok := SplitCalendarFeedToken(fullToken)
	if !ok {
		t.Fatal("expected the generated token to split")
	}
	// A different selector than the one whose hash we stored, same real verifier.
	otherSelector := strings.Repeat("B", calendarFeedSelectorLength)
	if otherSelector == selector {
		t.Fatal("test setup: other selector must differ from the real selector")
	}
	tampered := otherSelector + presentedVerifier

	if VerifyCalendarFeedToken(tampered, selector, verifierHash) {
		t.Fatal("expected verification to fail when the presented selector does not match the stored selector")
	}
}

// TestVerifyCalendarFeedTokenRejectsMalformedToken proves a token that is not
// exactly the fixed width is rejected outright (no panic, no partial match), so
// a truncated or padded feed URL never reaches a verifier compare.
func TestVerifyCalendarFeedTokenRejectsMalformedToken(t *testing.T) {
	_, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken() unexpected error: %v", err)
	}

	for _, malformed := range []string{
		"",
		"short",
		selector, // selector only, no verifier
		strings.Repeat("A", calendarFeedTokenLength-1), // one short
		strings.Repeat("A", calendarFeedTokenLength+1), // one long
	} {
		if VerifyCalendarFeedToken(malformed, selector, verifierHash) {
			t.Fatalf("expected malformed token %q (len %d) to fail verification", malformed, len(malformed))
		}
	}
}

// TestVerifyCalendarFeedTokenRejectsEmptyStoredColumns proves that when the
// stored columns are empty (feed off — both columns NULL/empty), no presented
// token verifies. This guards the "feed off" state against a token whose halves
// happen to be empty-ish.
func TestVerifyCalendarFeedTokenRejectsEmptyStoredColumns(t *testing.T) {
	fullToken, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken() unexpected error: %v", err)
	}

	if VerifyCalendarFeedToken(fullToken, "", verifierHash) {
		t.Fatal("expected verification to fail when the stored selector is empty")
	}
	if VerifyCalendarFeedToken(fullToken, selector, "") {
		t.Fatal("expected verification to fail when the stored verifier hash is empty")
	}
}

// TestGenerateCalendarFeedTokenHashAtRest proves the returned verifier hash is a
// bcrypt hash of the verifier and NOT the verifier plaintext: the stored value
// neither equals nor contains the secret verifier substring, and the plaintext
// verifier does not verify when treated as if it were the stored hash. This is
// the hash-at-rest invariant — the secret half never lands in a storable field
// in the clear.
func TestGenerateCalendarFeedTokenHashAtRest(t *testing.T) {
	fullToken, selector, verifierHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken() unexpected error: %v", err)
	}

	_, verifier, ok := SplitCalendarFeedToken(fullToken)
	if !ok {
		t.Fatal("expected the generated token to split")
	}

	if verifierHash == verifier {
		t.Fatal("verifier hash must not equal the verifier plaintext")
	}
	if strings.Contains(verifierHash, verifier) {
		t.Fatalf("verifier hash %q must not contain the verifier plaintext", verifierHash)
	}
	// The stored hash must not contain the full token or the shown-once verifier.
	if strings.Contains(verifierHash, fullToken) {
		t.Fatal("verifier hash must not contain the full token")
	}
	// The stored value is a valid bcrypt hash that opens only with the verifier,
	// never with the selector.
	if bcrypt.CompareHashAndPassword([]byte(verifierHash), []byte(verifier)) != nil {
		t.Fatal("expected the stored hash to verify against the real verifier")
	}
	if bcrypt.CompareHashAndPassword([]byte(verifierHash), []byte(selector)) == nil {
		t.Fatal("stored hash must not verify against the selector")
	}
}

// TestGenerateCalendarFeedTokenUniquePerCall proves two generations produce
// distinct selectors and distinct full tokens, so rotating always changes the
// lookup id (no collision, no reuse of the previous URL).
func TestGenerateCalendarFeedTokenUniquePerCall(t *testing.T) {
	firstToken, firstSelector, _, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("first GenerateCalendarFeedToken(): %v", err)
	}
	secondToken, secondSelector, _, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("second GenerateCalendarFeedToken(): %v", err)
	}

	if firstSelector == secondSelector {
		t.Fatal("expected two generations to yield distinct selectors")
	}
	if firstToken == secondToken {
		t.Fatal("expected two generations to yield distinct full tokens")
	}
}

// TestRotateCalendarFeedTokenInvalidatesOldToken proves that after a rotation
// (a fresh generate whose storables replace the previous pair) the OLD full
// token no longer verifies against the NEW storables — the old verifier does not
// match the new hash and the old selector does not match the new selector. This
// is the security core of "rotate revokes the previous URL".
func TestRotateCalendarFeedTokenInvalidatesOldToken(t *testing.T) {
	oldToken, oldSelector, oldHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("generate old token: %v", err)
	}
	// Sanity: the old token verifies against the old storables before rotation.
	if !VerifyCalendarFeedToken(oldToken, oldSelector, oldHash) {
		t.Fatal("old token should verify against the old storables before rotation")
	}

	newToken, newSelector, newHash, err := GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("generate new token (rotation): %v", err)
	}

	// After rotation, only the new token verifies against the new storables.
	if VerifyCalendarFeedToken(oldToken, newSelector, newHash) {
		t.Fatal("old token must NOT verify against the rotated (new) storables")
	}
	if !VerifyCalendarFeedToken(newToken, newSelector, newHash) {
		t.Fatal("new token must verify against the rotated (new) storables")
	}
}
