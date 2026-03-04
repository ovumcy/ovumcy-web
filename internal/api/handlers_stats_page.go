package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) ShowStats(c *fiber.Ctx) error {
	user, handled, err := handler.currentUserOrRedirectToLogin(c)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	language, messages, now := handler.currentPageViewContext(c)
	data, errorMessage, err := handler.buildStatsPageData(user, language, messages, now)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, errorMessage)
	}

	return handler.render(c, "stats", data)
}
