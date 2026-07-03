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

	user, claims, err := handler.authService.ResolveAuthSession(c.UserContext(), handler.secretKey, tokenValue, time.Now())
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
		switch {
		// codecov:ignore:start -- unreachable from this call site: decodeSealedAuthCookieToken
		// rejects an empty inner token before ResolveAuthSession runs, so ParseAuthSessionToken
		// never returns ErrAuthSessionTokenMissing here.
		case errors.Is(err, services.ErrAuthSessionTokenMissing):
			handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "missing auth cookie"))
			return nil, errors.New("missing auth cookie")
		// codecov:ignore:end
		case errors.Is(err, services.ErrAuthSessionTokenExpired):
			handler.clearAuthCookie(c)
			handler.logSecurityEvent(c, "auth.session", "denied", securityEventField("reason", "token expired"))
			return nil, errors.New("token expired")
		case errors.Is(err, services.ErrAuthSessionTokenInvalid),
			errors.Is(err, services.ErrAuthSessionTokenInvalidUserID),
			errors.Is(err, services.ErrAuthSessionTokenRevoked),
			errors.Is(err, services.ErrAuthInvalidCreds):
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
