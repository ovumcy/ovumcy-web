package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

const flashCookieTTL = 5 * time.Minute

func SetFlashCookie(c *fiber.Ctx, payload FlashPayload, secretKey []byte) {
	writeFlashCookie(c, payload, false, secretKey)
}

func SetFlashCookieWithSecure(c *fiber.Ctx, payload FlashPayload, secure bool, secretKey []byte) {
	writeFlashCookie(c, payload, secure, secretKey)
}

func (handler *Handler) setFlashCookie(c *fiber.Ctx, payload FlashPayload) {
	writeFlashCookie(c, payload, handler.cookieSecure, handler.secretKey)
}

func writeFlashCookie(c *fiber.Ctx, payload FlashPayload, secure bool, secretKey []byte) {
	payload = normalizeFlashPayload(payload)
	if flashPayloadEmpty(payload) {
		clearFlashCookie(c, secure)
		return
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return
	}
	codec, err := newSecureCookieCodec(secretKey)
	if err != nil {
		return
	}
	encoded, err := codec.seal(flashCookieName, serialized)
	if err != nil {
		return
	}

	c.Cookie(&fiber.Cookie{
		Name:     flashCookieName,
		Value:    encoded,
		Path:     "/",
		HTTPOnly: true,
		Secure:   secure,
		SameSite: "Lax",
		Expires:  time.Now().Add(flashCookieTTL),
	})
}

func (handler *Handler) popFlashCookie(c *fiber.Ctx) FlashPayload {
	raw := strings.TrimSpace(c.Cookies(flashCookieName))
	if raw == "" {
		return FlashPayload{}
	}
	clearFlashCookie(c, handler.cookieSecure)

	codec, err := newSecureCookieCodec(handler.secretKey)
	if err != nil {
		return FlashPayload{}
	}

	decoded, err := codec.open(flashCookieName, raw)
	if err != nil {
		return FlashPayload{}
	}

	payload := FlashPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return FlashPayload{}
	}
	return normalizeFlashPayload(payload)
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

func normalizeFlashPayload(payload FlashPayload) FlashPayload {
	payload.AuthError = strings.TrimSpace(payload.AuthError)
	payload.SettingsError = strings.TrimSpace(payload.SettingsError)
	payload.SettingsSuccess = strings.TrimSpace(payload.SettingsSuccess)
	payload.LoginEmail = services.NormalizeAuthEmail(payload.LoginEmail)
	payload.RegisterEmail = services.NormalizeAuthEmail(payload.RegisterEmail)
	payload.ForgotEmail = services.NormalizeAuthEmail(payload.ForgotEmail)
	return payload
}

func flashPayloadEmpty(payload FlashPayload) bool {
	return payload.AuthError == "" &&
		payload.SettingsError == "" &&
		payload.SettingsSuccess == "" &&
		payload.LoginEmail == "" &&
		payload.RegisterEmail == "" &&
		payload.ForgotEmail == ""
}
