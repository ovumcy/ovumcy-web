package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) ShowDashboard(c *fiber.Ctx) error {
	user, handled, err := handler.currentUserOrRedirectToLogin(c)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	language, messages, now := handler.currentPageViewContext(c)
	location := handler.requestLocation(c)
	data, errorMessage, err := handler.buildDashboardViewData(user, language, messages, now, location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, errorMessage)
	}

	return handler.render(c, "dashboard", data)
}
