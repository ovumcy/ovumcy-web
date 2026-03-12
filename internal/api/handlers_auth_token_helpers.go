package api

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func (handler *Handler) setAuthCookie(c *fiber.Ctx, user *models.User, rememberMe bool) error {
	tokenTTL := defaultAuthTokenTTL
	if rememberMe {
		tokenTTL = rememberAuthTokenTTL
	}

	token, err := handler.buildToken(user, tokenTTL)
	if err != nil {
		return err
	}
	encodedToken, err := handler.encodeAuthCookieToken(token)
	if err != nil {
		return err
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
	return nil
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

func (handler *Handler) buildToken(user *models.User, ttl time.Duration) (string, error) {
	if user == nil {
		return "", errors.New("user is required")
	}
	if ttl <= 0 {
		ttl = defaultAuthTokenTTL
	}
	return handler.authService.BuildAuthSessionToken(handler.secretKey, user.ID, user.Role, user.AuthSessionVersion, ttl, time.Now())
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
