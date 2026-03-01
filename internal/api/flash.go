package api

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func SetFlashCookie(c *fiber.Ctx, payload FlashPayload) {
	writeFlashCookie(c, payload, false)
}

func SetFlashCookieWithSecure(c *fiber.Ctx, payload FlashPayload, secure bool) {
	writeFlashCookie(c, payload, secure)
}

func (handler *Handler) setFlashCookie(c *fiber.Ctx, payload FlashPayload) {
	writeFlashCookie(c, payload, handler.cookieSecure)
}

func writeFlashCookie(c *fiber.Ctx, payload FlashPayload, secure bool) {
	payload.AuthError = strings.TrimSpace(payload.AuthError)
	payload.SettingsError = strings.TrimSpace(payload.SettingsError)
	payload.SettingsSuccess = strings.TrimSpace(payload.SettingsSuccess)
	payload.LoginEmail = services.NormalizeAuthEmail(payload.LoginEmail)
	payload.RegisterEmail = services.NormalizeAuthEmail(payload.RegisterEmail)

	if payload.AuthError == "" &&
		payload.SettingsError == "" &&
		payload.SettingsSuccess == "" &&
		payload.LoginEmail == "" &&
		payload.RegisterEmail == "" {
		clearFlashCookie(c, secure)
		return
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return
	}
	encoded := base64.RawURLEncoding.EncodeToString(serialized)

	c.Cookie(&fiber.Cookie{
		Name:     flashCookieName,
		Value:    encoded,
		Path:     "/",
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		Expires:  time.Now().Add(5 * time.Minute),
	})
}

func (handler *Handler) popFlashCookie(c *fiber.Ctx) FlashPayload {
	raw := strings.TrimSpace(c.Cookies(flashCookieName))
	if raw == "" {
		return FlashPayload{}
	}
	clearFlashCookie(c, handler.cookieSecure)

	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return FlashPayload{}
	}

	payload := FlashPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return FlashPayload{}
	}
	payload.AuthError = strings.TrimSpace(payload.AuthError)
	payload.SettingsError = strings.TrimSpace(payload.SettingsError)
	payload.SettingsSuccess = strings.TrimSpace(payload.SettingsSuccess)
	payload.LoginEmail = services.NormalizeAuthEmail(payload.LoginEmail)
	payload.RegisterEmail = services.NormalizeAuthEmail(payload.RegisterEmail)
	return payload
}

func clearFlashCookie(c *fiber.Ctx, secure bool) {
	c.Cookie(&fiber.Cookie{
		Name:     flashCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}
