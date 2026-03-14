package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ShowCalendar(c *fiber.Ctx) error {
	user, handled, err := handler.currentUserOrRedirectToLogin(c)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	language, messages, now := handler.currentPageViewContext(c)
	location := handler.requestLocation(c)
	minMonth := services.CalendarMinimumNavigableMonth(user, location)
	selectedDateQuery := strings.TrimSpace(c.Query("day"))
	if selectedDateQuery == "" {
		selectedDateQuery = strings.TrimSpace(c.Query("selected"))
	}
	activeMonth, selectedDate, err := services.ResolveCalendarMonthAndSelectedDateWithinBounds(c.Query("month"), selectedDateQuery, now, location, minMonth)
	if err != nil {
		if acceptsJSON(c) {
			return handler.respondMappedError(c, invalidMonthErrorSpec())
		}
		return redirectOrJSON(c, "/calendar")
	}

	data, err := handler.buildCalendarViewData(user, language, messages, now, activeMonth, selectedDate, location)
	if err != nil {
		return handler.respondMappedError(c, mapCalendarViewError(err))
	}
	data["SelectedDateEditMode"] = services.ParseBoolLike(c.Query("edit"))

	return handler.render(c, "calendar", data)
}
