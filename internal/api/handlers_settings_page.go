package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) ShowSettings(c *fiber.Ctx) error {
	user, handled, err := handler.currentUserOrRedirectToLogin(c)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	data, err := handler.buildSettingsPageData(c, user)
	if err != nil {
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}

	return handler.render(c, "settings", data)
}
