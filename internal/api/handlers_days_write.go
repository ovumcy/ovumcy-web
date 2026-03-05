package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) UpsertDay(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.respondMappedError(c, invalidDateErrorSpec())
	}

	payload, err := parseDayPayload(c)
	if err != nil {
		return handler.respondMappedError(c, invalidPayloadErrorSpec())
	}
	cleanIDs, err := handler.symptomService.ValidateSymptomIDs(user.ID, payload.SymptomIDs)
	if err != nil {
		return handler.respondMappedError(c, invalidSymptomIDsErrorSpec())
	}
	entry, err := handler.dayService.UpsertDayEntryWithAutoFill(user.ID, day, services.DayEntryInput{
		IsPeriod:   payload.IsPeriod,
		Flow:       payload.Flow,
		Notes:      payload.Notes,
		SymptomIDs: cleanIDs,
	}, location)
	if err != nil {
		return handler.respondMappedError(c, upsertDayPersistenceErrorSpec(err))
	}

	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		return handler.sendDaySaveStatus(c)
	}

	return c.JSON(entry)
}
