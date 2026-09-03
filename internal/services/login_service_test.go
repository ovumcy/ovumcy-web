package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubLoginAuthService struct {
	user  models.User
	err   error
	calls int
}

func (stub *stubLoginAuthService) AuthenticateCredentials(context.Context, string, string) (models.User, error) {
	stub.calls++
	if stub.err != nil {
		return models.User{}, stub.err
	}
	return stub.user, nil
}

type stubLoginResetTokenIssuer struct {
	token       string
	err         error
	called      bool
	lastUserID  uint
	lastTTL     time.Duration
	lastPurpose string
}

func (stub *stubLoginResetTokenIssuer) IssueResetTokenForUser(_ []byte, user *models.User, purpose string, ttl time.Duration, _ time.Time) (string, error) {
	stub.called = true
	if user != nil {
		stub.lastUserID = user.ID
	}
	stub.lastPurpose = purpose
	stub.lastTTL = ttl
	if stub.err != nil {
		return "", stub.err
	}
	return stub.token, nil
}

func TestLoginServiceAuthenticateWithoutForcedReset(t *testing.T) {
	service, reset := newLoginServiceForTest(models.User{ID: 7, MustChangePassword: false}, nil, "token")

	result, err := service.Authenticate(context.Background(), []byte("secret"), "127.0.0.1", "user@example.com", "StrongPass1", loginServiceTestTTL, loginServiceTestNow)
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if result.User.ID != 7 {
		t.Fatalf("expected user id 7, got %d", result.User.ID)
	}
	if result.RequiresPasswordReset {
		t.Fatalf("did not expect forced reset")
	}
	if result.ResetToken != "" {
		t.Fatalf("did not expect reset token for non-forced reset")
	}
	if reset.called {
		t.Fatalf("did not expect reset token issuance")
	}
}

func TestLoginServiceAuthenticateForcedResetIssuesToken(t *testing.T) {
	service, reset := newLoginServiceForTest(models.User{ID: 9, MustChangePassword: true}, nil, "issued-reset-token")

	result, err := service.Authenticate(context.Background(), []byte("secret"), "127.0.0.1", "user@example.com", "StrongPass1", loginServiceTestTTL, loginServiceTestNow)
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if !result.RequiresPasswordReset {
		t.Fatalf("expected forced reset")
	}
	if result.ResetToken != "issued-reset-token" {
		t.Fatalf("expected issued reset token, got %q", result.ResetToken)
	}
	if !reset.called || reset.lastUserID != 9 {
		t.Fatalf("expected reset token issuance for user 9")
	}
	if reset.lastTTL != loginServiceTestTTL {
		t.Fatalf("expected reset ttl %s, got %s", loginServiceTestTTL, reset.lastTTL)
	}
	// Authenticate only reaches the mint branch after AuthenticateCredentials
	// verifies a LOCAL password (the stub above stands in for it) — the same
	// call OIDC link-confirm's password challenge makes (A1). It must always
	// mint PasswordResetTokenPurposeForcedLocal, never forced-from-OIDC:
	// mislabelling it would let the token bypass the instance-wide
	// local-sign-in gate on exactly the path that just proved a local
	// password (PRIV-4).
	if reset.lastPurpose != PasswordResetTokenPurposeForcedLocal {
		t.Fatalf("expected forced-from-local purpose, got %q", reset.lastPurpose)
	}
}

// TestLoginServiceForcedResetOutranksTOTPForAnAccountWithBothFlags pins a
// DECISION, not merely the behaviour that happens to exist today: for an
// account carrying BOTH MustChangePassword and TOTPEnabled, Authenticate
// routes to the forced password reset and raises NO TOTP challenge.
//
// The ordering is deliberate, and it is the intentional recovery path for an
// owner whose second factor is unusable. TOTP secrets are encrypted under a key
// derived from the application secret, so after a SECRET_KEY rotation the
// stored ciphertext no longer opens and the 2FA challenge can never be answered
// by any code the authenticator produces; the operator-forced reset
// (`ovumcy reset-password <email>`) is the way back in that
// docs/security/cryptography.md and docs/self-hosted.md instruct an operator to
// use. Making TOTP win would withdraw that escape hatch from precisely the
// accounts whose second factor is already broken, leaving permanent owner
// lockout with no in-product remedy.
//
// It is not a downgrade of session security: the reset still bumps
// AuthSessionVersion in the same atomic update that writes the new password
// hash (internal/db/user_repository.go), so every session predating it dies.
//
// Both halves of the assertion matter — RequiresPasswordReset alone would still
// pass if the service started returning both flags at once, which the callers
// resolve inconsistently. No other test in this package pins the ordering, so
// deleting this case makes reversing the decision free and silent: read a
// failure here as "the decision was reversed", never as a stale assertion. The
// public declaration of this accepted risk is recorded separately from this
// test.
func TestLoginServiceForcedResetOutranksTOTPForAnAccountWithBothFlags(t *testing.T) {
	service, reset := newLoginServiceForTest(
		models.User{ID: 21, MustChangePassword: true, TOTPEnabled: true},
		nil,
		"issued-reset-token",
	)

	result, err := service.Authenticate(context.Background(), []byte("secret"), "127.0.0.1", "user@example.com", "StrongPass1", loginServiceTestTTL, loginServiceTestNow)
	if err != nil {
		t.Fatalf("Authenticate() unexpected error: %v", err)
	}
	if !result.RequiresPasswordReset {
		t.Fatalf("expected forced reset to outrank the TOTP challenge for an account with both flags")
	}
	if result.RequiresTOTP {
		t.Fatalf("expected no TOTP challenge on the forced-reset recovery path")
	}
	if result.ResetToken != "issued-reset-token" {
		t.Fatalf("expected issued reset token, got %q", result.ResetToken)
	}
	if !reset.called || reset.lastUserID != 21 {
		t.Fatalf("expected reset token issuance for user 21")
	}
}

func TestLoginServiceAuthenticatePropagatesInvalidCredentials(t *testing.T) {
	authErr := ErrAuthInvalidCreds
	service, reset := newLoginServiceForTest(models.User{}, authErr, "unused")

	if _, err := service.Authenticate(context.Background(), []byte("secret"), "127.0.0.1", "user@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow); !errors.Is(err, authErr) {
		t.Fatalf("expected auth error %v, got %v", authErr, err)
	}
	if reset.called {
		t.Fatalf("did not expect reset token issuance on auth error")
	}
}

func TestLoginServiceAuthenticateRateLimitsByIdentityAcrossIPs(t *testing.T) {
	auth := &stubLoginAuthService{err: ErrAuthInvalidCreds}
	reset := &stubLoginResetTokenIssuer{}
	service := NewLoginService(auth, reset, NewAttemptLimiter())
	service.ConfigureAttemptLimits(2, time.Hour)

	if _, err := service.Authenticate(context.Background(), []byte("secret"), "10.0.0.1", "owner@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected invalid credentials on first attempt, got %v", err)
	}
	if _, err := service.Authenticate(context.Background(), []byte("secret"), "10.0.0.2", "owner@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow.Add(time.Minute)); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected invalid credentials on second attempt, got %v", err)
	}

	if _, err := service.Authenticate(context.Background(), []byte("secret"), "10.0.0.3", "owner@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow.Add(2*time.Minute)); !errors.Is(err, ErrAuthLoginRateLimited) {
		t.Fatalf("expected ErrAuthLoginRateLimited on distributed attempt, got %v", err)
	}
	if auth.calls != 2 {
		t.Fatalf("expected auth service to be skipped after identity limit, got %d calls", auth.calls)
	}
}

// TestLoginServiceAuthenticateClearsTheIdentityCounterOnASuccessfulSignIn pins
// the counter reset that follows a correct password. Every rate-limit test
// drives failures only and asserts the lock, so deleting the Reset call left
// them all green — and an owner who mistypes on Monday, signs in, and mistypes
// again on Tuesday would be locked out of their own instance by attempts that
// were already answered correctly.
func TestLoginServiceAuthenticateClearsTheIdentityCounterOnASuccessfulSignIn(t *testing.T) {
	auth := &stubLoginAuthService{user: models.User{ID: 31}, err: ErrAuthInvalidCreds}
	service := NewLoginService(auth, &stubLoginResetTokenIssuer{}, NewAttemptLimiter())
	service.ConfigureAttemptLimits(2, time.Hour)

	secret := []byte("secret")
	const clientKey = "10.0.0.1"
	const email = "owner@example.com"

	if _, err := service.Authenticate(context.Background(), secret, clientKey, email, "wrong", loginServiceTestTTL, loginServiceTestNow); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected invalid credentials on the first attempt, got %v", err)
	}

	auth.err = nil
	if _, err := service.Authenticate(context.Background(), secret, clientKey, email, "StrongPass1", loginServiceTestTTL, loginServiceTestNow.Add(time.Minute)); err != nil {
		t.Fatalf("Authenticate() with the correct password: unexpected error: %v", err)
	}

	// One more failure. With the counter cleared this is the FIRST recent
	// failure and the next attempt still reaches the credential check; without
	// the reset it is the second and the identity is locked.
	auth.err = ErrAuthInvalidCreds
	if _, err := service.Authenticate(context.Background(), secret, clientKey, email, "wrong", loginServiceTestTTL, loginServiceTestNow.Add(2*time.Minute)); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected invalid credentials after the successful sign-in, got %v", err)
	}

	callsBefore := auth.calls
	if _, err := service.Authenticate(context.Background(), secret, clientKey, email, "wrong", loginServiceTestTTL, loginServiceTestNow.Add(3*time.Minute)); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected the successful sign-in to have cleared the earlier failure, got %v", err)
	}
	if auth.calls != callsBefore+1 {
		t.Fatalf("expected the credential check to run again after the reset, got %d calls", auth.calls-callsBefore)
	}
}

func TestLoginServiceAuthenticateMapsResetTokenIssueError(t *testing.T) {
	auth := &stubLoginAuthService{user: models.User{ID: 12, MustChangePassword: true}}
	reset := &stubLoginResetTokenIssuer{err: errors.New("sign failed")}
	service := NewLoginService(auth, reset, NewAttemptLimiter())

	if _, err := service.Authenticate(context.Background(), []byte("secret"), "127.0.0.1", "user@example.com", "StrongPass1", loginServiceTestTTL, loginServiceTestNow); !errors.Is(err, ErrLoginResetTokenIssue) {
		t.Fatalf("expected ErrLoginResetTokenIssue, got %v", err)
	}
	if !reset.called || reset.lastUserID != 12 {
		t.Fatalf("expected reset token issuance for user 12")
	}
}

var (
	loginServiceTestNow = time.Date(2026, time.March, 2, 13, 0, 0, 0, time.UTC)
	loginServiceTestTTL = 30 * time.Minute
)

func newLoginServiceForTest(user models.User, authErr error, token string) (*LoginService, *stubLoginResetTokenIssuer) {
	auth := &stubLoginAuthService{user: user, err: authErr}
	reset := &stubLoginResetTokenIssuer{token: token}
	return NewLoginService(auth, reset, NewAttemptLimiter()), reset
}
