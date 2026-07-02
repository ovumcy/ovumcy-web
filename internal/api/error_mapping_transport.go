package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// apiError renders an error response in the format matching the request.
// HTML/HTMX requests get a localized status fragment; JSON requests get the
// standard envelope with both the legacy top-level `error` string key and the
// richer `error_detail` object describing category and target. The top-level
// key stays for backward compatibility with clients that already parse it.
func apiError(c *fiber.Ctx, spec APIErrorSpec) error {
	if responseFormat(c) == httpx.ResponseFormatHTMX {
		rendered := spec.Key
		flashKey := spec.Key
		if key := services.AuthErrorTranslationKey(spec.Key); key != "" {
			flashKey = key
			if localized := translateMessage(currentMessages(c), key); localized != key {
				rendered = localized
			}
		} else if localized := translateMessage(currentMessages(c), spec.Key); localized != spec.Key {
			rendered = localized
		}
		return c.Status(spec.Status).SendString(httpx.StatusErrorMarkup(rendered, flashKey))
	}
	return c.Status(spec.Status).JSON(fiber.Map{
		"error": spec.Key,
		"error_detail": fiber.Map{
			"key":      spec.Key,
			"category": string(spec.Category),
			"target":   string(spec.Target),
		},
	})
}

// requestTooLargeErrorSpec maps a transport-level 413 (fiber's BodyLimit
// rejection) to the shared error-spec shape. The stable key "request_too_large"
// lets a JSON client (for example the settings restore flow, whose payload is
// the one large body the app accepts) resolve a localized message without the
// server ever echoing the rejected body. Kept as a global spec: the limit is
// enforced before any handler runs, so there is no form to scope it to.
func requestTooLargeErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusRequestEntityTooLarge, APIErrorCategoryTooLarge, "request_too_large")
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
func RespondRequestEntityTooLarge(c *fiber.Ctx) error {
	return apiError(c, requestTooLargeErrorSpec())
}

func (handler *Handler) respondAuthError(c *fiber.Ctx, spec APIErrorSpec) error {
	if (isV1AuthFormPath(c.Path()) || strings.HasPrefix(c.Path(), "/auth/oidc")) && !acceptsJSON(c) && !isHTMX(c) {
		flash := FlashPayload{AuthError: spec.Key}
		switch c.Path() {
		case "/api/v1/users":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/register", fiber.StatusSeeOther)
		case "/api/v1/sessions":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/login", fiber.StatusSeeOther)
		case "/api/v1/password-resets":
			flash.ForgotEmail = services.NormalizeAuthEmail(c.FormValue("email"))
			handler.setFlashCookie(c, flash)
			return c.Redirect("/forgot-password", fiber.StatusSeeOther)
		case "/auth/oidc", "/auth/oidc/start", "/auth/oidc/callback":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/login", fiber.StatusSeeOther)
		case "/api/v1/password-resets/redeem":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/reset-password", fiber.StatusSeeOther)
		case "/api/v1/sessions/2fa-challenge":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/auth/2fa", fiber.StatusSeeOther)
		default:
			handler.setFlashCookie(c, flash)
			return c.Redirect("/login", fiber.StatusSeeOther)
		}
	}
	return apiError(c, spec)
}

// isV1AuthFormPath enumerates the v1 auth endpoints that accept browser form
// submissions and therefore need flash-redirect handling on error. Listed
// explicitly rather than prefix-matched on /api/v1/ because the broader v1
// surface (days, symptoms, settings) returns JSON or HTMX status fragments
// and must NOT flash-redirect on error.
func isV1AuthFormPath(path string) bool {
	switch path {
	case "/api/v1/users", "/api/v1/sessions", "/api/v1/sessions/current",
		"/api/v1/sessions/2fa-challenge", "/api/v1/password-resets",
		"/api/v1/password-resets/redeem":
		return true
	}
	return false
}

func (handler *Handler) respondSettingsError(c *fiber.Ctx, spec APIErrorSpec) error {
	if isHTMX(c) {
		rendered := spec.Key
		flashKey := spec.Key
		if key := services.AuthErrorTranslationKey(spec.Key); key != "" {
			flashKey = key
			if localized := translateMessage(currentMessages(c), key); localized != key {
				rendered = localized
			}
		}
		return c.Status(fiber.StatusOK).SendString(httpx.StatusErrorMarkup(rendered, flashKey))
	}
	if strings.HasPrefix(c.Path(), "/api/v1/users/current") && !acceptsJSON(c) {
		handler.setFlashCookie(c, FlashPayload{SettingsError: spec.Key})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}
	return apiError(c, spec)
}
