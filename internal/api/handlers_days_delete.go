package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var dayDeleteMutation = healthMutationKind{action: "health.day_delete", target: "day_entry"}

// DeleteDay handles DELETE /api/v1/days/:date. The optional "source" query
// param ("calendar" or "dashboard") selects which HTMX response shape the
// browser UI expects; programmatic clients can omit it and receive 204.
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

	source := strings.ToLower(strings.TrimSpace(c.Query("source")))
	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		switch source {
		case "dashboard":
			c.Set("HX-Redirect", "/dashboard")
			return c.SendStatus(200)
		default:
			return handler.renderDayEditorPartial(c, user, day)
		}
	}

	if source == "dashboard" {
		return redirectOrJSON(c, "/dashboard")
	}
	return c.SendStatus(204)
}
