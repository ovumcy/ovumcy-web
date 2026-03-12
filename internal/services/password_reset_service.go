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

func (service *PasswordResetService) ParseResetToken(secretKey []byte, rawToken string, now time.Time) (*PasswordResetClaims, error) {
	return ParsePasswordResetToken(secretKey, rawToken, now)
}

func (service *PasswordResetService) StartRecovery(secretKey []byte, limiterKey string, email string, rawRecoveryCode string, now time.Time, tokenTTL time.Duration) (string, error) {
	if service.auth == nil {
		return "", errors.New("auth service is required")
	}
	if now.IsZero() {
		now = time.Now()
	}

	normalizedEmail := NormalizeAuthEmail(email)
	if service.recoveryPolicy.TooManyRecent(limiterKey, normalizedEmail, now) {
		return "", ErrPasswordRecoveryRateLimited
	}
	if normalizedEmail == "" {
		service.recoveryPolicy.AddFailure(limiterKey, "", now)
		return "", ErrPasswordRecoveryInputInvalid
	}

	code := NormalizeRecoveryCode(rawRecoveryCode)
	if err := ValidateRecoveryCodeFormat(code); err != nil {
		service.recoveryPolicy.AddFailure(limiterKey, normalizedEmail, now)
		return "", ErrPasswordRecoveryCodeInvalid
	}

	user, err := service.auth.FindUserByEmailAndRecoveryCode(normalizedEmail, code)
	if err != nil {
		if errors.Is(err, ErrRecoveryCodeNotFound) {
			service.recoveryPolicy.AddFailure(limiterKey, normalizedEmail, now)
			return "", ErrPasswordRecoveryCodeInvalid
		}
		return "", err
	}

	token, err := service.auth.BuildPasswordResetToken(secretKey, user.ID, user.PasswordHash, tokenTTL, now)
	if err != nil {
		return "", err
	}

	service.recoveryPolicy.Reset(limiterKey, normalizedEmail)
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
