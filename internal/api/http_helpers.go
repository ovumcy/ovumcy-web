package api

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/gofiber/fiber/v2"
)

func redirectOrJSON(c *fiber.Ctx, path string) error {
	if isHTMX(c) {
		c.Set("HX-Redirect", path)
		return c.SendStatus(fiber.StatusOK)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect(path, fiber.StatusSeeOther)
}

func apiError(c *fiber.Ctx, status int, message string) error {
	if isHTMX(c) {
		rendered := message
		if key := authErrorTranslationKey(message); key != "" {
			if localized := translateMessage(currentMessages(c), key); localized != key {
				rendered = localized
			}
		}
		return c.Status(status).SendString(fmt.Sprintf("<div class=\"status-error\">%s</div>", template.HTMLEscapeString(rendered)))
	}
	return c.Status(status).JSON(fiber.Map{"error": message})
}

func acceptsJSON(c *fiber.Ctx) bool {
	return strings.Contains(strings.ToLower(c.Get("Accept")), "application/json")
}

func isHTMX(c *fiber.Ctx) bool {
	return strings.EqualFold(c.Get("HX-Request"), "true")
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
