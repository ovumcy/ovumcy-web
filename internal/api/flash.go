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
// oidc_stepup_continuation.go), the requireFirstPartyRequest refusals that
// themselves fire on the cross-site request the guard exists to name
// (refuseOIDCStepupContinueRequest, refuseRegisterPickupRequest), and the
// /auth/oidc rate limiter (RespondAuthRateLimited → respondAuthError,
// error_mapping_transport.go): that limiter is mounted on the whole prefix
// with no method filter, so a cross-site top-level GET navigation can trip it
// too — seal into THIS cookie via setCSRFExemptFlashCookie, never
// flashCookieSpec's. Because it is a separate cookie, a token-less writer
// structurally cannot overwrite or erase flashCookieName's value: it never
// sends a Set-Cookie for that name at all. See popFlashCookie for the
// read-side precedence between the two.
//
// A THIRD group cannot be sorted at compile time: requireFirstPartyRequest is
// deliberately monotone (see firstPartyRequestRefusal) and lets through a
// stated "same-site" origin and a missing Fetch Metadata family, neither of
// which is evidence of a same-origin request. ContinueOIDCStepup's
// no-continuation arm, refuseOIDCStepupCallback's same-site arm, and
// PickupRegister's redirectToPostRegisterSignin all defer their slot choice
// to setFlashCookieForRequestOrigin, which picks the page slot only for a
// STATED "same-origin" Sec-Fetch-Site (WEB-40 round 3).
//
// Every OTHER setFlashCookie call site sits behind session + CSRF (the
// authenticated settings routes) or a step-up whose dispatch already matched
// the sealed state secret (the per-purpose completions the direct same-site
// callback shares with ContinueOIDCStepup's successful leg) — none of those is
// reachable by a request carrying no token from another site, so they keep
// using setFlashCookie/flashCookieSpec unchanged.
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

// setFlashCookieForRequestOrigin is the slot decision for a site that is
// reached by BOTH a same-origin navigation and one requireFirstPartyRequest's
// Fetch-Metadata check lets through without proving same-origin (WEB-40
// round 3): a stated "same-site" origin, a stated "none", or the family
// missing entirely all satisfy that guard (it is deliberately monotone — see
// firstPartyRequestRefusal) but none of them is evidence the request is the
// app's own document, the same standard callbackArrivedCrossSite already
// applies for the OIDC bounce. So the page slot is reserved for the one value
// that IS such evidence — a STATED "same-origin" — and every other case,
// including silence, goes to the exempt slot. This mirrors
// callbackArrivedCrossSite's monotone reasoning rather than negating it: that
// helper still decides "is this cross-site" for the bounce; this one decides
// "is this provably same-origin" for the flash slot, and the two disagree on
// purpose for "same-site" and "missing", which the bounce runs through
// unchanged (state-secret gated) but the flash slot must not.
func (handler *Handler) setFlashCookieForRequestOrigin(c fiber.Ctx, payload FlashPayload) {
	if strings.EqualFold(strings.TrimSpace(c.Get(headerSecFetchSite)), secFetchSiteSameOrigin) {
		handler.setFlashCookie(c, payload)
		return
	}
	handler.setCSRFExemptFlashCookie(c, payload)
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
// cleared (its existing single-use contract). When the page slot wins, the
// exempt slot is retracted too rather than left standing: a stale
// token-less message left riding would otherwise surface on a later,
// unrelated render, once the page slot that outranked it has been consumed
// and is no longer there to keep outranking it. The exempt slot is read
// (and, on its own, cleared) only when the page slot was EMPTY, which is the
// ordinary case — a genuine provider refusal with nothing else pending.
//
// Known accepted gap (F4, round 3): "wins" is decided on flashPayloadEmpty,
// not on whether the rendering page actually shows one of the page slot's
// fields. buildSettingsViewData reads only SettingsError/SettingsSuccess and
// the auth pages read only AuthError/ForgotEmail (see redirectSettingsRefusal
// and respondAuthError), so a page slot minted for one page kind (e.g. an
// AuthError meant for /login) that a request instead pops from the OTHER page
// kind (/settings) is popped, discarded unrendered, AND still outranks and
// clears a genuinely pending exempt message — losing both. This needs two
// flashes racing across tabs/page-kinds within the 5-minute TTL, is not
// reachable by a token-less writer (every page-slot writer is either
// session+CSRF-gated or state-secret/origin-gated after F1/F2 above), and is
// left as a documented gap rather than threaded through with a per-page field
// predicate no other call site needs.
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
		handler.clearCSRFExemptFlashCookie(c)
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
