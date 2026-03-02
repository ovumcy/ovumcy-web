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
	user, err := handler.authService.ResolveUserByAuthSessionToken(handler.secretKey, tokenValue, time.Now())
	if err != nil {
		switch services.ClassifyAuthSessionResolveError(err) {
		case services.AuthSessionResolveErrorMissing:
			return nil, errors.New("missing auth cookie")
		case services.AuthSessionResolveErrorExpired:
			return nil, errors.New("token expired")
		case services.AuthSessionResolveErrorInvalid:
			return nil, errors.New("invalid token")
		default:
			return nil, err
		}
	}

	return user, nil
}
