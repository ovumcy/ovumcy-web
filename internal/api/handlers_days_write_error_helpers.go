package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func upsertDayPersistenceAPIError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, services.ErrDayEntryLoadFailed):
		return apiError(c, fiber.StatusInternalServerError, "failed to load day")
	case errors.Is(err, services.ErrDayEntryCreateFailed):
		return apiError(c, fiber.StatusInternalServerError, "failed to create day")
	case errors.Is(err, services.ErrDayEntryUpdateFailed):
		return apiError(c, fiber.StatusInternalServerError, "failed to update day")
	default:
		return apiError(c, fiber.StatusInternalServerError, "failed to update day")
	}
}

func deleteDayPersistenceAPIError(c *fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, services.ErrDeleteDayFailed):
		return apiError(c, fiber.StatusInternalServerError, "failed to delete day")
	case errors.Is(err, services.ErrSyncLastPeriodFailed):
		return apiError(c, fiber.StatusInternalServerError, "failed to sync last period start")
	default:
		return apiError(c, fiber.StatusInternalServerError, "failed to delete day")
	}
}
