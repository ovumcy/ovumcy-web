package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/httpx"
)

func (handler *Handler) NotFound(c *fiber.Ctx) error {
	if strings.HasPrefix(c.Path(), "/api/") || acceptsJSON(c) {
		return respondGlobalMappedError(c, notFoundErrorSpec())
	}

	if isHTMX(c) {
		message := translateMessage(currentMessages(c), "not_found.title")
		if message == "not_found.title" {
			message = "Page not found"
		}
		c.Status(fiber.StatusNotFound)
		return c.SendString(httpx.StatusErrorMarkup(message))
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
