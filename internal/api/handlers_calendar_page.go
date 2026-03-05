package api

import (
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
	activeMonth, selectedDate, err := services.ResolveCalendarMonthAndSelectedDate(c.Query("month"), c.Query("day"), now, location)
	if err != nil {
		return handler.respondMappedError(c, invalidMonthErrorSpec())
	}

	data, err := handler.buildCalendarViewData(user, language, messages, now, activeMonth, selectedDate, location)
	if err != nil {
		return handler.respondMappedError(c, mapCalendarViewError(err))
	}

	return handler.render(c, "calendar", data)
}
