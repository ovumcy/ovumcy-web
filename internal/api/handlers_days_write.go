package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) UpsertDay(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "health.day_upsert", spec, "day_entry")
		return handler.respondMappedError(c, spec)
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		spec := invalidDateErrorSpec()
		handler.logHealthDataMutationError(c, "health.day_upsert", spec, "day_entry")
		return handler.respondMappedError(c, spec)
	}

	payload, err := parseDayPayload(c, user)
	if err != nil {
		spec := invalidPayloadErrorSpec()
		handler.logHealthDataMutationError(c, "health.day_upsert", spec, "day_entry")
		return handler.respondMappedError(c, spec)
	}
	cleanIDs, err := handler.symptomService.ValidateSymptomIDs(user.ID, payload.SymptomIDs)
	if err != nil {
		spec := invalidSymptomIDsErrorSpec()
		handler.logHealthDataMutationError(c, "health.day_upsert", spec, "day_entry")
		return handler.respondMappedError(c, spec)
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
		spec := upsertDayPersistenceErrorSpec(err)
		handler.logHealthDataMutationError(c, "health.day_upsert", spec, "day_entry")
		return handler.respondMappedError(c, spec)
	}
	if !user.ShownPeriodTip && payload.IsPeriod && services.ParseBoolLike(c.FormValue("ack_period_tip")) {
		if err := handler.dayService.AcknowledgePeriodTip(user.ID); err == nil {
			user.ShownPeriodTip = true
		}
	}

	feedback, feedbackErr := handler.dayService.ResolveDayFeedback(user, day, time.Now().In(location), location)
	if feedbackErr == nil && feedback.ShowLongPeriodWarning && !feedback.LongPeriodCycleStart.IsZero() {
		if err := handler.dayService.AcknowledgeLongPeriodWarning(user.ID, feedback.LongPeriodCycleStart, location); err == nil {
			warnedAt := feedback.LongPeriodCycleStart
			user.LongPeriodWarnedAt = &warnedAt
		}
	}

	handler.logHealthDataMutation(c, "health.day_upsert", "success", "day_entry")

	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		if feedbackErr == nil {
			if feedback.ShowSpottingCycleWarning {
				c.Set("X-Ovumcy-Notice", translateMessage(currentMessages(c), "dashboard.spotting_cycle_warning"))
			} else if feedback.ShowLongPeriodWarning {
				c.Set("X-Ovumcy-Notice", translateMessage(currentMessages(c), "dashboard.long_period_warning"))
			}
			return handler.sendDaySaveStatus(c, feedback.MessageKey)
		}
		return handler.sendDaySaveStatus(c, "")
	}

	return c.JSON(entry)
}

func (handler *Handler) MarkCycleStart(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "health.cycle_start_mark", spec, "cycle_start")
		return handler.respondMappedError(c, spec)
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		spec := invalidDateErrorSpec()
		handler.logHealthDataMutationError(c, "health.cycle_start_mark", spec, "cycle_start")
		return handler.respondMappedError(c, spec)
	}

	cycleStartPolicy, _ := handler.dayService.ResolveManualCycleStartPolicy(user, day, time.Now().In(location), location)

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
		spec := upsertDayPersistenceErrorSpec(err)
		handler.logHealthDataMutationError(c, "health.cycle_start_mark", spec, "cycle_start")
		return handler.respondMappedError(c, spec)
	}
	if !user.ShownPeriodTip && services.ParseBoolLike(c.FormValue("ack_period_tip")) {
		if err := handler.dayService.AcknowledgePeriodTip(user.ID); err == nil {
			user.ShownPeriodTip = true
		}
	}

	handler.logHealthDataMutation(c, "health.cycle_start_mark", "success", "cycle_start")

	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		c.Set("HX-Refresh", "true")
		if cycleStartPolicy.PotentialImplantation {
			c.Set("X-Ovumcy-Notice", translateMessage(currentMessages(c), "dashboard.implantation_warning"))
		}
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
