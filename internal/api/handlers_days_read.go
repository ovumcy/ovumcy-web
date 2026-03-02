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

	from, to, err := services.ParseDayRange(c.Query("from"), c.Query("to"), handler.location)
	if err != nil {
		switch services.ClassifyDayRangeError(err) {
		case services.DayRangeErrorFromInvalid:
			return apiError(c, fiber.StatusBadRequest, "invalid from date")
		case services.DayRangeErrorToInvalid:
			return apiError(c, fiber.StatusBadRequest, "invalid to date")
		case services.DayRangeErrorInvalid:
			return apiError(c, fiber.StatusBadRequest, "invalid range")
		default:
			return apiError(c, fiber.StatusBadRequest, "invalid range")
		}
	}
	logs, err := handler.viewerService.FetchLogsForViewer(user, from, to, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch logs")
	}

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
	logEntry, err := handler.viewerService.FetchLogByDateForViewer(user, day, handler.location)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to fetch day")
	}

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
