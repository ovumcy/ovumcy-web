package security

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestExchangeCodeRefusesATokenWithoutASubject pins the claims-parse half of
// the empty-subject rule: the (issuer, subject) pair is the identity every
// lookup and link keys on, so a signed, nonce-matching token whose sub is
// absent or blank must be refused before any caller sees its claims. The
// positive anchor is TestExchangeCodeHappyPath, which carries a real sub
// through the same client.
func TestExchangeCodeRefusesATokenWithoutASubject(t *testing.T) {
	for name, subject := range map[string]any{
		"absent": nil,
		"empty":  "",
		"blank":  "   ",
	} {
		t.Run(name, func(t *testing.T) {
			mock, caPEM := newMockOIDCProvider(t)
			caFile := writeIssuerCAFile(t, caPEM)

			claims := jwt.MapClaims{
				"iss":   mock.issuer,
				"aud":   "ovumcy",
				"exp":   time.Now().Add(time.Hour).Unix(),
				"iat":   time.Now().Unix(),
				"nonce": "nonce-sub",
				"email": "owner@example.com",
			}
			if subject != nil {
				claims["sub"] = subject
			}
			mock.idToken = mustSignMockIDToken(t, mock, claims)

			client := newExchangeTestClient(t, mock, caFile)
			result, err := client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-sub")
			if err == nil {
				t.Fatalf("a token with no usable subject must be refused, got claims %+v", result.Claims)
			}
			if !strings.Contains(err.Error(), "sub") {
				t.Fatalf("expected the missing-subject refusal, got %v", err)
			}
		})
	}
}
