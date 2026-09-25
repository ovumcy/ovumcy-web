package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// apiError renders an error response in the format matching the request.
// HTML/HTMX requests get a localized status fragment; JSON requests get the
// standard envelope with both the legacy top-level `error` string key and the
// richer `error_detail` object describing category and target. The top-level
// key stays for backward compatibility with clients that already parse it.
//
// A plain HTML navigation submitting POST /lang is the one route-level
// exception, taken here rather than per cause so every refusal on that route —
// handler, CSRF, recovered panic, request deadline — answers the same page.
func apiError(c fiber.Ctx, spec APIErrorSpec) error {
	if isLanguageSwitchPageNavigation(c) {
		return sendLanguageSwitchStatusFragment(c, spec)
	}
	if responseFormat(c) == httpx.ResponseFormatHTMX {
		return sendHTMLFragment(c.Status(spec.Status), localizedStatusErrorMarkup(c, spec))
	}
	return c.Status(spec.Status).JSON(apiErrorEnvelope(spec))
}

// apiErrorEnvelope is the JSON body every mapped rejection answers with. It is
// factored out so a response that carries an EXTENSION member — today only the
// rate limiters' retry_after_seconds — adds it to the same envelope rather than
// replacing it with a shape of its own.
func apiErrorEnvelope(spec APIErrorSpec) fiber.Map {
	return fiber.Map{
		"error": spec.Key,
		"error_detail": fiber.Map{
			"key":      spec.Key,
			"category": string(spec.Category),
			"target":   string(spec.Target),
		},
	}
}

// localizedStatusErrorMarkup renders one spec as the shared status-error
// fragment: the localized message plus the stable key next to it, so a test or
// a Playwright spec asserts the key and never the copy.
func localizedStatusErrorMarkup(c fiber.Ctx, spec APIErrorSpec) string {
	rendered := spec.Key
	flashKey := spec.Key
	if key := services.AuthErrorTranslationKey(spec.Key); key != "" {
		flashKey = key
		if localized, translated := lookupMessage(currentMessages(c), key); translated {
			rendered = localized
		}
	} else if localized, translated := lookupMessage(currentMessages(c), spec.Key); translated {
		rendered = localized
	}
	return httpx.StatusErrorMarkup(rendered, flashKey)
}

// respondPageFormStatusFragment answers one mapped spec as the shared localized
// status fragment, `text/html`. It is the arm for a client that is a browser
// performing a full-page form navigation rather than an API caller: the JSON
// envelope would be painted into the browser window as text. It answers through
// sendHTMLFragment, which labels the markup `text/html`; a bare SendString would
// leave it `text/plain` and the browser would show the tags. The catalogue is resolved
// here because a refusal can be produced before LanguageMiddleware has run (the
// edge limiters sit ahead of it), and without it the fragment renders its own
// machine key as the visible message.
//
// Status and stable key still come from the spec, so this changes the carrier
// and nothing about the contract.
func (handler *Handler) respondPageFormStatusFragment(c fiber.Ctx, spec APIErrorSpec) error {
	handler.ensureRequestMessages(c)
	return sendLanguageSwitchStatusFragment(c, spec)
}

// respondPageFormMappedError is the page-form counterpart of respondMappedError:
// a plain HTML navigation gets the status fragment above, every other
// negotiation keeps the envelope every mapped rejection answers with. Used by
// `POST /lang` — the one public form with no HTMX and no JavaScript behind it —
// whose limiter refusal already answers in this shape.
func (handler *Handler) respondPageFormMappedError(c fiber.Ctx, spec APIErrorSpec) error {
	if responseFormat(c) != httpx.ResponseFormatHTML {
		return handler.respondMappedError(c, spec)
	}
	return handler.respondPageFormStatusFragment(c, spec)
}

// transportErrorSpecsByStatus maps every HTTP status the app can answer as an
// explicit *fiber.Error onto the shared error-spec shape, keyed by status code
// alone. It exists because the envelope is an APP-WIDE contract: a client that
// learned to parse {error, error_detail} for one rejection must not meet the
// framework's bare English string on the next one, and the framework raises
// *fiber.Error for conditions no handler ever sees (an unroutable request, a
// body it cannot decode, a head it cannot parse). Mapping by status is what
// makes the coverage total — a status nothing in this repo raises today still
// answers in the app's own format the day something starts raising it.
//
// Two entries reuse a spec defined elsewhere on purpose, so one status keeps one
// key no matter which layer produced it: 401 shares unauthorizedErrorSpec with
// the auth guard, 404 shares notFoundErrorSpec with the route-level not-found,
// and 429 shares globalRateLimitErrorSpec with the limiters. The rest carry
// their own stable machine key, following the snake_case shape the pre-routing
// rejections established.
//
// The 503 entry is deliberately NOT the deadline guard's key: an expired request
// budget answers 503 "request_timeout" through RespondRequestTimeout, which is a
// statement about the caller's request, while a bare 503 raised as a fiber error
// is a statement about the server. Same status, different cause, different key —
// which is exactly what a machine key is for.
var transportErrorSpecsByStatus = map[int]APIErrorSpec{
	fiber.StatusBadRequest:                  globalErrorSpec(fiber.StatusBadRequest, APIErrorCategoryValidation, "bad_request"),
	fiber.StatusUnauthorized:                unauthorizedErrorSpec(),
	fiber.StatusForbidden:                   globalErrorSpec(fiber.StatusForbidden, APIErrorCategoryForbidden, "forbidden"),
	fiber.StatusNotFound:                    notFoundErrorSpec(),
	fiber.StatusMethodNotAllowed:            globalErrorSpec(fiber.StatusMethodNotAllowed, APIErrorCategoryValidation, "method_not_allowed"),
	fiber.StatusRequestEntityTooLarge:       globalErrorSpec(fiber.StatusRequestEntityTooLarge, APIErrorCategoryTooLarge, "request_too_large"),
	fiber.StatusUnsupportedMediaType:        globalErrorSpec(fiber.StatusUnsupportedMediaType, APIErrorCategoryValidation, "unsupported_media_type"),
	fiber.StatusTooManyRequests:             globalRateLimitErrorSpec(),
	fiber.StatusRequestHeaderFieldsTooLarge: globalErrorSpec(fiber.StatusRequestHeaderFieldsTooLarge, APIErrorCategoryTooLarge, "request_headers_too_large"),
	fiber.StatusInternalServerError:         globalErrorSpec(fiber.StatusInternalServerError, APIErrorCategoryInternal, "internal_error"),
	fiber.StatusServiceUnavailable:          globalErrorSpec(fiber.StatusServiceUnavailable, APIErrorCategoryInternal, "service_unavailable"),
}

// transportErrorSpecForStatus resolves the spec for one status. It is total by
// construction: an unlisted status falls back to its class rather than to the
// framework's text, so no future *fiber.Error can escape the envelope. Anything
// that is not a 4xx or 5xx is answered as 500 — an error handler reached with a
// success or redirect code is a defect in the caller, and honouring that status
// would ship a body claiming failure under a status claiming success.
func transportErrorSpecForStatus(status int) APIErrorSpec {
	if spec, ok := transportErrorSpecsByStatus[status]; ok {
		return spec
	}
	switch {
	case status >= 400 && status < 500:
		return globalErrorSpec(status, APIErrorCategoryValidation, "request_rejected")
	case status >= 500 && status < 600:
		return globalErrorSpec(status, APIErrorCategoryInternal, "internal_error")
	default:
		return transportErrorSpecsByStatus[fiber.StatusInternalServerError]
	}
}

// RespondTransportError answers one status through the same content-negotiated
// formatting as every mapped domain error. It is the single entry point the
// top-level Fiber ErrorHandler in cmd/ovumcy uses for every explicit
// *fiber.Error and for the generic 500 it substitutes for a raw error or a
// recovered panic.
//
// Only the STATUS crosses this boundary. The *fiber.Error's message never does:
// it is framework English at best and, for an error wrapped by a handler,
// arbitrary internal text at worst — table names, file paths, driver messages.
// The client gets the app's own stable key instead, which is both safer and more
// useful to parse.
func RespondTransportError(c fiber.Ctx, status int) error {
	return apiError(c, transportErrorSpecForStatus(status))
}

// requestTooLargeErrorSpec maps a transport-level 413 (fiber's BodyLimit
// rejection) to the shared error-spec shape. The stable key "request_too_large"
// lets a JSON client (for example the settings restore flow, whose payload is
// the one large body the app accepts) resolve a localized message without the
// server ever echoing the rejected body. Kept as a global spec: the limit is
// enforced before any handler runs, so there is no form to scope it to.
func requestTooLargeErrorSpec() APIErrorSpec {
	return transportErrorSpecForStatus(fiber.StatusRequestEntityTooLarge)
}

// RespondRequestEntityTooLarge renders the mapped 413 through the same
// content-negotiated formatting as every other mapped error: a stable JSON
// envelope for API/HTMX-JSON clients, a localized status fragment for HTMX.
// It is exported because fiber enforces BodyLimit in its core server error
// path (App.serverErrorHandler) on a fresh context before app middleware runs,
// so the top-level ErrorHandler in cmd/ovumcy must reach it directly rather
// than through a route handler. Localization is best-effort: on that early
// path request-scoped messages are absent, so the response falls back to the
// stable key, which is exactly what a machine client keys on.
func RespondRequestEntityTooLarge(c fiber.Ctx) error {
	return apiError(c, requestTooLargeErrorSpec())
}

// requestTimeoutErrorSpec maps a request that outlived its budget
// (RequestBudget, enforced by RequestDeadlineGuard) to the shared error-spec
// shape. 503 rather than 500: nothing about the request was wrong and the
// condition is transient, so the stable key "request_timeout" tells a client to
// retry rather than to report a fault. Global for the same reason as its 413
// sibling — the deadline expires somewhere below the handler, so there is no
// form field to scope it to.
func requestTimeoutErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusServiceUnavailable, APIErrorCategoryInternal, "request_timeout")
}

// RespondRequestTimeout renders that 503 through the same content-negotiated
// formatting as every other mapped error. Exported because the guard that
// detects the condition is middleware, registered in the composition root.
func RespondRequestTimeout(c fiber.Ctx) error {
	return apiError(c, requestTimeoutErrorSpec())
}

// requestHeadersTooLargeErrorSpec maps a transport-level 431 (the request head —
// start line plus every header, cookies included — not fitting fasthttp's read
// buffer) to the shared error-spec shape, for the same reason as the 413 above:
// the client should get a stable key rather than fiber's bare
// "Request Header Fields Too Large" string.
func requestHeadersTooLargeErrorSpec() APIErrorSpec {
	return transportErrorSpecForStatus(fiber.StatusRequestHeaderFieldsTooLarge)
}

// RespondRequestHeadersTooLarge renders the mapped 431 through the shared
// negotiation, and is exported for the same reason as its 413 sibling: fiber
// raises this from its core server error path on a context whose request head
// never parsed, so the top-level ErrorHandler in cmd/ovumcy must reach it
// directly. Nothing about the rejected request is echoed — with an unparseable
// head there is no method, path, or cookie value to leak even by accident.
func RespondRequestHeadersTooLarge(c fiber.Ctx) error {
	return apiError(c, requestHeadersTooLargeErrorSpec())
}

func (handler *Handler) respondAuthError(c fiber.Ctx, spec APIErrorSpec) error {
	path := httpx.RoutingNormalizedPath(c.Path())
	if (isV1AuthFormPath(path) || strings.HasPrefix(path, "/auth/oidc")) && !acceptsJSON(c) && !isHTMX(c) {
		flash := FlashPayload{AuthError: spec.Key}
		switch path {
		case "/api/v1/users":
			handler.setFlashCookie(c, flash)
			return c.Redirect().Status(fiber.StatusSeeOther).To("/register")
		case "/api/v1/sessions":
			handler.setFlashCookie(c, flash)
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		case "/api/v1/password-resets":
			flash.ForgotEmail = services.NormalizeAuthEmail(c.FormValue("email"))
			handler.setFlashCookie(c, flash)
			return c.Redirect().Status(fiber.StatusSeeOther).To("/forgot-password")
		case "/auth/oidc", "/auth/oidc/start", "/auth/oidc/callback":
			handler.setFlashCookie(c, flash)
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		case "/api/v1/password-resets/redeem":
			handler.setFlashCookie(c, flash)
			return c.Redirect().Status(fiber.StatusSeeOther).To("/reset-password")
		case "/api/v1/sessions/2fa-challenge":
			handler.setFlashCookie(c, flash)
			return c.Redirect().Status(fiber.StatusSeeOther).To("/auth/2fa")
		case "/api/v1/sessions/current":
			// No <form> can submit DELETE and there is no page to send the client
			// back to, so a plain client refused here by either limiter gets the
			// mapped-error envelope, never a redirect (WEB-72).
			return apiError(c, spec)
		// default is reachable: the SSO limiter is mounted on the whole /auth/oidc
		// prefix (not just start/callback), so a refusal on a sub-path with no case
		// of its own — the OIDC logout bridge, its redirect leg, link-confirm,
		// callback/continue — lands here too. Every one of those is a page-flow
		// continuation, so the same /login redirect it already gets for the listed
		// cases is the right fallback, not a gap.
		default:
			handler.setFlashCookie(c, flash)
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		}
	}
	return apiError(c, spec)
}

// isV1AuthFormPath enumerates the v1 auth endpoints that accept browser form
// submissions and therefore need flash-redirect handling on error. Listed
// explicitly rather than prefix-matched on /api/v1/ because the broader v1
// surface (days, symptoms, settings) returns JSON or HTMX status fragments
// and must NOT flash-redirect on error.
//
// DELETE /api/v1/sessions/current stays a member although no <form> can submit
// DELETE: this predicate also selects RespondAPIRateLimited's spec for the
// app-wide /api limiter, whose auth_form target JSON and HTMX clients already
// see. respondAuthError refuses the redirect for it with its own case instead.
func isV1AuthFormPath(path string) bool {
	switch path {
	case "/api/v1/users", "/api/v1/sessions", "/api/v1/sessions/current",
		"/api/v1/sessions/2fa-challenge", "/api/v1/password-resets",
		"/api/v1/password-resets/redeem":
		return true
	}
	return false
}

func (handler *Handler) respondSettingsError(c fiber.Ctx, spec APIErrorSpec) error {
	if isHTMX(c) {
		rendered := spec.Key
		flashKey := spec.Key
		if key := services.AuthErrorTranslationKey(spec.Key); key != "" {
			flashKey = key
			if localized, translated := lookupMessage(currentMessages(c), key); translated {
				rendered = localized
			}
		}
		return sendHTMLFragment(c.Status(fiber.StatusOK), httpx.StatusErrorMarkup(rendered, flashKey))
	}
	if strings.HasPrefix(httpx.RoutingNormalizedPath(c.Path()), "/api/v1/users/current") && !acceptsJSON(c) {
		handler.setFlashCookie(c, FlashPayload{SettingsError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
	return apiError(c, spec)
}

// redirectSettingsRefusal flashes a refusal the SETTINGS page can render and
// sends the browser back to it. Every OIDC step-up that authorizes a settings
// action completes on /auth/oidc/callback and finishes by redirecting to
// /settings, so its refusals cannot answer through respondMappedError above —
// they have to ride a flash across the redirect.
//
// The channel is not interchangeable. buildSettingsViewData feeds the settings
// view service only flash.SettingsSuccess and flash.SettingsError; flash.AuthError
// is read exclusively by the auth pages (auth_page_view_helpers.go), which a
// redirect to /settings never reaches. A refusal flashed as AuthError is
// therefore consumed by nothing: the owner lands on an unchanged settings page
// and cannot tell a refused step-up from a successful one, while the success
// arm's SettingsSuccess renders. Regression:
// TestSettingsStepupRefusalsRenderOnTheSettingsPage.
//
// The key must also be one services.AuthErrorTranslationKey maps (that is the
// lookup resolveSettingsStatusKeys performs on the flashed error), or the banner
// renders empty for the same reason. Regression:
// TestEverySettingsStepupRefusalKeyMapsToLocalizedCopy.
func (handler *Handler) redirectSettingsRefusal(c fiber.Ctx, spec APIErrorSpec) error {
	handler.setFlashCookie(c, FlashPayload{SettingsError: spec.Key})
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}

// redirectSignedOutRefusal answers a refusal raised after this device's auth
// cookie was already cleared: a revocation raced the change, or the change
// committed and no session could be re-issued past it. Neither
// redirectSettingsRefusal nor respondSettingsError's plain-HTML arm can carry
// it — /settings needs the cookie that is gone, so AuthRequired bounces the
// browser to /login and the SettingsError flash is read by nothing there. The
// refusal rides flash.AuthError, the channel the sign-in page reads, and the
// browser is sent to /login directly (HX-Redirect for HTMX, whose swapped
// fragment would sit on a page that no longer has a session). Like
// redirectSettingsRefusal it ignores Accept: a step-up callback is a navigation
// back from the provider, and a page is the only answer it can show.
func (handler *Handler) redirectSignedOutRefusal(c fiber.Ctx, spec APIErrorSpec) error {
	handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
	if isHTMX(c) {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
}

// respondSignedOutRefusal is redirectSignedOutRefusal for a route that also
// serves JSON clients: they have no page to land on and keep the mapped status
// and key.
func (handler *Handler) respondSignedOutRefusal(c fiber.Ctx, spec APIErrorSpec) error {
	if acceptsJSON(c) {
		return handler.respondMappedError(c, spec)
	}
	return handler.redirectSignedOutRefusal(c, spec)
}
