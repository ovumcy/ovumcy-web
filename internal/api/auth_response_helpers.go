package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/httpx"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) respondAuthError(c *fiber.Ctx, status int, message string) error {
	if strings.HasPrefix(c.Path(), "/api/auth/") && !acceptsJSON(c) && !isHTMX(c) {
		flash := FlashPayload{AuthError: message}
		switch c.Path() {
		case "/api/auth/register":
			email := services.NormalizeAuthEmail(c.FormValue("email"))
			flash.RegisterEmail = email
			handler.setFlashCookie(c, flash)
			return c.Redirect("/register", fiber.StatusSeeOther)
		case "/api/auth/login":
			flash.LoginEmail = services.NormalizeAuthEmail(c.FormValue("email"))
			handler.setFlashCookie(c, flash)
			return c.Redirect("/login", fiber.StatusSeeOther)
		case "/api/auth/forgot-password":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/forgot-password", fiber.StatusSeeOther)
		case "/api/auth/reset-password":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/reset-password", fiber.StatusSeeOther)
		default:
			handler.setFlashCookie(c, flash)
			return c.Redirect("/login", fiber.StatusSeeOther)
		}
	}
	return apiError(c, status, message)
}

func (handler *Handler) respondSettingsError(c *fiber.Ctx, status int, message string) error {
	if isHTMX(c) {
		rendered := message
		if key := services.AuthErrorTranslationKey(message); key != "" {
			if localized := translateMessage(currentMessages(c), key); localized != key {
				rendered = localized
			}
		}
		return c.Status(fiber.StatusOK).SendString(httpx.StatusErrorMarkup(rendered))
	}
	if (strings.HasPrefix(c.Path(), "/api/settings/") || strings.HasPrefix(c.Path(), "/settings/")) && !acceptsJSON(c) {
		handler.setFlashCookie(c, FlashPayload{SettingsError: message})
		return c.Redirect("/settings", fiber.StatusSeeOther)
	}
	return apiError(c, status, message)
}
