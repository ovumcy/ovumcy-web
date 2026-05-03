package services

import (
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var ErrLoginResetTokenIssue = errors.New("login reset token issue")
var ErrAuthLoginRateLimited = errors.New("auth login rate limited")

type LoginAuthService interface {
	AuthenticateCredentials(email string, password string) (models.User, error)
}

type LoginResetTokenIssuer interface {
	IssueResetTokenForUser(secretKey []byte, user *models.User, ttl time.Duration, now time.Time) (string, error)
}

type LoginService struct {
	auth          LoginAuthService
	reset         LoginResetTokenIssuer
	attemptPolicy *AuthAttemptPolicy
}

type LoginResult struct {
	User                  models.User
	RequiresPasswordReset bool
	ResetToken            string
	RequiresTOTP          bool
}

func NewLoginService(auth LoginAuthService, reset LoginResetTokenIssuer, limiter *AttemptLimiter) *LoginService {
	return &LoginService{
		auth:          auth,
		reset:         reset,
		attemptPolicy: NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow),
	}
}

func (service *LoginService) ConfigureAttemptLimits(attempts int, window time.Duration) {
	service.attemptPolicy.Configure(attempts, window)
}

func (service *LoginService) Authenticate(
	secretKey []byte,
	clientKey string,
	email string,
	password string,
	resetTokenTTL time.Duration,
	now time.Time,
) (LoginResult, error) {
	normalizedEmail := NormalizeAuthEmail(email)
	if service.attemptPolicy.TooManyRecent(secretKey, clientKey, normalizedEmail, now) {
		return LoginResult{}, ErrAuthLoginRateLimited
	}

	user, err := service.auth.AuthenticateCredentials(email, password)
	if err != nil {
		if errors.Is(err, ErrAuthInvalidCreds) {
			service.attemptPolicy.AddFailure(secretKey, clientKey, normalizedEmail, now)
		}
		return LoginResult{}, err
	}

	service.attemptPolicy.Reset(secretKey, clientKey, normalizedEmail)
	result := LoginResult{User: user}
	if !user.MustChangePassword {
		if user.TOTPEnabled {
			result.RequiresTOTP = true
			return result, nil
		}
		return result, nil
	}

	token, err := service.reset.IssueResetTokenForUser(secretKey, &user, resetTokenTTL, now)
	if err != nil {
		return LoginResult{}, ErrLoginResetTokenIssue
	}
	result.RequiresPasswordReset = true
	result.ResetToken = token
	return result, nil
}
