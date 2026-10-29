package api

import (
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
	return httpx.NegotiateResponseFormat(c)
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
	title, translated := lookupMessage(messages, key)
	if !translated {
		return fallback
	}
	return title
}

// brandedPageTitle is localizedPageTitle for a page whose catalogue entry is
// the bare page name rather than a full "Ovumcy | …" title: the app name is
// prefixed the way every other page's title carries it.
func brandedPageTitle(messages map[string]string, key string, fallback string) string {
	title, translated := lookupMessage(messages, key)
	if !translated {
		return fallback
	}
	appName, named := lookupMessage(messages, "app.name")
	if !named {
		appName = "Ovumcy"
	}
	return appName + " | " + title
}
