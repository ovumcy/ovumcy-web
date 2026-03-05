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
	data, err := handler.buildStatsPageData(user, language, messages, now)
	if err != nil {
		return handler.respondMappedError(c, mapStatsPageViewError(err))
	}

	return handler.render(c, "stats", data)
}
