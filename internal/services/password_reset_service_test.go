package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

func TestPasswordResetServiceStartRecoveryRateLimited(t *testing.T) {
	repo := &stubAuthUserRepo{}
	authService := NewAuthService(repo)
	limiter := NewAttemptLimiter()
	service := NewPasswordResetService(authService, limiter)
	secretKey := []byte("test-secret")

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"
	for index := 0; index < DefaultRecoveryAttemptsLimit; index++ {
		service.recoveryPolicy.AddFailure(secretKey, key, "owner@example.com", now.Add(-1*time.Minute))
	}

	_, err := service.StartRecovery(secretKey, key, "owner@example.com", "OVUM-ABCD-2345-EFGH", now, 30*time.Minute)
	if !errors.Is(err, ErrPasswordRecoveryRateLimited) {
		t.Fatalf("expected ErrPasswordRecoveryRateLimited, got %v", err)
	}
}

func TestPasswordResetServiceStartRecoveryInvalidEmailAddsFailure(t *testing.T) {
	repo := &stubAuthUserRepo{}
	authService := NewAuthService(repo)
	limiter := NewAttemptLimiter()
	service := NewPasswordResetService(authService, limiter)
	service.ConfigureRecoveryAttemptLimits(1, DefaultRecoveryAttemptsWindow)
	secretKey := []byte("test-secret")

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"

	_, err := service.StartRecovery(secretKey, key, "invalid-email", "OVUM-ABCD-2345-EFGH", now, 30*time.Minute)
	if !errors.Is(err, ErrPasswordRecoveryInputInvalid) {
		t.Fatalf("expected ErrPasswordRecoveryInputInvalid, got %v", err)
	}
	if !service.recoveryPolicy.TooManyRecent(secretKey, key, "", now) {
		t.Fatalf("expected limiter to record failed recovery attempt")
	}
}

func TestPasswordResetServiceStartRecoveryInvalidCodeAddsFailure(t *testing.T) {
	repo := &stubAuthUserRepo{}
	authService := NewAuthService(repo)
	limiter := NewAttemptLimiter()
	service := NewPasswordResetService(authService, limiter)
	service.ConfigureRecoveryAttemptLimits(1, DefaultRecoveryAttemptsWindow)
	secretKey := []byte("test-secret")

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"

	_, err := service.StartRecovery(secretKey, key, "owner@example.com", "invalid", now, 30*time.Minute)
	if !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected ErrPasswordRecoveryCodeInvalid, got %v", err)
	}
	if !service.recoveryPolicy.TooManyRecent(secretKey, key, "owner@example.com", now) {
		t.Fatalf("expected limiter to record failed recovery attempt")
	}
}

func TestPasswordResetServiceStartRecoveryRateLimitsByIdentityAcrossIPs(t *testing.T) {
	repo := &stubAuthUserRepo{}
	authService := NewAuthService(repo)
	service := NewPasswordResetService(authService, NewAttemptLimiter())
	service.ConfigureRecoveryAttemptLimits(2, time.Hour)
	secretKey := []byte("test-secret")

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	code := "invalid"

	if _, err := service.StartRecovery(secretKey, "10.0.0.1", "owner@example.com", code, now, 30*time.Minute); !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected invalid recovery code on first attempt, got %v", err)
	}
	if _, err := service.StartRecovery(secretKey, "10.0.0.2", "owner@example.com", code, now.Add(time.Minute), 30*time.Minute); !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected invalid recovery code on second attempt, got %v", err)
	}
	if _, err := service.StartRecovery(secretKey, "10.0.0.3", "owner@example.com", code, now.Add(2*time.Minute), 30*time.Minute); !errors.Is(err, ErrPasswordRecoveryRateLimited) {
		t.Fatalf("expected ErrPasswordRecoveryRateLimited after distributed attempts, got %v", err)
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
			Email:            "owner@example.com",
			PasswordHash:     string(passwordHash),
			RecoveryCodeHash: recoveryHash,
			LocalAuthEnabled: true,
		},
	}
	authService := NewAuthService(repo)
	limiter := NewAttemptLimiter()
	service := NewPasswordResetService(authService, limiter)
	service.ConfigureRecoveryAttemptLimits(2, DefaultRecoveryAttemptsWindow)
	secretKey := []byte("test-secret")

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"
	service.recoveryPolicy.AddFailure(secretKey, key, "owner@example.com", now.Add(-1*time.Minute))

	token, err := service.StartRecovery(secretKey, key, "owner@example.com", recoveryCode, now, 30*time.Minute)
	if err != nil {
		t.Fatalf("StartRecovery() unexpected error: %v", err)
	}
	if token == "" {
		t.Fatalf("expected non-empty reset token")
	}
	if service.recoveryPolicy.limiter.TooManyRecentAny(service.recoveryPolicy.keys(secretKey, key, "owner@example.com"), now, 1, DefaultRecoveryAttemptsWindow) {
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
			ID:               42,
			PasswordHash:     string(passwordHash),
			LocalAuthEnabled: true,
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
