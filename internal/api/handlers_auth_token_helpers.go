package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func (handler *Handler) setAuthCookie(c *fiber.Ctx, user *models.User, rememberMe bool) (string, error) {
	tokenTTL := defaultAuthTokenTTL
	if rememberMe {
		tokenTTL = rememberAuthTokenTTL
	}

	token, sessionID, err := handler.buildTokenWithSessionID(user, tokenTTL)
	if err != nil {
		return "", err
	}
	encodedToken, err := handler.encodeAuthCookieToken(token)
	if err != nil {
		return "", err
	}

	cookie := &fiber.Cookie{
		Name:     authCookieName,
		Value:    encodedToken,
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
	}
	if rememberMe {
		cookie.Expires = time.Now().Add(tokenTTL)
	}
	c.Cookie(cookie)
	return sessionID, nil
}

func (handler *Handler) clearAuthCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     authCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}

func (handler *Handler) clearAuthRelatedCookies(c *fiber.Ctx) {
	handler.clearAuthCookie(c)
	handler.clearOIDCLogoutTransportCookies(c)
	handler.clearRecoveryCodePageCookie(c)
	handler.clearResetPasswordCookie(c)
}

func (handler *Handler) buildTokenWithSessionID(user *models.User, ttl time.Duration) (string, string, error) {
	if user == nil {
		return "", "", errors.New("user is required")
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

	logoutState, found, err := handler.oidcLogoutStateSvc.Load(oldSessionID, time.Now())
	if err != nil || !found {
		return err
	}
	if !validOIDCLogoutState(logoutState) {
		return handler.oidcLogoutStateSvc.Delete(oldSessionID)
	}
	if err := handler.oidcLogoutStateSvc.Save(newSessionID, logoutState, time.Now()); err != nil {
		return err
	}
	return handler.oidcLogoutStateSvc.Delete(oldSessionID)
}

func (handler *Handler) encodeAuthCookieToken(rawToken string) (string, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return "", errors.New("auth token is required")
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return "", err
	}
	return codec.seal(authCookieName, []byte(rawToken))
}

func (handler *Handler) decodeSealedAuthCookieToken(rawValue string) (string, error) {
	codec, err := newSecureCookieCodec(handler.secretKey)
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
