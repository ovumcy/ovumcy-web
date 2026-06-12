package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var authCookieSpec = sealedCookieSpec{name: authCookieName, path: "/"}

func (handler *Handler) setAuthCookie(c *fiber.Ctx, user *models.User, rememberMe bool) (string, error) {
	tokenTTL := defaultAuthTokenTTL
	if rememberMe {
		tokenTTL = rememberAuthTokenTTL
	}

	token, sessionID, err := handler.buildTokenWithSessionID(user, tokenTTL)
	if err != nil {
		return "", err
	}
	// Session-scoped unless remember-me: a zero expires keeps the cookie
	// for the browser session while the token payload carries its own TTL.
	var expires time.Time
	if rememberMe {
		expires = time.Now().Add(tokenTTL)
	}
	if err := handler.writeSealedCookie(c, authCookieSpec, []byte(token), expires); err != nil {
		return "", err
	}
	return sessionID, nil
}

func (handler *Handler) clearAuthCookie(c *fiber.Ctx) {
	handler.clearSealedCookie(c, authCookieSpec)
}

func (handler *Handler) clearAuthRelatedCookies(c *fiber.Ctx) {
	handler.clearAuthCookie(c)
	handler.clearOIDCLogoutBridgeCookie(c)
	handler.clearRecoveryCodePageCookie(c)
	handler.clearResetPasswordCookie(c)
	handler.clearTOTPPendingCookie(c)
}

func (handler *Handler) buildTokenWithSessionID(user *models.User, ttl time.Duration) (string, string, error) {
	if user == nil {
		return "", "", errors.New("user is required")
	}
	if err := services.ValidateSupportedWebUser(user); err != nil {
		return "", "", err
	}
	if ttl <= 0 {
		ttl = defaultAuthTokenTTL
	}
	return handler.authService.BuildAuthSessionTokenWithSessionID(handler.secretKey, user.ID, user.Role, user.AuthSessionVersion, ttl, time.Now())
}

func (handler *Handler) rotateOIDCLogoutState(c *fiber.Ctx, newSessionID string) error {
	if handler == nil || handler.oidcLogoutStateSvc == nil {
		return nil
	}

	newSessionID = strings.TrimSpace(newSessionID)
	if newSessionID == "" {
		return nil
	}

	currentSession, ok := currentAuthSession(c)
	if !ok || currentSession == nil {
		return nil
	}

	oldSessionID := strings.TrimSpace(currentSession.SessionID)
	if oldSessionID == "" || oldSessionID == newSessionID {
		return nil
	}

	logoutState, found, err := handler.oidcLogoutStateSvc.Load(c.UserContext(), oldSessionID, time.Now())
	if err != nil || !found {
		return err
	}
	if !validOIDCLogoutState(logoutState) {
		return handler.oidcLogoutStateSvc.Delete(c.UserContext(), oldSessionID) // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
	}
	if err := handler.oidcLogoutStateSvc.Save(c.UserContext(), newSessionID, logoutState, time.Now()); err != nil { // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
		return err
	}
	return handler.oidcLogoutStateSvc.Delete(c.UserContext(), oldSessionID) // codecov:ignore -- OIDC logout-state rotation; covered by the e2e OIDC lanes
}

// refreshCurrentSession re-issues the auth cookie for the request's user
// after an operation that bumped auth_session_version, so the originating
// device stays signed in while every other session is invalidated. The
// `scope` argument is used for security-event logging only.
func (handler *Handler) refreshCurrentSession(c *fiber.Ctx, user *models.User, scope string) error {
	sessionID, err := handler.setAuthCookie(c, user, false)
	if err != nil {
		handler.clearAuthCookie(c)
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authWebSignInUnavailableErrorSpec()
		}
		handler.logSecurityError(c, scope, spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.rotateOIDCLogoutState(c, sessionID); err != nil {
		handler.logSecurityEvent(c, scope, "provider_logout_state_rotation_failed")
	}
	return nil
}

func (handler *Handler) encodeAuthCookieToken(rawToken string) (string, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return "", errors.New("auth token is required")
	}

	codec, err := handler.cookieCodec()
	if err != nil {
		return "", err
	}
	return codec.seal(authCookieName, []byte(rawToken))
}

func (handler *Handler) decodeSealedAuthCookieToken(rawValue string) (string, error) {
	codec, err := handler.cookieCodec()
	if err != nil {
		return "", err
	}

	plaintext, err := codec.open(authCookieName, rawValue)
	if err != nil {
		return "", err
	}

	token := strings.TrimSpace(string(plaintext))
	if token == "" {
		return "", errors.New("auth token is required")
	}
	return token, nil
}
