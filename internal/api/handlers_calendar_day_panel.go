package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) CalendarDayPanel(c *fiber.Ctx) error {
	user, handled, err := currentUserOrUnauthorized(c)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.respondMappedError(c, invalidDateErrorSpec())
	}

	return handler.renderDayEditorPartial(c, user, day)
}

func (handler *Handler) renderDayEditorPartial(c *fiber.Ctx, user *models.User, day time.Time) error {
	language, messages, now := handler.currentPageViewContext(c)
	location := handler.requestLocation(c)
	payload, err := handler.buildDayEditorPartialData(user, language, messages, day, now, location)
	if err != nil {
		return handler.respondMappedError(c, mapDayEditorViewError(err))
	}
	return handler.renderPartial(c, "day_editor_partial", payload)
}
