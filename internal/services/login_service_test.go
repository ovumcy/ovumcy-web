package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type stubLoginAuthService struct {
	user  models.User
	err   error
	calls int
}

func (stub *stubLoginAuthService) AuthenticateCredentials(string, string) (models.User, error) {
	stub.calls++
	if stub.err != nil {
		return models.User{}, stub.err
	}
	return stub.user, nil
}

type stubLoginResetTokenIssuer struct {
	token      string
	err        error
	called     bool
	lastUserID uint
	lastTTL    time.Duration
}

func (stub *stubLoginResetTokenIssuer) IssueResetTokenForUser(_ []byte, user *models.User, ttl time.Duration, _ time.Time) (string, error) {
	stub.called = true
	if user != nil {
		stub.lastUserID = user.ID
	}
	stub.lastTTL = ttl
	if stub.err != nil {
		return "", stub.err
	}
	return stub.token, nil
}

func TestLoginServiceAuthenticateWithoutForcedReset(t *testing.T) {
	service, reset := newLoginServiceForTest(models.User{ID: 7, MustChangePassword: false}, nil, "token")

	result, err := service.Authenticate([]byte("secret"), "127.0.0.1", "user@example.com", "StrongPass1", loginServiceTestTTL, loginServiceTestNow)
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

	result, err := service.Authenticate([]byte("secret"), "127.0.0.1", "user@example.com", "StrongPass1", loginServiceTestTTL, loginServiceTestNow)
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
}

func TestLoginServiceAuthenticatePropagatesInvalidCredentials(t *testing.T) {
	authErr := ErrAuthInvalidCreds
	service, reset := newLoginServiceForTest(models.User{}, authErr, "unused")

	if _, err := service.Authenticate([]byte("secret"), "127.0.0.1", "user@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow); !errors.Is(err, authErr) {
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

	if _, err := service.Authenticate([]byte("secret"), "10.0.0.1", "owner@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected invalid credentials on first attempt, got %v", err)
	}
	if _, err := service.Authenticate([]byte("secret"), "10.0.0.2", "owner@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow.Add(time.Minute)); !errors.Is(err, ErrAuthInvalidCreds) {
		t.Fatalf("expected invalid credentials on second attempt, got %v", err)
	}

	if _, err := service.Authenticate([]byte("secret"), "10.0.0.3", "owner@example.com", "wrong", loginServiceTestTTL, loginServiceTestNow.Add(2*time.Minute)); !errors.Is(err, ErrAuthLoginRateLimited) {
		t.Fatalf("expected ErrAuthLoginRateLimited on distributed attempt, got %v", err)
	}
	if auth.calls != 2 {
		t.Fatalf("expected auth service to be skipped after identity limit, got %d calls", auth.calls)
	}
}

func TestLoginServiceAuthenticateMapsResetTokenIssueError(t *testing.T) {
	auth := &stubLoginAuthService{user: models.User{ID: 12, MustChangePassword: true}}
	reset := &stubLoginResetTokenIssuer{err: errors.New("sign failed")}
	service := NewLoginService(auth, reset, NewAttemptLimiter())

	if _, err := service.Authenticate([]byte("secret"), "127.0.0.1", "user@example.com", "StrongPass1", loginServiceTestTTL, loginServiceTestNow); !errors.Is(err, ErrLoginResetTokenIssue) {
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
