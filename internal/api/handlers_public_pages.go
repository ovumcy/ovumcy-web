package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) SetupStatus(c *fiber.Ctx) error {
	needsSetup, err := handler.requiresInitialSetup()
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to load setup state")
	}
	return c.JSON(fiber.Map{"needs_setup": needsSetup})
}

func (handler *Handler) SetLanguage(c *fiber.Ctx) error {
	language := handler.i18n.NormalizeLanguage(c.Params("lang"))
	handler.setLanguageCookie(c, language)

	nextPath := services.SanitizeRedirectPath(c.Query("next"), "/")
	if isHTMX(c) {
		c.Set("HX-Redirect", nextPath)
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect(nextPath, fiber.StatusSeeOther)
}

func (handler *Handler) ShowPrivacyPage(c *fiber.Ctx) error {
	messages := currentMessages(c)
	authenticatedUser := handler.optionalAuthenticatedUser(c)
	data := buildPrivacyPageData(messages, c.Query("back"), authenticatedUser)
	return handler.render(c, "privacy", data)
}
