package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) authenticateRequest(c *fiber.Ctx) (*models.User, error) {
	rawToken := strings.TrimSpace(c.Cookies(authCookieName))
	if rawToken == "" {
		return nil, errors.New("missing auth cookie")
	}
	tokenValue := rawToken
	if strings.HasPrefix(rawToken, secureCookieVersion+".") {
		decodedToken, err := handler.decodeSealedAuthCookieToken(rawToken)
		if err != nil {
			return nil, errors.New("invalid token")
		}
		tokenValue = decodedToken
	}

	handler.ensureDependencies()
	user, err := handler.authService.ResolveUserByAuthSessionToken(handler.secretKey, tokenValue, time.Now())
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAuthSessionTokenMissing):
			return nil, errors.New("missing auth cookie")
		case errors.Is(err, services.ErrAuthSessionTokenExpired):
			return nil, errors.New("token expired")
		case errors.Is(err, services.ErrAuthSessionTokenInvalid),
			errors.Is(err, services.ErrAuthSessionTokenInvalidUserID),
			errors.Is(err, services.ErrAuthInvalidCreds):
			return nil, errors.New("invalid token")
		default:
			return nil, err
		}
	}

	return user, nil
}
