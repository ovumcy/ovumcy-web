package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type upsertDayRequest struct {
	user            *models.User
	location        *time.Location
	day             time.Time
	payload         dayPayload
	cleanSymptomIDs []uint
}

func (handler *Handler) UpsertDay(c *fiber.Ctx) error {
	request, spec, ok := handler.resolveUpsertDayRequest(c)
	if !ok {
		handler.logHealthDataMutationError(c, "health.day_upsert", spec, "day_entry")
		return handler.respondMappedError(c, spec)
	}

	entry, err := handler.dayService.UpsertDayEntryWithAutoFill(
		request.user.ID,
		request.day,
		buildUpsertDayEntryInput(request.payload, request.cleanSymptomIDs, request.user, !hasJSONBody(c)),
		request.location,
	)
	if err != nil {
		spec := upsertDayPersistenceErrorSpec(err)
		handler.logHealthDataMutationError(c, "health.day_upsert", spec, "day_entry")
		return handler.respondMappedError(c, spec)
	}

	feedback, feedbackErr := handler.applyUpsertDayAcknowledgements(c, request)

	handler.logHealthDataMutation(c, "health.day_upsert", "success", "day_entry")
	return handler.respondUpsertDaySuccess(c, entry, feedback, feedbackErr)
}

func (handler *Handler) resolveUpsertDayRequest(c *fiber.Ctx) (upsertDayRequest, APIErrorSpec, bool) {
	user, ok := currentUser(c)
	if !ok {
		return upsertDayRequest{}, unauthorizedErrorSpec(), false
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return upsertDayRequest{}, invalidDateErrorSpec(), false
	}

	payload, err := parseDayPayload(c, user)
	if err != nil {
		return upsertDayRequest{}, invalidPayloadErrorSpec(), false
	}

	cleanIDs, err := handler.symptomService.ValidateSymptomIDs(user.ID, payload.SymptomIDs)
	if err != nil {
		return upsertDayRequest{}, invalidSymptomIDsErrorSpec(), false
	}

	return upsertDayRequest{
		user:            user,
		location:        location,
		day:             day,
		payload:         payload,
		cleanSymptomIDs: cleanIDs,
	}, APIErrorSpec{}, true
}

func buildUpsertDayEntryInput(payload dayPayload, cleanSymptomIDs []uint, user *models.User, preserveHiddenFields bool) services.DayEntryInput {
	preserveSexActivity := preserveHiddenFields && user != nil && user.HideSexChip
	preserveBBT := preserveHiddenFields && user != nil && !user.TrackBBT
	preserveCervicalMucus := preserveHiddenFields && user != nil && !user.TrackCervicalMucus
	preserveCycleFactors := preserveHiddenFields && user != nil && user.HideCycleFactors
	preserveNotes := preserveHiddenFields && user != nil && user.HideNotesField

	return services.DayEntryInput{
		IsPeriod:              payload.IsPeriod,
		Flow:                  payload.Flow,
		Mood:                  payload.Mood,
		SexActivity:           payload.SexActivity,
		BBT:                   payload.BBT,
		CervicalMucus:         payload.CervicalMucus,
		CycleFactorKeys:       payload.CycleFactorKeys,
		Notes:                 payload.Notes,
		SymptomIDs:            cleanSymptomIDs,
		PreserveSexActivity:   preserveSexActivity,
		PreserveBBT:           preserveBBT,
		PreserveCervicalMucus: preserveCervicalMucus,
		PreserveCycleFactors:  preserveCycleFactors,
		PreserveNotes:         preserveNotes,
	}
}

func (handler *Handler) applyUpsertDayAcknowledgements(c *fiber.Ctx, request upsertDayRequest) (services.DayFeedbackState, error) {
	if !request.user.ShownPeriodTip && request.payload.IsPeriod && services.ParseBoolLike(c.FormValue("ack_period_tip")) {
		if err := handler.dayService.AcknowledgePeriodTip(request.user.ID); err == nil {
			request.user.ShownPeriodTip = true
		}
	}

	feedback, feedbackErr := handler.dayService.ResolveDayFeedback(request.user, request.day, time.Now().In(request.location), request.location)
	if feedbackErr == nil && feedback.ShowLongPeriodWarning && !feedback.LongPeriodCycleStart.IsZero() {
		if err := handler.dayService.AcknowledgeLongPeriodWarning(request.user.ID, feedback.LongPeriodCycleStart, request.location); err == nil {
			warnedAt := feedback.LongPeriodCycleStart
			request.user.LongPeriodWarnedAt = &warnedAt
		}
	}
	return feedback, feedbackErr
}

func (handler *Handler) respondUpsertDaySuccess(c *fiber.Ctx, entry models.DailyLog, feedback services.DayFeedbackState, feedbackErr error) error {
	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		if feedbackErr == nil {
			if feedback.ShowSpottingCycleWarning {
				setEncodedResponseNotice(c, translateMessage(currentMessages(c), "dashboard.spotting_cycle_warning"))
			} else if feedback.ShowLongPeriodWarning {
				setEncodedResponseNotice(c, translateMessage(currentMessages(c), "dashboard.long_period_warning"))
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
			setEncodedResponseNotice(c, translateMessage(currentMessages(c), "dashboard.implantation_warning"))
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
