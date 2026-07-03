package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/csrf"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
)

func redirectOrJSON(c fiber.Ctx, path string) error {
	switch responseFormat(c) {
	case httpx.ResponseFormatHTMX:
		c.Set("HX-Redirect", path)
		return c.SendStatus(fiber.StatusOK)
	case httpx.ResponseFormatJSON:
		return c.JSON(fiber.Map{"ok": true})
	default:
		return c.Redirect().Status(fiber.StatusSeeOther).To(path)
	}
}

func acceptsJSON(c fiber.Ctx) bool {
	return responseFormat(c) == httpx.ResponseFormatJSON
}

func isHTMX(c fiber.Ctx) bool {
	return responseFormat(c) == httpx.ResponseFormatHTMX
}

func hasJSONBody(c fiber.Ctx) bool {
	return httpx.HasJSONContentType(c)
}

func responseFormat(c fiber.Ctx) httpx.ResponseFormat {
	return httpx.NegotiateResponseFormat(c, httpx.JSONModeAcceptOrContentType)
}

// csrfToken returns the per-request CSRF token the middleware stored in the
// request context, for embedding in rendered forms. Fiber v3 removed the
// string "csrf" Locals key (v2's ContextKey), so the token is read via the
// middleware's typed accessor; it returns "" when the middleware did not run
// for this request.
func csrfToken(c fiber.Ctx) string {
	return csrf.TokenFromContext(c)
}

func localizedPageTitle(messages map[string]string, key string, fallback string) string {
	title := translateMessage(messages, key)
	if title == key || strings.TrimSpace(title) == "" {
		return fallback
	}
	return title
}
