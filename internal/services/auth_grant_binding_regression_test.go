package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"golang.org/x/crypto/bcrypt"
)

// TestSessionAndResetTokensDoNotCrossDomains pins the key split: a session
// token and a reset token minted under the same SECRET_KEY are each refused by
// the other's parser, and a token signed the pre-split way — HS256 directly
// under SECRET_KEY, with the right claims — is refused by both. Each parser
// first accepts its own token, so the refusals are not a parser that refuses
// everything.
func TestSessionAndResetTokensDoNotCrossDomains(t *testing.T) {
	secret := []byte("test-secret-token-domains")
	now := time.Date(2026, time.September, 21, 10, 0, 0, 0, time.UTC)

	sessionToken, _, err := BuildAuthSessionTokenWithVersionAndSessionID(secret, 7, models.RoleOwner, 1, time.Hour, now)
	if err != nil {
		t.Fatalf("build session token: %v", err)
	}
	resetToken, err := BuildPasswordResetToken(secret, 7, "$2a$10$storedhashstoredhashstoredhashstoredhashstoredhash", 1, PasswordResetTokenPurposeRecovery, time.Hour, now)
	if err != nil {
		t.Fatalf("build reset token: %v", err)
	}

	if _, err := ParseAuthSessionToken(secret, sessionToken, now); err != nil {
		t.Fatalf("anchor: the session parser refused its own token: %v", err)
	}
	if _, err := ParsePasswordResetToken(secret, resetToken, now); err != nil {
		t.Fatalf("anchor: the reset parser refused its own token: %v", err)
	}
	if _, err := ParsePasswordResetToken(secret, sessionToken, now); err == nil {
		t.Fatal("the reset parser accepted a session token")
	}
	if _, err := ParseAuthSessionToken(secret, resetToken, now); err == nil {
		t.Fatal("the session parser accepted a reset token")
	}

	// Pre-split tokens: right claims, signed directly under SECRET_KEY. Every
	// claim the parser requires is present (a session id included), so the
	// signing key is the only thing that can refuse it.
	legacySession := jwt.NewWithClaims(jwt.SigningMethodHS256, AuthSessionClaims{
		UserID:         7,
		Role:           models.RoleOwner,
		SessionVersion: 1,
		SessionID:      "legacy-session-id",
		RegisteredClaims: jwt.RegisteredClaims{
			Audience:  jwt.ClaimStrings{authSessionTokenDomain.audience},
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	})
	legacySession.Header["typ"] = authSessionTokenDomain.typ
	rawLegacySession, err := legacySession.SignedString(secret)
	if err != nil {
		t.Fatalf("sign legacy session token: %v", err)
	}
	if _, err := ParseAuthSessionToken(secret, rawLegacySession, now); err == nil {
		t.Fatal("the session parser accepted a token signed directly under SECRET_KEY")
	}

	sessionKey, err := security.DeriveTokenSigningKey(secret, security.AuthSessionTokenKeyLabel)
	if err != nil {
		t.Fatalf("derive session key: %v", err)
	}
	resetKey, err := security.DeriveTokenSigningKey(secret, security.PasswordResetTokenKeyLabel)
	if err != nil {
		t.Fatalf("derive reset key: %v", err)
	}
	if string(sessionKey) == string(resetKey) || string(sessionKey) == string(secret) {
		t.Fatal("the session and reset keys must be distinct from each other and from SECRET_KEY")
	}
	if _, err := security.DeriveTokenSigningKey(secret, " "); err == nil {
		t.Fatal("an empty purpose label must be refused, not derived")
	}
}

// TestResetGrantDiesWithTheSessionVersionItWasMintedAt pins the reset grant's
// binding to auth_session_version: a grant minted before a recovery-code
// rotation is refused, though the password hash its fingerprint covers never
// changed, and a grant minted after the rotation resolves.
func TestResetGrantDiesWithTheSessionVersionItWasMintedAt(t *testing.T) {
	secret := []byte("test-secret-reset-grant-version")
	now := time.Now()

	hash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	repo := &stubAuthUserRepo{}
	repo.user = models.User{
		ID:                 9,
		PasswordHash:       string(hash),
		RecoveryCodeHash:   "old-recovery",
		LocalAuthEnabled:   true,
		AuthSessionVersion: 1,
		Role:               models.RoleOwner,
	}
	service := NewAuthService(repo)

	staleGrant, err := BuildPasswordResetToken(secret, 9, string(hash), 1, PasswordResetTokenPurposeRecovery, time.Hour, now)
	if err != nil {
		t.Fatalf("build stale grant: %v", err)
	}
	if _, err := service.ResolveUserByResetToken(context.Background(), secret, staleGrant, now); err != nil {
		t.Fatalf("anchor: a grant at the current version must resolve, got %v", err)
	}

	if _, err := service.RegenerateRecoveryCode(context.Background(), 9); err != nil {
		t.Fatalf("regenerate recovery code: %v", err)
	}
	if repo.user.AuthSessionVersion != 2 || repo.user.PasswordHash != string(hash) {
		t.Fatalf("setup: expected the rotation to bump the version to 2 and leave the hash, got version %d", repo.user.AuthSessionVersion)
	}

	if _, err := service.ResolveUserByResetToken(context.Background(), secret, staleGrant, now); !errors.Is(err, ErrInvalidResetToken) {
		t.Fatalf("a grant minted before the recovery-code rotation must be refused, got %v", err)
	}
	freshGrant, err := BuildPasswordResetToken(secret, 9, string(hash), repo.user.AuthSessionVersion, PasswordResetTokenPurposeRecovery, time.Hour, now)
	if err != nil {
		t.Fatalf("build fresh grant: %v", err)
	}
	if _, err := service.ResolveUserByResetToken(context.Background(), secret, freshGrant, now); err != nil {
		t.Fatalf("a grant minted after the rotation must resolve, got %v", err)
	}
}

// TestSecondFactorGrantRefusedAfterAVersionBumpOrForcedReset pins the pending
// TOTP grant's gate: current version passes, a bumped row, a missing version
// and an operator-forced reset each refuse.
func TestSecondFactorGrantRefusedAfterAVersionBumpOrForcedReset(t *testing.T) {
	user := &models.User{ID: 3, AuthSessionVersion: 4}
	if !SecondFactorGrantCurrent(4, user) {
		t.Fatal("anchor: a grant at the row's version must pass")
	}
	if SecondFactorGrantCurrent(3, user) {
		t.Fatal("a grant minted before a version bump must be refused")
	}
	if SecondFactorGrantCurrent(0, &models.User{ID: 3, AuthSessionVersion: 0}) {
		t.Fatal("a grant carrying no version must be refused, not read as version 1")
	}
	if SecondFactorGrantCurrent(4, &models.User{ID: 3, AuthSessionVersion: 4, MustChangePassword: true}) {
		t.Fatal("a grant for an account under a forced reset must be refused")
	}
}

// TestAuthAttemptResetClientForgivesOnlyTheSucceedingClient pins ResetClient,
// the reset of the unauthenticated flows: an owner's success from one client
// clears that client's bucket and nothing else — neither another client's
// failures nor the identity bucket they pooled against the same account.
func TestAuthAttemptResetClientForgivesOnlyTheSucceedingClient(t *testing.T) {
	secret := []byte("test-secret-attempt-reset")
	now := time.Now()
	policy := NewAuthAttemptPolicy("login", nil, 3, time.Minute)

	for range 3 {
		policy.AddFailure(secret, "attacker-ip", "owner@example.test", now)
	}
	policy.AddFailure(secret, "owner-ip", "", now)
	if !policy.TooManyRecent(secret, "attacker-ip", "owner@example.test", now) {
		t.Fatal("anchor: the attacker's client must be over budget")
	}

	policy.ResetClient("owner-ip")

	if !policy.TooManyRecent(secret, "attacker-ip", "", now) {
		t.Fatal("the owner's success reset the attacker's client bucket")
	}
	if !policy.TooManyRecent(secret, "fresh-ip", "owner@example.test", now) {
		t.Fatal("the owner's success reset the identity bucket other clients pooled")
	}
	policy.AddFailure(secret, "owner-ip", "", now)
	policy.AddFailure(secret, "owner-ip", "", now)
	if policy.TooManyRecent(secret, "owner-ip", "", now) {
		t.Fatal("the succeeding client's own bucket must have been cleared")
	}
}

// TestAuthSessionTokenRefusesCorrectKeyWrongDomainClaim pins that the
// derived key alone does not admit a session token: a forgery signed under
// the CORRECT auth-session HKDF key (security.AuthSessionTokenKeyLabel) is
// still refused when the `aud` claim or the `typ` header does not also match
// the domain, per authTokenDomain.parse's WithAudience option and its typ
// check (auth_token_domain.go). Every other claim is built the same way the
// real builder would, so the only defect in each case is the one named.
func TestAuthSessionTokenRefusesCorrectKeyWrongDomainClaim(t *testing.T) {
	secret := []byte("test-secret-session-domain-claim")
	now := time.Date(2026, time.September, 22, 9, 0, 0, 0, time.UTC)

	sessionKey, err := security.DeriveTokenSigningKey(secret, security.AuthSessionTokenKeyLabel)
	if err != nil {
		t.Fatalf("derive session key: %v", err)
	}

	build := func(audience jwt.ClaimStrings, typ string) string {
		claims := AuthSessionClaims{
			UserID:         7,
			Role:           models.RoleOwner,
			SessionVersion: 1,
			SessionID:      "regression-session-id",
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "7",
				Audience:  audience,
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(now),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		token.Header["typ"] = typ
		raw, signErr := token.SignedString(sessionKey)
		if signErr != nil {
			t.Fatalf("sign forged session token: %v", signErr)
		}
		return raw
	}

	// Anchor: the same builder, stamped with the domain's own aud and typ,
	// resolves — so a case below failing is the broken claim, not some other
	// defect the hand-built claims happen to share.
	anchor := build(jwt.ClaimStrings{authSessionTokenDomain.audience}, authSessionTokenDomain.typ)
	if _, err := ParseAuthSessionToken(secret, anchor, now); err != nil {
		t.Fatalf("anchor: a correctly-shaped hand-built session token was refused: %v", err)
	}

	cases := []struct {
		name     string
		audience jwt.ClaimStrings
		typ      string
	}{
		{"foreign audience, correct typ", jwt.ClaimStrings{passwordResetTokenDomain.audience}, authSessionTokenDomain.typ},
		{"missing audience, correct typ", nil, authSessionTokenDomain.typ},
		{"correct audience, foreign typ", jwt.ClaimStrings{authSessionTokenDomain.audience}, passwordResetTokenDomain.typ},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := build(tc.audience, tc.typ)
			if _, err := ParseAuthSessionToken(secret, raw, now); err == nil {
				t.Fatalf("the session parser accepted a token with %s, signed under the correct derived key", tc.name)
			}
		})
	}
}

// TestPasswordResetTokenRefusesCorrectKeyWrongDomainClaim is
// TestAuthSessionTokenRefusesCorrectKeyWrongDomainClaim's mirror for the
// reset-grant domain (security.PasswordResetTokenKeyLabel): a forgery signed
// under the CORRECT derived reset key is still refused on a foreign/missing
// `aud` or a foreign `typ` header.
func TestPasswordResetTokenRefusesCorrectKeyWrongDomainClaim(t *testing.T) {
	secret := []byte("test-secret-reset-domain-claim")
	now := time.Date(2026, time.September, 22, 9, 0, 0, 0, time.UTC)
	storedHash := "$2a$10$storedhashstoredhashstoredhashstoredhashstoredhash"

	resetKey, err := security.DeriveTokenSigningKey(secret, security.PasswordResetTokenKeyLabel)
	if err != nil {
		t.Fatalf("derive reset key: %v", err)
	}

	build := func(audience jwt.ClaimStrings, typ string) string {
		claims := PasswordResetClaims{
			UserID:         7,
			Purpose:        PasswordResetTokenPurposeRecovery,
			PasswordState:  PasswordStateFingerprint(storedHash),
			SessionVersion: 1,
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "7",
				Audience:  audience,
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
				IssuedAt:  jwt.NewNumericDate(now),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		token.Header["typ"] = typ
		raw, signErr := token.SignedString(resetKey)
		if signErr != nil {
			t.Fatalf("sign forged reset token: %v", signErr)
		}
		return raw
	}

	anchor := build(jwt.ClaimStrings{passwordResetTokenDomain.audience}, passwordResetTokenDomain.typ)
	if _, err := ParsePasswordResetToken(secret, anchor, now); err != nil {
		t.Fatalf("anchor: a correctly-shaped hand-built reset token was refused: %v", err)
	}

	cases := []struct {
		name     string
		audience jwt.ClaimStrings
		typ      string
	}{
		{"foreign audience, correct typ", jwt.ClaimStrings{authSessionTokenDomain.audience}, passwordResetTokenDomain.typ},
		{"missing audience, correct typ", nil, passwordResetTokenDomain.typ},
		{"correct audience, foreign typ", jwt.ClaimStrings{passwordResetTokenDomain.audience}, authSessionTokenDomain.typ},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := build(tc.audience, tc.typ)
			if _, err := ParsePasswordResetToken(secret, raw, now); err == nil {
				t.Fatalf("the reset parser accepted a token with %s, signed under the correct derived key", tc.name)
			}
		})
	}
}
