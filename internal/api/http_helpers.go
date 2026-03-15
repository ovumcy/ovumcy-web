package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/httpx"
)

func redirectOrJSON(c *fiber.Ctx, path string) error {
	switch responseFormat(c) {
	case httpx.ResponseFormatHTMX:
		c.Set("HX-Redirect", path)
		return c.SendStatus(fiber.StatusOK)
	case httpx.ResponseFormatJSON:
		return c.JSON(fiber.Map{"ok": true})
	default:
		return c.Redirect(path, fiber.StatusSeeOther)
	}
}

func acceptsJSON(c *fiber.Ctx) bool {
	return responseFormat(c) == httpx.ResponseFormatJSON
}

func isHTMX(c *fiber.Ctx) bool {
	return responseFormat(c) == httpx.ResponseFormatHTMX
}

func hasJSONBody(c *fiber.Ctx) bool {
	return httpx.HasJSONContentType(c)
}

func responseFormat(c *fiber.Ctx) httpx.ResponseFormat {
	return httpx.NegotiateResponseFormat(c, httpx.JSONModeAcceptOrContentType)
}

func csrfToken(c *fiber.Ctx) string {
	token, _ := c.Locals("csrf").(string)
	return token
}

func localizedPageTitle(messages map[string]string, key string, fallback string) string {
	title := translateMessage(messages, key)
	if title == key || strings.TrimSpace(title) == "" {
		return fallback
	}
	return title
}
