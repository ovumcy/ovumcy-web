package api

import (
	"time"

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

	payload, err := parseDayPayload(c, user)
	if err != nil {
		return handler.respondMappedError(c, invalidPayloadErrorSpec())
	}
	cleanIDs, err := handler.symptomService.ValidateSymptomIDs(user.ID, payload.SymptomIDs)
	if err != nil {
		return handler.respondMappedError(c, invalidSymptomIDsErrorSpec())
	}
	entry, err := handler.dayService.UpsertDayEntryWithAutoFill(user.ID, day, services.DayEntryInput{
		IsPeriod:      payload.IsPeriod,
		Flow:          payload.Flow,
		Mood:          payload.Mood,
		SexActivity:   payload.SexActivity,
		BBT:           payload.BBT,
		CervicalMucus: payload.CervicalMucus,
		Notes:         payload.Notes,
		SymptomIDs:    cleanIDs,
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

func (handler *Handler) MarkCycleStart(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.respondMappedError(c, invalidDateErrorSpec())
	}

	if err := handler.dayService.MarkCycleStartManually(
		user.ID,
		day,
		time.Now().In(location),
		location,
		services.ManualCycleStartOptions{
			ReplaceExisting: services.ParseBoolLike(c.FormValue("replace_existing")),
			MarkUncertain:   services.ParseBoolLike(c.FormValue("mark_uncertain")),
		},
	); err != nil {
		return handler.respondMappedError(c, upsertDayPersistenceErrorSpec(err))
	}

	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		c.Set("HX-Refresh", "true")
		return c.SendStatus(fiber.StatusNoContent)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}

	if c.Query("source") == "calendar" {
		month := day.Format("2006-01")
		return redirectOrJSON(c, "/calendar?month="+month+"&day="+day.Format("2006-01-02"))
	}
	return redirectOrJSON(c, "/dashboard")
}
