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

	if strings.TrimSpace(input.Language) == "" {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}
	// A language this build does not ship is REFUSED here rather than folded
	// into the default. Normalizing it was harmless while the choice lived only
	// in a cookie the next explicit switch overwrote; now the same value is
	// stored on the account and re-issued at every sign-in, so a typo or a
	// hand-made request would make a language the owner never picked stick to
	// every device they own. The submitted value is bounded by the form's own
	// options, so refusing it costs nothing an owner can reach.
	if !handler.i18n.IsSupportedLanguage(input.Language) {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}
	language := handler.i18n.NormalizeLanguage(input.Language)

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
