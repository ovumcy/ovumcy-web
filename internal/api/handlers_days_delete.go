package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) DeleteDailyLog(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, dayDeleteMutation, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Query("date"), location)
	if err != nil {
		return handler.failMutation(c, dayDeleteMutation, invalidDateErrorSpec())
	}
	if err := handler.dayService.DeleteDayEntry(c.UserContext(), user.ID, day, location); err != nil {
		return handler.failMutation(c, dayDeleteMutation, mapDayDeleteError(err))
	}

	handler.logMutationSuccess(c, dayDeleteMutation)

	source := strings.ToLower(strings.TrimSpace(c.Query("source")))
	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		switch source {
		case "calendar":
			return handler.renderDayEditorPartial(c, user, day)
		case "dashboard":
			c.Set("HX-Redirect", "/dashboard")
			return c.SendStatus(200)
		default:
			return c.SendStatus(204)
		}
	}

	if source == "dashboard" {
		return redirectOrJSON(c, "/dashboard")
	}
	return c.SendStatus(204)
}

var dayDeleteMutation = healthMutationKind{action: "health.day_delete", target: "day_entry"}

func (handler *Handler) DeleteDay(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, dayDeleteMutation, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.failMutation(c, dayDeleteMutation, invalidDateErrorSpec())
	}
	if err := handler.dayService.DeleteDayEntry(c.UserContext(), user.ID, day, location); err != nil {
		return handler.failMutation(c, dayDeleteMutation, mapDayDeleteError(err))
	}

	handler.logMutationSuccess(c, dayDeleteMutation)

	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		return handler.renderDayEditorPartial(c, user, day)
	}

	return c.SendStatus(204)
}
