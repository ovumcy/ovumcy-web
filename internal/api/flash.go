package api

import (
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const flashCookieTTL = 5 * time.Minute

var flashCookieSpec = sealedCookieSpec{name: flashCookieName, path: "/"}

func (handler *Handler) setFlashCookie(c fiber.Ctx, payload FlashPayload) {
	payload = normalizeFlashPayload(payload)
	if flashPayloadEmpty(payload) {
		handler.clearFlashCookie(c)
		return
	}

	// Both failures below end the same way for the user — a redirect with no
	// explanation of the error that caused it — so the flash cannot be
	// propagated to the caller: every one of its ~60 call sites is already on
	// an error path whose response is decided. What it can do is stop being
	// silent. One operational diagnostic per failure, naming the carrier and
	// the reason and never the payload (a flash may carry the submitted email),
	// is the difference between "no flash was warranted" and "the error carrier
	// is broken". Regression: TestFlashCookieWriteFailureIsReported.
	serialized, err := json.Marshal(payload)
	if err != nil {
		log.Printf("flash cookie: encode failed: %s", SafeLogError(err)) // codecov:ignore -- defensive: a struct of strings has no failing marshal
		return
	}
	if err := handler.writeSealedCookie(c, flashCookieSpec, serialized, time.Now().Add(flashCookieTTL)); err != nil {
		log.Printf("flash cookie: sealed write failed: %s", SafeLogError(err))
	}
}

func (handler *Handler) popFlashCookie(c fiber.Ctx) FlashPayload {
	if c.Method() == fiber.MethodHead {
		// The cookie is single-use and a HEAD response always drops the body
		// that would carry it (the HEAD twin registerHEADTwins registers runs
		// this same chain) — so popping it here would spend the one flash
		// write, ForgotEmail prefill included, on a visit that could never
		// display it. Leave it sealed for the GET that can.
		return FlashPayload{}
	}
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

func (handler *Handler) clearFlashCookie(c fiber.Ctx) {
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
