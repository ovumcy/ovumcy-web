package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) GetDays(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	from, to, err := services.ParseDayRange(c.Query("from"), c.Query("to"), location)
	if err != nil {
		return handler.respondMappedError(c, mapDayRangeError(err))
	}
	logs, err := handler.viewerService.FetchLogsForViewer(user, from, to, location)
	if err != nil {
		return handler.respondMappedError(c, dayLogsFetchErrorSpec())
	}

	return c.JSON(logs)
}

func (handler *Handler) GetDay(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.respondMappedError(c, invalidDateErrorSpec())
	}
	logEntry, err := handler.viewerService.FetchLogByDateForViewer(user, day, location)
	if err != nil {
		return handler.respondMappedError(c, dayFetchErrorSpec())
	}

	return c.JSON(logEntry)
}

func (handler *Handler) CheckDayExists(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.respondMappedError(c, invalidDateErrorSpec())
	}
	exists, err := handler.dayService.DayHasDataForDate(user.ID, day, location)
	if err != nil {
		return handler.respondMappedError(c, dayFetchErrorSpec())
	}

	return c.JSON(fiber.Map{"exists": exists})
}
