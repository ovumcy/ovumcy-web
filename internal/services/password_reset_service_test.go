package services

import (
	"context"
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
	for range DefaultRecoveryAttemptsLimit {
		service.recoveryPolicy.AddFailure(secretKey, key, "owner@example.com", now.Add(-1*time.Minute))
	}

	_, err := service.StartRecovery(context.Background(), secretKey, key, "owner@example.com", "OVUM-ABCD-2345-EFGH", "StrongPass1", now, 30*time.Minute)
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

	_, err := service.StartRecovery(context.Background(), secretKey, key, "invalid-email", "OVUM-ABCD-2345-EFGH", "StrongPass1", now, 30*time.Minute)
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

	_, err := service.StartRecovery(context.Background(), secretKey, key, "owner@example.com", "invalid", "StrongPass1", now, 30*time.Minute)
	if !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected ErrPasswordRecoveryCodeInvalid, got %v", err)
	}
	if !service.recoveryPolicy.TooManyRecent(secretKey, key, "owner@example.com", now) {
		t.Fatalf("expected limiter to record failed recovery attempt")
	}
}

// TestPasswordResetServiceStartRecoveryWrongPasswordAddsFailure pins that the
// new operand is inside the attempt budget: a correct recovery code with a
// wrong password must return the SAME ErrPasswordRecoveryCodeInvalid as a wrong
// code and must consume an attempt, or the password join would be an unmetered
// guessing surface.
func TestPasswordResetServiceStartRecoveryWrongPasswordAddsFailure(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	repo := &stubAuthUserRepo{
		findByEmailOptionalEmail: "owner@example.com",
		findByEmailOptionalFound: true,
		findByEmailOptionalUser: models.User{
			ID:               77,
			Email:            "owner@example.com",
			PasswordHash:     string(passwordHash),
			RecoveryCodeHash: recoveryHash,
			LocalAuthEnabled: true,
			Role:             models.RoleOwner,
		},
	}
	service := NewPasswordResetService(NewAuthService(repo), NewAttemptLimiter())
	service.ConfigureRecoveryAttemptLimits(1, DefaultRecoveryAttemptsWindow)
	secretKey := []byte("test-secret")

	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"

	if _, err := service.StartRecovery(context.Background(), secretKey, key, "owner@example.com", recoveryCode, "NotThePassword9", now, 30*time.Minute); !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected ErrPasswordRecoveryCodeInvalid for a wrong password, got %v", err)
	}
	if !service.recoveryPolicy.TooManyRecent(secretKey, key, "owner@example.com", now) {
		t.Fatalf("expected limiter to record the wrong-password recovery attempt")
	}
}

// TestPasswordResetServiceStartRecoveryAnswersAnUnknownAddressLikeAKnownOne
// pins the enumeration-safe collapse on the branch a MALFORMED code never
// reaches. Both negative cases in this file submit "invalid", which
// ValidateRecoveryCodeFormat refuses before the lookup runs, so the not-found
// mapping and the attempt it books were exercised by nothing: an unknown
// address could start answering differently — or for free, outside the attempt
// budget — and this file stayed green. The two calls below differ only in
// whether the account exists, and everything observable about them must match.
func TestPasswordResetServiceStartRecoveryAnswersAnUnknownAddressLikeAKnownOne(t *testing.T) {
	recoveryCode, recoveryHash, err := GenerateRecoveryCodeHash()
	if err != nil {
		t.Fatalf("GenerateRecoveryCodeHash() unexpected error: %v", err)
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	newService := func() *PasswordResetService {
		repo := &stubAuthUserRepo{
			findByEmailOptionalEmail: "owner@example.com",
			findByEmailOptionalFound: true,
			findByEmailOptionalUser: models.User{
				ID:               77,
				Email:            "owner@example.com",
				PasswordHash:     string(passwordHash),
				RecoveryCodeHash: recoveryHash,
				LocalAuthEnabled: true,
				Role:             models.RoleOwner,
			},
		}
		service := NewPasswordResetService(NewAuthService(repo), NewAttemptLimiter())
		service.ConfigureRecoveryAttemptLimits(1, DefaultRecoveryAttemptsWindow)
		return service
	}

	secretKey := []byte("test-secret")
	now := time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC)
	key := "127.0.0.1"

	// A well-formed code the format check accepts, so both calls reach the
	// lookup. The known address gets the wrong code; the unknown address gets
	// the right one and does not exist.
	unknownService := newService()
	_, unknownErr := unknownService.StartRecovery(context.Background(), secretKey, key, "stranger@example.com", recoveryCode, "StrongPass1", now, 30*time.Minute)

	knownService := newService()
	_, knownErr := knownService.StartRecovery(context.Background(), secretKey, key, "owner@example.com", "OVUM-ABCD-2345-EFGH", "StrongPass1", now, 30*time.Minute)

	if !errors.Is(unknownErr, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("an unknown address must answer with ErrPasswordRecoveryCodeInvalid, got %v", unknownErr)
	}
	if !errors.Is(knownErr, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("a known address with a wrong code must answer with ErrPasswordRecoveryCodeInvalid, got %v", knownErr)
	}
	if unknownErr != knownErr {
		t.Fatalf("the two answers must be the same value, got %v and %v", unknownErr, knownErr)
	}

	// Both must also cost an attempt: an unmetered miss on a non-existent
	// address is a free oracle even when the error text matches.
	if !unknownService.recoveryPolicy.TooManyRecent(secretKey, key, "stranger@example.com", now) {
		t.Fatal("expected the unknown-address attempt to be booked against the limiter")
	}
	if !knownService.recoveryPolicy.TooManyRecent(secretKey, key, "owner@example.com", now) {
		t.Fatal("expected the wrong-code attempt to be booked against the limiter")
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

	if _, err := service.StartRecovery(context.Background(), secretKey, "10.0.0.1", "owner@example.com", code, "StrongPass1", now, 30*time.Minute); !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected invalid recovery code on first attempt, got %v", err)
	}
	if _, err := service.StartRecovery(context.Background(), secretKey, "10.0.0.2", "owner@example.com", code, "StrongPass1", now.Add(time.Minute), 30*time.Minute); !errors.Is(err, ErrPasswordRecoveryCodeInvalid) {
		t.Fatalf("expected invalid recovery code on second attempt, got %v", err)
	}
	if _, err := service.StartRecovery(context.Background(), secretKey, "10.0.0.3", "owner@example.com", code, "StrongPass1", now.Add(2*time.Minute), 30*time.Minute); !errors.Is(err, ErrPasswordRecoveryRateLimited) {
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
			Role:             models.RoleOwner,
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

	token, err := service.StartRecovery(context.Background(), secretKey, key, "owner@example.com", recoveryCode, "StrongPass1", now, 30*time.Minute)
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
			Role:             models.RoleOwner,
		},
	}
	authService := NewAuthService(repo)
	service := NewPasswordResetService(authService, nil)

	token, err := authService.BuildPasswordResetToken(secret, 42, repo.user.PasswordHash, 30*time.Minute, now)
	if err != nil {
		t.Fatalf("BuildPasswordResetToken() unexpected error: %v", err)
	}

	user, recoveryCode, err := service.CompleteReset(context.Background(), secret, token, "EvenStronger2", "EvenStronger2", now.Add(1*time.Minute))
	if err != nil {
		t.Fatalf("CompleteReset() unexpected error: %v", err)
	}
	if user == nil || user.ID != 42 {
		t.Fatalf("expected resolved user 42, got %#v", user)
	}
	// The repository stub serves one embedded user, so the returned struct
	// would look identical for a write scoped to any account. Pin the id the
	// credential write was actually addressed to.
	if repo.updatedUserID != 42 {
		t.Fatalf("expected the reset write to be scoped to owner 42, got %d", repo.updatedUserID)
	}
	if recoveryCode == "" {
		t.Fatalf("expected non-empty rotated recovery code")
	}
}
