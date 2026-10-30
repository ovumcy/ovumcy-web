package security

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func exchangeWithIDTokenEmailVerified(t *testing.T, emailVerified any, present bool) (OIDCExchangeResult, error) {
	t.Helper()

	mock, caPEM := newMockOIDCProvider(t)
	caFile := writeIssuerCAFile(t, caPEM)
	claims := jwt.MapClaims{
		"iss":   mock.issuer,
		"sub":   "user-123",
		"aud":   "ovumcy",
		"exp":   time.Now().Add(time.Hour).Unix(),
		"iat":   time.Now().Add(-time.Minute).Unix(),
		"nonce": "nonce-abc",
		"email": "owner@example.com",
	}
	if present {
		claims["email_verified"] = emailVerified
	}
	mock.idToken = mustSignMockIDToken(t, mock, claims)

	client := newExchangeTestClient(t, mock, caFile)
	return client.ExchangeCode(context.Background(), "auth-code", "verifier-xyz", "nonce-abc")
}

// TestExchangeCodeReadsIDTokenEmailVerifiedAsBooleanOrExactString: the ID
// token's email_verified is a JSON boolean, or exactly the string "true" or
// "false" that some providers (AWS Cognito) send (WEB-366). An absent claim is
// unverified.
func TestExchangeCodeReadsIDTokenEmailVerifiedAsBooleanOrExactString(t *testing.T) {
	for name, testCase := range map[string]struct {
		value   any
		present bool
		want    bool
	}{
		"boolean true":  {value: true, present: true, want: true},
		"boolean false": {value: false, present: true, want: false},
		"string true":   {value: "true", present: true, want: true},
		"string false":  {value: "false", present: true, want: false},
		"absent":        {present: false, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := exchangeWithIDTokenEmailVerified(t, testCase.value, testCase.present)
			if err != nil {
				t.Fatalf("ExchangeCode: %v", err)
			}
			if result.Claims.EmailVerified != testCase.want {
				t.Fatalf("EmailVerified = %v, want %v", result.Claims.EmailVerified, testCase.want)
			}
		})
	}
}

// TestExchangeCodeRefusesAnyOtherIDTokenEmailVerified: every other shape fails
// the exchange instead of being read as verified or unverified, so the
// email-verification gate stays fail-closed (WEB-366).
func TestExchangeCodeRefusesAnyOtherIDTokenEmailVerified(t *testing.T) {
	for name, value := range map[string]any{
		"upper-case string": "TRUE",
		"string one":        "1",
		"string yes":        "yes",
		"padded string":     " true",
		"empty string":      "",
		"null":              nil,
		"number one":        1,
		"number zero":       0,
		"object":            map[string]any{"value": true},
		"array":             []any{true},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := exchangeWithIDTokenEmailVerified(t, value, true)
			if err == nil {
				t.Fatalf("expected the exchange to be refused, got EmailVerified=%v", result.Claims.EmailVerified)
			}
		})
	}
}
