package services

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestBuildAndParseAuthSessionToken(t *testing.T) {
	secret := []byte("test-auth-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	token, err := BuildAuthSessionToken(secret, 42, "owner", 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionToken() unexpected error: %v", err)
	}

	claims, err := ParseAuthSessionToken(secret, token, now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("ParseAuthSessionToken() unexpected error: %v", err)
	}
	if claims.UserID != 42 {
		t.Fatalf("expected user id 42, got %d", claims.UserID)
	}
	if claims.Role != "owner" {
		t.Fatalf("expected role owner, got %q", claims.Role)
	}
}

func TestParseAuthSessionTokenRejectsExpired(t *testing.T) {
	secret := []byte("test-auth-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	token, err := BuildAuthSessionToken(secret, 42, "owner", 1*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionToken() unexpected error: %v", err)
	}

	_, err = ParseAuthSessionToken(secret, token, now.Add(2*time.Minute))
	if !errors.Is(err, ErrAuthSessionTokenExpired) {
		t.Fatalf("expected ErrAuthSessionTokenExpired, got %v", err)
	}
}

func TestParseAuthSessionTokenRejectsInvalidSignature(t *testing.T) {
	secret := []byte("test-auth-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	token, err := BuildAuthSessionToken(secret, 42, "owner", 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildAuthSessionToken() unexpected error: %v", err)
	}

	_, err = ParseAuthSessionToken([]byte("other-secret"), token, now.Add(1*time.Minute))
	if !errors.Is(err, ErrAuthSessionTokenInvalid) {
		t.Fatalf("expected ErrAuthSessionTokenInvalid, got %v", err)
	}
}

func TestParseAuthSessionTokenRejectsWrongAlgorithm(t *testing.T) {
	secret := []byte("test-auth-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)

	claims := AuthSessionClaims{
		UserID: 42,
		Role:   "owner",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(10 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	unsigned := jwt.NewWithClaims(jwt.SigningMethodNone, claims)
	rawToken, err := unsigned.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("sign none token: %v", err)
	}

	_, err = ParseAuthSessionToken(secret, rawToken, now.Add(1*time.Minute))
	if !errors.Is(err, ErrAuthSessionTokenInvalid) {
		t.Fatalf("expected ErrAuthSessionTokenInvalid, got %v", err)
	}
}

