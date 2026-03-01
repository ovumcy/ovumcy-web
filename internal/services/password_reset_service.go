package services

import (
	"errors"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

const (
	DefaultRecoveryAttemptsLimit  = 8
	DefaultRecoveryAttemptsWindow = 15 * time.Minute
)

var (
	ErrPasswordRecoveryRateLimited = errors.New("password recovery rate limited")
	ErrPasswordRecoveryCodeInvalid = errors.New("password recovery code invalid")
)

type PasswordResetService struct {
	auth                 *AuthService
	recoveryLimiter      *AttemptLimiter
	recoveryAttempts     int
	recoveryAttemptWindow time.Duration
}

func NewPasswordResetService(auth *AuthService, limiter *AttemptLimiter) *PasswordResetService {
	if limiter == nil {
		limiter = NewAttemptLimiter()
	}
	return &PasswordResetService{
		auth:                  auth,
		recoveryLimiter:       limiter,
		recoveryAttempts:      DefaultRecoveryAttemptsLimit,
		recoveryAttemptWindow: DefaultRecoveryAttemptsWindow,
	}
}

func (service *PasswordResetService) IssueResetTokenForUser(secretKey []byte, user *models.User, ttl time.Duration, now time.Time) (string, error) {
	if user == nil {
		return "", ErrAuthUserRequired
	}
	return service.auth.BuildPasswordResetToken(secretKey, user.ID, user.PasswordHash, ttl, now)
}

func (service *PasswordResetService) ParseResetToken(secretKey []byte, rawToken string, now time.Time) (*PasswordResetClaims, error) {
	return ParsePasswordResetToken(secretKey, rawToken, now)
}

func (service *PasswordResetService) StartRecovery(secretKey []byte, limiterKey string, rawRecoveryCode string, now time.Time, tokenTTL time.Duration) (string, error) {
	if service.auth == nil {
		return "", errors.New("auth service is required")
	}
	if now.IsZero() {
		now = time.Now()
	}

	key := NormalizeLimiterKey(limiterKey)
	if service.recoveryLimiter.TooManyRecent(key, now, service.recoveryAttempts, service.recoveryAttemptWindow) {
		return "", ErrPasswordRecoveryRateLimited
	}

	code := NormalizeRecoveryCode(rawRecoveryCode)
	if err := ValidateRecoveryCodeFormat(code); err != nil {
		service.recoveryLimiter.AddFailure(key, now, service.recoveryAttemptWindow)
		return "", ErrPasswordRecoveryCodeInvalid
	}

	user, err := service.auth.FindUserByRecoveryCode(code)
	if err != nil {
		if errors.Is(err, ErrRecoveryCodeNotFound) {
			service.recoveryLimiter.AddFailure(key, now, service.recoveryAttemptWindow)
			return "", ErrPasswordRecoveryCodeInvalid
		}
		return "", err
	}

	token, err := service.auth.BuildPasswordResetToken(secretKey, user.ID, user.PasswordHash, tokenTTL, now)
	if err != nil {
		return "", err
	}

	service.recoveryLimiter.Reset(key)
	return token, nil
}

func (service *PasswordResetService) CompleteReset(secretKey []byte, rawToken string, password string, confirmPassword string, now time.Time) (*models.User, string, error) {
	if service.auth == nil {
		return nil, "", errors.New("auth service is required")
	}
	if err := service.auth.ValidateResetPasswordInput(password, confirmPassword); err != nil {
		return nil, "", err
	}

	user, err := service.auth.ResolveUserByResetToken(secretKey, rawToken, now)
	if err != nil {
		return nil, "", ErrInvalidResetToken
	}

	recoveryCode, err := service.auth.ResetPasswordAndRotateRecoveryCode(user, password)
	if err != nil {
		return nil, "", err
	}

	return user, recoveryCode, nil
}
