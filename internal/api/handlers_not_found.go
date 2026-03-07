package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) NotFound(c *fiber.Ctx) error {
	if strings.HasPrefix(c.Path(), "/api/") || acceptsJSON(c) || isHTMX(c) {
		return respondNotFoundMappedError(c)
	}

	currentUser := handler.optionalAuthenticatedUser(c)
	if currentUser != nil {
		c.Locals(contextUserKey, currentUser)
	}

	primaryPath := "/login"
	primaryLabelKey := "not_found.action_login"
	if currentUser != nil {
		primaryPath = "/dashboard"
		primaryLabelKey = "not_found.action_dashboard"
	}

	c.Status(fiber.StatusNotFound)
	return handler.render(c, "not_found", fiber.Map{
		"Title":           localizedPageTitle(currentMessages(c), "meta.title.not_found", "Ovumcy | Page Not Found"),
		"CurrentUser":     currentUser,
		"PrimaryPath":     primaryPath,
		"PrimaryLabelKey": primaryLabelKey,
	})
}
