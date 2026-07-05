package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) authenticateRequest(c fiber.Ctx) (*models.User, error) {
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

	user, claims, err := handler.authService.ResolveAuthSession(c.Context(), handler.secretKey, tokenValue, time.Now())
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

// persistRequestTimezone updates the authenticated owner's stored IANA timezone
// when the current request carries a valid one that differs from the persisted
// value. It is the write half of the per-user timezone persistence that the
// request-free reminder pass (issue #124) reads.
//
// Scope safety: the target id is taken solely from the resolved session user
// (user.ID from the sealed auth cookie), never from any request path, query, or
// body — a request can only ever rewrite its own owner's timezone. Only a value
// that clears the shared request-timezone validator (requestTimezoneName) is
// ever written; unsafe/malformed input and the "Local" token are rejected and
// leave the column untouched. The write is skipped entirely when the value is
// unchanged, so the common request path issues no UPDATE.
//
// Best-effort: a persistence failure is logged and swallowed — it must not fail
// an otherwise-valid authenticated request, and the timezone column is not a
// security-posture field (no session-version bump).
func (handler *Handler) persistRequestTimezone(c fiber.Ctx, user *models.User) {
	if handler.settingsService == nil || user == nil {
		return
	}
	timezoneName, ok := requestTimezoneName(c.Get(timezoneHeaderName), c.Cookies(timezoneCookieName))
	if !ok {
		return
	}
	if _, err := handler.settingsService.PersistTimezone(c.Context(), user.ID, user.Timezone, timezoneName); err != nil {
		handler.logSecurityEvent(c, "user.timezone", "failure", securityEventField("reason", "persist failed"))
		return
	}
	// Keep the in-request user copy consistent so downstream handlers and a
	// second middleware pass observe the freshly persisted value.
	user.Timezone = timezoneName
}
