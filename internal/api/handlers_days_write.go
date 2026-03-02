package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) UpsertDay(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	day, err := services.ParseDayDate(c.Params("date"), handler.location)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid date")
	}

	payload, err := parseDayPayload(c)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid payload")
	}
	cleanIDs, err := handler.symptomService.ValidateSymptomIDs(user.ID, payload.SymptomIDs)
	if err != nil {
		return apiError(c, fiber.StatusBadRequest, "invalid symptom ids")
	}
	entry, err := handler.dayService.UpsertDayEntryWithAutoFill(user.ID, day, services.DayEntryInput{
		IsPeriod:   payload.IsPeriod,
		Flow:       payload.Flow,
		Notes:      payload.Notes,
		SymptomIDs: cleanIDs,
	}, handler.location)
	if err != nil {
		switch services.ClassifyDayUpsertError(err) {
		case services.DayUpsertErrorInvalidFlow:
			return apiError(c, fiber.StatusBadRequest, "invalid flow value")
		case services.DayUpsertErrorLoadFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to load day")
		case services.DayUpsertErrorSyncLastPeriodFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to sync last period start")
		default:
			return upsertDayPersistenceAPIError(c, err)
		}
	}

	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		return handler.sendDaySaveStatus(c)
	}

	return c.JSON(entry)
}
