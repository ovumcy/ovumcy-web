package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) ShowCalendar(c fiber.Ctx) error {
	user, handled, err := handler.currentUserOrRedirectToLogin(c)
	if err != nil {
		return err
	}
	if handled {
		return nil
	}

	language, messages, now := handler.currentPageViewContext(c)
	location := handler.requestLocation(c)
	bounds, err := handler.calendarViewService.NavigableMonthBounds(c.Context(), user, now, location)
	if err != nil {
		return handler.respondMappedError(c, mapCalendarViewError(err))
	}
	selectedDateQuery := strings.TrimSpace(c.Query("day"))
	if selectedDateQuery == "" {
		selectedDateQuery = strings.TrimSpace(c.Query("selected"))
	}
	monthInvalid := false
	activeMonth, selectedDate, err := services.ResolveCalendarMonthAndSelectedDateWithinBounds(c.Query("month"), selectedDateQuery, now, location, bounds.Min, bounds.Max)
	if err != nil {
		if acceptsJSON(c) {
			return handler.respondMappedError(c, invalidMonthErrorSpec())
		}
		// Answered on the page, as if ?month= were absent, so the notice can say
		// why and ?day= survives; an empty month query always resolves.
		monthInvalid = true
		activeMonth, selectedDate, _ = services.ResolveCalendarMonthAndSelectedDateWithinBounds("", selectedDateQuery, now, location, bounds.Min, bounds.Max)
	}

	data, err := handler.buildCalendarViewData(c.Context(), user, language, messages, now, activeMonth, selectedDate, bounds, location)
	if err != nil {
		return handler.respondMappedError(c, mapCalendarViewError(err))
	}
	data["SelectedDateEditMode"] = services.ParseBoolLike(c.Query("edit"))
	data["CalendarMonthInvalid"] = monthInvalid

	return handler.render(c, "calendar", data)
}
