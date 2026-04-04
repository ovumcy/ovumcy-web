package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) authenticateRequest(c *fiber.Ctx) (*models.User, error) {
	rawToken := strings.TrimSpace(c.Cookies(authCookieName))
	if rawToken == "" {
		return nil, errors.New("missing auth cookie")
	}

	if !strings.HasPrefix(rawToken, secureCookieVersion+".") {
		handler.clearAuthCookie(c)
		handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "invalid token"))
		return nil, errors.New("invalid token")
	}

	tokenValue, err := handler.decodeSealedAuthCookieToken(rawToken)
	if err != nil {
		handler.clearAuthCookie(c)
		handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "invalid token"))
		return nil, errors.New("invalid token")
	}

	user, claims, err := handler.authService.ResolveAuthSession(handler.secretKey, tokenValue, time.Now())
	if err != nil {
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			handler.clearAuthRelatedCookies(c)
			handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "unsupported role"))
			return nil, err
		}
		if errors.Is(err, services.ErrAuthSessionTokenRevoked) {
			handler.clearAuthCookie(c)
			handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "revoked session"))
			return nil, errors.New("invalid token")
		}
		switch services.ClassifyAuthSessionResolveError(err) {
		case services.AuthSessionResolveErrorMissing:
			handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "missing auth cookie"))
			return nil, errors.New("missing auth cookie")
		case services.AuthSessionResolveErrorExpired:
			handler.clearAuthCookie(c)
			handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "token expired"))
			return nil, errors.New("token expired")
		case services.AuthSessionResolveErrorInvalid:
			handler.clearAuthCookie(c)
			handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "invalid token"))
			return nil, errors.New("invalid token")
		default:
			handler.logSecurityEvent(c, "auth.session", "failure", securityEventField("reason", "token resolve failed"))
			return nil, err
		}
	}

	c.Locals(contextAuthSessionKey, claims)
	return user, nil
}
