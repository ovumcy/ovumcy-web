package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

const resetPasswordCookieTTL = 30 * time.Minute

type resetPasswordCookiePayload struct {
	Token  string `json:"token"`
	Forced bool   `json:"forced,omitempty"`
}

func (handler *Handler) setResetPasswordCookie(c *fiber.Ctx, token string, forced bool) error {
	token = strings.TrimSpace(token)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		return errors.New("reset token is required")
	}

	payload := resetPasswordCookiePayload{
		Token:  token,
		Forced: forced,
	}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return err
	}
	encoded, err := codec.seal(resetPasswordCookieName, serialized)
	if err != nil {
		return err
	}

	c.Cookie(&fiber.Cookie{
		Name:     resetPasswordCookieName,
		Value:    encoded,
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(resetPasswordCookieTTL),
	})
	return nil
}

func (handler *Handler) readResetPasswordCookie(c *fiber.Ctx) (string, bool) {
	raw := strings.TrimSpace(c.Cookies(resetPasswordCookieName))
	if raw == "" {
		return "", false
	}

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		handler.clearResetPasswordCookie(c)
		return "", false
	}
	decoded, err := codec.open(resetPasswordCookieName, raw)
	if err != nil {
		handler.clearResetPasswordCookie(c)
		return "", false
	}

	payload := resetPasswordCookiePayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearResetPasswordCookie(c)
		return "", false
	}

	token := strings.TrimSpace(payload.Token)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		return "", false
	}
	return token, payload.Forced
}

func (handler *Handler) clearResetPasswordCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     resetPasswordCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}
