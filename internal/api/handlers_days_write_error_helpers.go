package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func upsertDayPersistenceAPIError(c *fiber.Ctx, err error) error {
	switch services.ClassifyDayUpsertError(err) {
	case services.DayUpsertErrorLoadFailed:
		return apiError(c, fiber.StatusInternalServerError, "failed to load day")
	case services.DayUpsertErrorCreateFailed:
		return apiError(c, fiber.StatusInternalServerError, "failed to create day")
	case services.DayUpsertErrorUpdateFailed:
		return apiError(c, fiber.StatusInternalServerError, "failed to update day")
	case services.DayUpsertErrorSyncLastPeriodFailed:
		return apiError(c, fiber.StatusInternalServerError, "failed to sync last period start")
	default:
		return apiError(c, fiber.StatusInternalServerError, "failed to update day")
	}
}

func deleteDayPersistenceAPIError(c *fiber.Ctx, err error) error {
	switch services.ClassifyDayDeleteError(err) {
	case services.DayDeleteErrorDeleteFailed:
		return apiError(c, fiber.StatusInternalServerError, "failed to delete day")
	case services.DayDeleteErrorSyncLastPeriodFailed:
		return apiError(c, fiber.StatusInternalServerError, "failed to sync last period start")
	default:
		return apiError(c, fiber.StatusInternalServerError, "failed to delete day")
	}
}
