package services

import (
	"context"
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	DefaultRecoveryAttemptsLimit  = 8
	DefaultRecoveryAttemptsWindow = 15 * time.Minute
)

var (
	ErrPasswordRecoveryRateLimited  = errors.New("password recovery rate limited")
	ErrPasswordRecoveryInputInvalid = errors.New("password recovery input invalid")
	ErrPasswordRecoveryCodeInvalid  = errors.New("password recovery code invalid")
)

type PasswordResetService struct {
	auth           *AuthService
	recoveryPolicy *AuthAttemptPolicy
}

func NewPasswordResetService(auth *AuthService, limiter *AttemptLimiter) *PasswordResetService {
	if limiter == nil {
		limiter = NewAttemptLimiter()
	}
	return &PasswordResetService{
		auth:           auth,
		recoveryPolicy: NewAuthAttemptPolicy("recovery", limiter, DefaultRecoveryAttemptsLimit, DefaultRecoveryAttemptsWindow),
	}
}

func (service *PasswordResetService) ConfigureRecoveryAttemptLimits(attempts int, window time.Duration) {
	service.recoveryPolicy.Configure(attempts, window)
}

func (service *PasswordResetService) IssueResetTokenForUser(secretKey []byte, user *models.User, ttl time.Duration, now time.Time) (string, error) {
	if user == nil {
		return "", ErrAuthUserRequired
	}
	return service.auth.BuildPasswordResetToken(secretKey, user.ID, user.PasswordHash, ttl, now)
}

// StartRecovery mints a password-reset token for an owner who proves TWO
// factors: the account's current password AND its recovery code. The recovery
// code stands in for the second factor (TOTP), never for the password — an
// owner whose TOTP secret stopped decrypting after a SECRET_KEY rotation still
// knows the password, while an attacker who photographed the recovery code once
// no longer holds a standing takeover credential.
//
// Every failure below — unknown address, local auth disabled, wrong recovery
// code, wrong or absent password, unsupported role — returns the SAME
// ErrPasswordRecoveryCodeInvalid and books the SAME attempt failure, so the
// route reveals neither account existence nor which operand was wrong.
func (service *PasswordResetService) StartRecovery(ctx context.Context, secretKey []byte, limiterKey string, email string, rawRecoveryCode string, password string, now time.Time, tokenTTL time.Duration) (string, error) {
	if service.auth == nil {
		return "", errors.New("auth service is required")
	}
	if now.IsZero() {
		now = time.Now()
	}

	normalizedEmail := NormalizeAuthEmail(email)
	if service.recoveryPolicy.TooManyRecent(secretKey, limiterKey, normalizedEmail, now) {
		return "", ErrPasswordRecoveryRateLimited
	}
	if normalizedEmail == "" {
		service.recoveryPolicy.AddFailure(secretKey, limiterKey, "", now)
		return "", ErrPasswordRecoveryInputInvalid
	}

	code := NormalizeRecoveryCode(rawRecoveryCode)
	if err := ValidateRecoveryCodeFormat(code); err != nil {
		service.recoveryPolicy.AddFailure(secretKey, limiterKey, normalizedEmail, now)
		return "", ErrPasswordRecoveryCodeInvalid
	}

	user, err := service.auth.FindUserByEmailRecoveryCodeAndPassword(ctx, normalizedEmail, code, password)
	if err != nil {
		if errors.Is(err, ErrRecoveryCodeNotFound) {
			service.recoveryPolicy.AddFailure(secretKey, limiterKey, normalizedEmail, now)
			return "", ErrPasswordRecoveryCodeInvalid
		}
		return "", err
	}

	token, err := service.auth.BuildPasswordResetToken(secretKey, user.ID, user.PasswordHash, tokenTTL, now)
	if err != nil {
		return "", err
	}

	service.recoveryPolicy.Reset(secretKey, limiterKey, normalizedEmail)
	return token, nil
}

func (service *PasswordResetService) CompleteReset(ctx context.Context, secretKey []byte, rawToken string, password string, confirmPassword string, now time.Time) (*models.User, string, error) {
	if service.auth == nil {
		return nil, "", errors.New("auth service is required")
	}
	if err := service.auth.ValidateResetPasswordInput(password, confirmPassword); err != nil {
		return nil, "", err
	}

	user, err := service.auth.ResolveUserByResetToken(ctx, secretKey, rawToken, now)
	if err != nil {
		return nil, "", ErrInvalidResetToken
	}

	// Capture the password hash that the token was validated against before
	// the write. ResetPasswordAndRotateRecoveryCodeCAS passes it as a CAS
	// predicate so only one of any concurrent redeems of the same token can
	// win; the loser receives ErrResetTokenAlreadyConsumed.
	oldPasswordHash := user.PasswordHash

	recoveryCode, err := service.auth.ResetPasswordAndRotateRecoveryCodeCAS(ctx, user, oldPasswordHash, password)
	if err != nil {
		return nil, "", err
	}

	return user, recoveryCode, nil
}
