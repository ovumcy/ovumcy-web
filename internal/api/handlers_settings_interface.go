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
	// Read BEFORE the cookie below is rewritten: the language this very page was
	// rendered in decides whether the save can be answered in place.
	languageChanged := language != currentLanguage(c)
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

	// Same language: nothing on the page changes, so the confirmation lands in
	// the Interface card's own status island and the page stays where it is.
	if isHTMX(c) && !languageChanged {
		return sendHTMLFragment(c, htmxSettingsSuccessMarkup(c, status, "Interface settings updated."))
	}

	// A new language re-renders the whole page, so a reload is genuinely needed.
	// It comes back to the card (the hash), where the flash is rendered in a
	// live region; the text itself travels in the sealed flash cookie only.
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	if isHTMX(c) && strings.HasSuffix(c.Get("HX-Current-URL"), interfaceCardFragment) {
		// Redirecting to the URL the browser is already on is a hash-only
		// navigation: no reload. A refresh keeps the hash and the new language.
		c.Set("HX-Refresh", "true")
		return c.SendStatus(fiber.StatusOK)
	}
	return redirectOrJSON(c, "/settings"+interfaceCardFragment)
}

// interfaceCardFragment is the fragment of the Interface settings card.
const interfaceCardFragment = "#settings-interface"
