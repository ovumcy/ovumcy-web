package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestPasswordResetServiceStartRecoveryRateLimited(t *testing.T) {
	repo := &stubAuthUserRepo{}
	authService := NewAuthService(repo)
	limiter := NewAttemptLimiter()
	service := NewPasswordResetService(authService, limiter)

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"
	for index := 0; index < DefaultRecoveryAttemptsLimit; index++ {
		limiter.AddFailure(key, now.Add(-1*time.Minute), DefaultRecoveryAttemptsWindow)
	}

	_, err := service.StartRecovery([]byte("test-secret"), key, "OVUM-ABCD-2345-EFGH", now, 30*time.Minute)
	if !errors.Is(err, ErrPasswordRecoveryRateLimited) {
		t.Fatalf("expected ErrPasswordRecoveryRateLimited, got %v", err)
	}
}

func TestPasswordResetServiceStartRecoveryInvalidCodeAddsFailure(t *testing.T) {
	repo := &stubAuthUserRepo{}
	authService := NewAuthService(repo)
	limiter := NewAttemptLimiter()
	service := NewPasswordResetService(authService, limiter)

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"

	_, err := service.StartRecovery([]byte("test-secret"), key, "invalid", now, 30*time.Minute)
	if !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected ErrPasswordRecoveryCodeInvalid, got %v", err)
	}
	if !limiter.TooManyRecent(key, now, 1, DefaultRecoveryAttemptsWindow) {
		t.Fatalf("expected limiter to record failed recovery attempt")
	}
}

func TestPasswordResetServiceStartRecoverySuccessResetsLimiter(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		user: models.User{
			ID:               77,
			PasswordHash:     string(passwordHash),
			RecoveryCodeHash: recoveryHash,
		},
	}
	authService := NewAuthService(repo)
	limiter := NewAttemptLimiter()
	service := NewPasswordResetService(authService, limiter)

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"
	limiter.AddFailure(key, now.Add(-1*time.Minute), DefaultRecoveryAttemptsWindow)

	token, err := service.StartRecovery([]byte("test-secret"), key, recoveryCode, now, 30*time.Minute)
	if err != nil {
		t.Fatalf("StartRecovery() unexpected error: %v", err)
	}
	if token == "" {
		t.Fatalf("expected non-empty reset token")
	}
	if limiter.TooManyRecent(key, now, 1, DefaultRecoveryAttemptsWindow) {
		t.Fatalf("expected limiter reset after successful recovery flow")
	}
}

func TestPasswordResetServiceCompleteReset(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		user: models.User{
			ID:           42,
			PasswordHash: string(passwordHash),
		},
	}
	authService := NewAuthService(repo)
	service := NewPasswordResetService(authService, nil)

	token, err := authService.BuildPasswordResetToken(secret, 42, repo.user.PasswordHash, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken() unexpected error: %v", err)
	}

	user, recoveryCode, err := service.CompleteReset(secret, token, "EvenStronger2", "EvenStronger2", now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("CompleteReset() unexpected error: %v", err)
	}
	if user == nil || user.ID != 42 {
		t.Fatalf("expected resolved user 42, got %#v", user)
	}
	if recoveryCode == "" {
		t.Fatalf("expected non-empty rotated recovery code")
	}
}

