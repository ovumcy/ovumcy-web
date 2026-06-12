package api

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const flashCookieTTL = 5 * time.Minute

var flashCookieSpec = sealedCookieSpec{name: flashCookieName, path: "/"}

func (handler *Handler) setFlashCookie(c *fiber.Ctx, payload FlashPayload) {
	payload = normalizeFlashPayload(payload)
	if flashPayloadEmpty(payload) {
		handler.clearFlashCookie(c)
		return
	}

	serialized, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_ = handler.writeSealedCookie(c, flashCookieSpec, serialized, time.Now().Add(flashCookieTTL))
}

func (handler *Handler) popFlashCookie(c *fiber.Ctx) FlashPayload {
	raw := strings.TrimSpace(c.Cookies(flashCookieName))
	if raw == "" {
		return FlashPayload{}
	}
	handler.clearFlashCookie(c)

	codec, err := handler.cookieCodec()
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

func (handler *Handler) clearFlashCookie(c *fiber.Ctx) {
	handler.clearSealedCookie(c, flashCookieSpec)
}

func normalizeFlashPayload(payload FlashPayload) FlashPayload {
	payload.AuthError = strings.TrimSpace(payload.AuthError)
	payload.SettingsError = strings.TrimSpace(payload.SettingsError)
	payload.SettingsSuccess = strings.TrimSpace(payload.SettingsSuccess)
	payload.ForgotEmail = services.NormalizeAuthEmail(payload.ForgotEmail)
	return payload
}

func flashPayloadEmpty(payload FlashPayload) bool {
	return payload.AuthError == "" &&
		payload.SettingsError == "" &&
		payload.SettingsSuccess == "" &&
		payload.ForgotEmail == ""
}
