package services

import (
	"errors"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var ErrLoginResetTokenIssue = errors.New("login reset token issue")

type LoginAuthService interface {
	AuthenticateCredentials(email string, password string) (models.User, error)
}

type LoginResetTokenIssuer interface {
	IssueResetTokenForUser(secretKey []byte, user *models.User, ttl time.Duration, now time.Time) (string, error)
}

type LoginService struct {
	auth  LoginAuthService
	reset LoginResetTokenIssuer
}

type LoginResult struct {
	User                  models.User
	RequiresPasswordReset bool
	ResetToken            string
}

func NewLoginService(auth LoginAuthService, reset LoginResetTokenIssuer) *LoginService {
	return &LoginService{
		auth:  auth,
		reset: reset,
	}
}

func (service *LoginService) Authenticate(
	secretKey []byte,
	email string,
	password string,
	resetTokenTTL time.Duration,
	now time.Time,
) (LoginResult, error) {
	user, err := service.auth.AuthenticateCredentials(email, password)
	if err != nil {
		return LoginResult{}, err
	}

	result := LoginResult{User: user}
	if !user.MustChangePassword {
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
