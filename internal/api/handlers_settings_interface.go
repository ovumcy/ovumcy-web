package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) UpdateInterfaceSettings(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input := interfaceSettingsInput{}
	if err := c.Bind().Body(&input); err != nil {
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

	// The language is saved in BOTH stores: the account column is what a device
	// with no cookie is served on its next sign-in, the cookie is what this
	// browser renders from until then. `language` is already normalized against
	// the shipped locales above, which is the precondition the service states.
	// The theme has no account-side half — it stays client `localStorage`.
	if err := handler.settingsService.SaveInterfaceLanguage(c.Context(), user.ID, language); err != nil {
		return handler.respondMappedError(c, settingsInterfaceUpdateErrorSpec())
	}
	handler.setLanguageCookie(c, language)
	status := services.SettingsInterfaceUpdatedStatus

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
