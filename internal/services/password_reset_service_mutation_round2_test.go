package services

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPasswordResetServiceStartRecoveryHonorsSharedLimiter(t *testing.T) {
	// Production wires one AttemptLimiter into several services (see
	// cmd/ovumcy/main.go and the api test harness). The recovery policy must
	// use the caller-supplied limiter so cross-service attempt counts are
	// coordinated. We model this with two services that share one limiter:
	// a failure recorded through serviceB's policy must be visible to
	// serviceA's rate-limit check.
	repo := &stubAuthUserRepo{}
	authService := NewAuthService(repo)
	sharedLimiter := NewAttemptLimiter()

	serviceA := NewPasswordResetService(authService, sharedLimiter)
	serviceA.ConfigureRecoveryAttemptLimits(1, DefaultRecoveryAttemptsWindow)
	serviceB := NewPasswordResetService(authService, sharedLimiter)
	serviceB.ConfigureRecoveryAttemptLimits(1, DefaultRecoveryAttemptsWindow)

	secretKey := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"
	email := "owner@example.com"

	// Seed a recovery failure through the *other* service that shares the
	// same limiter instance.
	serviceB.recoveryPolicy.AddFailure(secretKey, key, email, now.Add(-1*time.Minute))

	// serviceA must observe the shared failure and rate-limit. Use a valid
	// email + bad code so that, absent the shared count, the call would
	// instead return ErrPasswordRecoveryCodeInvalid -- making the two
	// outcomes distinguishable.
	_, err := serviceA.StartRecovery(context.Background(), secretKey, key, email, "invalid", now, 30*time.Minute)
	if !errors.Is(err, ErrPasswordRecoveryRateLimited) {
		t.Fatalf("expected ErrPasswordRecoveryRateLimited from shared limiter, got %v", err)
	}
}
