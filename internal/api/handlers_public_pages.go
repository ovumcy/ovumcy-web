package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) SetLanguage(c fiber.Ctx) error {
	languageInput := strings.TrimSpace(c.FormValue("lang"))
	if languageInput == "" {
		return fiber.ErrBadRequest
	}

	language := handler.i18n.NormalizeLanguage(languageInput)
	handler.setLanguageCookie(c, language)

	nextPath := services.SanitizeRedirectPath(c.FormValue("next"), "/")
	if isHTMX(c) {
		c.Set("HX-Redirect", nextPath)
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To(nextPath)
}

func (handler *Handler) ShowPrivacyPage(c fiber.Ctx) error {
	messages := currentMessages(c)
	authenticatedUser := handler.optionalAuthenticatedUser(c)
	data := buildPrivacyPageData(messages, c.Query("back"), authenticatedUser)
	return handler.render(c, "privacy", data)
}
