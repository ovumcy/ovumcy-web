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

	data, errorMessage, err := handler.buildSettingsPageData(c, user)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, errorMessage)
	}

	return handler.render(c, "settings", data)
}
