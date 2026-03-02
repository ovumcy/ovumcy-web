package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type stubLoginAuthService struct {
	user models.User
	err  error
}

func (stub *stubLoginAuthService) AuthenticateCredentials(string, string) (models.User, error) {
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

func TestLoginServiceAuthenticate(t *testing.T) {
	now := time.Date(2026, time.March, 2, 13, 0, 0, 0, time.UTC)
	ttl := 30 * time.Minute

	t.Run("valid credentials without forced reset", func(t *testing.T) {
		auth := &stubLoginAuthService{
			user: models.User{ID: 7, MustChangePassword: false},
		}
		reset := &stubLoginResetTokenIssuer{token: "token"}
		service := NewLoginService(auth, reset)

		result, err := service.Authenticate([]byte("secret"), "user@example.com", "StrongPass1", ttl, now)
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
	})

	t.Run("forced reset issues token", func(t *testing.T) {
		auth := &stubLoginAuthService{
			user: models.User{ID: 9, MustChangePassword: true},
		}
		reset := &stubLoginResetTokenIssuer{token: "issued-reset-token"}
		service := NewLoginService(auth, reset)

		result, err := service.Authenticate([]byte("secret"), "user@example.com", "StrongPass1", ttl, now)
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
		if reset.lastTTL != ttl {
			t.Fatalf("expected reset ttl %s, got %s", ttl, reset.lastTTL)
		}
	})

	t.Run("invalid credentials propagated", func(t *testing.T) {
		authErr := ErrAuthInvalidCreds
		auth := &stubLoginAuthService{err: authErr}
		reset := &stubLoginResetTokenIssuer{token: "unused"}
		service := NewLoginService(auth, reset)

		if _, err := service.Authenticate([]byte("secret"), "user@example.com", "wrong", ttl, now); !errors.Is(err, authErr) {
			t.Fatalf("expected auth error %v, got %v", authErr, err)
		}
		if reset.called {
			t.Fatalf("did not expect reset token issuance on auth error")
		}
	})

	t.Run("token issue error mapped", func(t *testing.T) {
		auth := &stubLoginAuthService{
			user: models.User{ID: 12, MustChangePassword: true},
		}
		reset := &stubLoginResetTokenIssuer{err: errors.New("sign failed")}
		service := NewLoginService(auth, reset)

		if _, err := service.Authenticate([]byte("secret"), "user@example.com", "StrongPass1", ttl, now); !errors.Is(err, ErrLoginResetTokenIssue) {
			t.Fatalf("expected ErrLoginResetTokenIssue, got %v", err)
		}
		if !reset.called || reset.lastUserID != 12 {
			t.Fatalf("expected reset token issuance for user 12")
		}
	})
}
