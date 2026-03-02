package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) GetDays(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	from, err := services.ParseDayDate(c.Query("from"), handler.location)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid from date")
	}
	to, err := services.ParseDayDate(c.Query("to"), handler.location)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid to date")
	}
	if to.Before(from) {
		return apiError(c, fiber.StatusBadRequest, "invalid range")
	}
	logs, err := handler.dayService.FetchLogsForUser(user.ID, from, to, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch logs")
	}

	services.SanitizeLogsForViewer(user, logs)

	return c.JSON(logs)
}

func (handler *Handler) GetDay(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	day, err := services.ParseDayDate(c.Params("date"), handler.location)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid date")
	}
	logEntry, err := handler.dayService.FetchLogByDate(user.ID, day, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch day")
	}

	logEntry = services.SanitizeLogForViewer(user, logEntry)

	return c.JSON(logEntry)
}

func (handler *Handler) CheckDayExists(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	day, err := services.ParseDayDate(c.Params("date"), handler.location)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid date")
	}
	exists, err := handler.dayService.DayHasDataForDate(user.ID, day, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch day")
	}

	return c.JSON(fiber.Map{"exists": exists})
}
