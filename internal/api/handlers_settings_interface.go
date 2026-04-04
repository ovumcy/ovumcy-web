package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) UpdateInterfaceSettings(c *fiber.Ctx) error {
	if _, ok := currentUser(c); !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input := interfaceSettingsInput{}
	if err := c.BodyParser(&input); err != nil {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	language := handler.i18n.NormalizeLanguage(input.Language)
	if strings.TrimSpace(input.Language) == "" {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	theme := services.NormalizeInterfaceTheme(input.Theme)
	if theme == "" {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	handler.setLanguageCookie(c, language)
	status := handler.settingsService.ResolveInterfaceUpdateStatus()

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":       true,
			"status":   status,
			"language": language,
			"theme":    theme,
		})
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}
