package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func apiError(c *fiber.Ctx, status int, message string) error {
	if responseFormat(c) == httpx.ResponseFormatHTMX {
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

func (handler *Handler) respondAuthError(c *fiber.Ctx, status int, message string) error {
	if (strings.HasPrefix(c.Path(), "/api/auth/") || strings.HasPrefix(c.Path(), "/auth/oidc")) && !acceptsJSON(c) && !isHTMX(c) {
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
			flash.ForgotEmail = services.NormalizeAuthEmail(c.FormValue("email"))
			handler.setFlashCookie(c, flash)
			return c.Redirect("/forgot-password", fiber.StatusSeeOther)
		case "/auth/oidc", "/auth/oidc/start", "/auth/oidc/callback":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/login", fiber.StatusSeeOther)
		case "/api/auth/reset-password":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/reset-password", fiber.StatusSeeOther)
		case "/api/auth/2fa":
			handler.setFlashCookie(c, flash)
			return c.Redirect("/auth/2fa", fiber.StatusSeeOther)
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
