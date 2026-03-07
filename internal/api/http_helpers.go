package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/httpx"
	"github.com/terraincognita07/ovumcy/internal/services"
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
		if key := services.AuthErrorTranslationKey(message); key != "" {
			if localized := translateMessage(currentMessages(c), key); localized != key {
				rendered = localized
			}
		} else if localized := translateMessage(currentMessages(c), message); localized != message {
			rendered = localized
		}
		return c.Status(status).SendString(httpx.StatusErrorMarkup(rendered))
	}
	return c.Status(status).JSON(fiber.Map{"error": message})
}

func acceptsJSON(c *fiber.Ctx) bool {
	return httpx.AcceptsJSON(c, httpx.JSONModeAcceptOnly)
}

func isHTMX(c *fiber.Ctx) bool {
	return httpx.IsHTMX(c)
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
