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

// exemptFlashCookieSpec is the second flash channel (WEB-40): a write reached
// through a request the CSRF middleware never validates — the OIDC callback's
// sole exemption and its unguarded query-mode GET twin (handlers_auth_oidc.go,
// StartOIDCLogin/CompleteOIDCLogin), the step-up cross-site refusal that runs
// on that same request (refuseOIDCStepupCallback's cross-site arm,
// oidc_stepup_continuation.go), and the requireFirstPartyRequest refusals that
// themselves fire on the cross-site request the guard exists to name
// (refuseOIDCStepupContinueRequest, refuseRegisterPickupRequest) — seals into
// THIS cookie via setCSRFExemptFlashCookie, never flashCookieSpec's. Because
// it is a separate cookie, a token-less writer structurally cannot overwrite
// or erase flashCookieName's value: it never sends a Set-Cookie for that name
// at all. See popFlashCookie for the read-side precedence between the two.
//
// Every OTHER setFlashCookie call site sits behind either session + CSRF (the
// authenticated settings routes) or requireFirstPartyRequest already having
// passed (ContinueOIDCStepup and the per-purpose step-up completions it
// shares with the direct same-site callback, and PickupRegister's own body) —
// none of those is reachable by a request carrying no token from another
// site, so they keep using setFlashCookie/flashCookieSpec unchanged.
var exemptFlashCookieSpec = sealedCookieSpec{name: exemptFlashCookieName, path: "/"}

func (handler *Handler) setFlashCookie(c fiber.Ctx, payload FlashPayload) {
	handler.writeFlashCookie(c, flashCookieSpec, handler.clearFlashCookie, payload)
}

// setCSRFExemptFlashCookie is setFlashCookie's twin for a write reachable
// through a CSRF-exempt/token-less request. See exemptFlashCookieSpec for
// exactly which call sites belong here and why.
func (handler *Handler) setCSRFExemptFlashCookie(c fiber.Ctx, payload FlashPayload) {
	handler.writeFlashCookie(c, exemptFlashCookieSpec, handler.clearCSRFExemptFlashCookie, payload)
}

func (handler *Handler) writeFlashCookie(c fiber.Ctx, spec sealedCookieSpec, clear func(fiber.Ctx), payload FlashPayload) {
	payload = normalizeFlashPayload(payload)
	if flashPayloadEmpty(payload) {
		clear(c)
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
	expiresAt := time.Now().Add(flashCookieTTL)
	payload.ExpiresAt = expiresAt
	serialized, err := json.Marshal(payload)
	if err != nil {
		log.Printf("flash cookie: encode failed: %s", SafeLogError(err)) // codecov:ignore -- defensive: a struct of strings has no failing marshal
		return
	}
	if err := handler.writeSealedCookie(c, spec, serialized, expiresAt); err != nil {
		log.Printf("flash cookie: sealed write failed: %s", SafeLogError(err))
	}
}

// popFlashCookie applies the precedence the WEB-40 decision names: the page
// slot (flashCookieName) wins whenever it carries anything — it is the
// trusted, same-origin channel every ordinary page redirect writes, and a
// token-less writer must never outrank it. The page slot is always read and
// cleared (its existing single-use contract). The exempt slot is read and
// cleared only when the page slot was EMPTY, which is the ordinary case (a
// genuine provider refusal with nothing else pending); the rare coincidence
// of both being pending in the same response leaves the exempt cookie
// standing rather than destroying its message — it surfaces on the owner's
// next navigation instead of on this one, never lost outright.
func (handler *Handler) popFlashCookie(c fiber.Ctx) FlashPayload {
	if c.Method() == fiber.MethodHead {
		// The cookie is single-use and a HEAD response always drops the body
		// that would carry it (the HEAD twin registerHEADTwins registers runs
		// this same chain) — so popping it here would spend the one flash
		// write, ForgotEmail prefill included, on a visit that could never
		// display it. Leave it sealed for the GET that can.
		return FlashPayload{}
	}
	page := handler.popFlashCookieBySpec(c, flashCookieName, flashCookieSpec)
	if !flashPayloadEmpty(page) {
		return page
	}
	return handler.popFlashCookieBySpec(c, exemptFlashCookieName, exemptFlashCookieSpec)
}

func (handler *Handler) popFlashCookieBySpec(c fiber.Ctx, cookieName string, spec sealedCookieSpec) FlashPayload {
	raw := strings.TrimSpace(c.Cookies(cookieName))
	if raw == "" {
		return FlashPayload{}
	}
	handler.clearSealedCookie(c, spec)

	codec, err := handler.cookieCodec()
	if err != nil {
		return FlashPayload{}
	}

	decoded, err := codec.open(cookieName, raw)
	if err != nil {
		return FlashPayload{}
	}

	payload := FlashPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return FlashPayload{}
	}
	// The bound is the server's, not the browser's: a payload minted without
	// one (a pre-upgrade value) or past it is refused, so a kept sealed value
	// cannot replay its message or its ForgotEmail prefill.
	if payload.ExpiresAt.IsZero() || time.Now().After(payload.ExpiresAt) {
		return FlashPayload{}
	}
	return normalizeFlashPayload(payload)
}

func (handler *Handler) clearFlashCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, flashCookieSpec)
}

func (handler *Handler) clearCSRFExemptFlashCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, exemptFlashCookieSpec)
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
