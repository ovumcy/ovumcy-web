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

var (
	dayUpsertMutation      = healthMutationKind{action: "health.day_upsert", target: "day_entry"}
	cycleStartMarkMutation = healthMutationKind{action: "health.cycle_start_mark", target: "cycle_start"}
)

func (handler *Handler) UpsertDay(c *fiber.Ctx) error {
	request, spec, ok := handler.resolveUpsertDayRequest(c)
	if !ok {
		handler.logMutationError(c, dayUpsertMutation, spec)
		return handler.respondMappedError(c, spec)
	}

	entry, err := handler.dayService.UpsertDayEntryWithAutoFill(
		c.UserContext(),
		request.user.ID,
		request.day,
		buildUpsertDayEntryInput(request.payload, request.cleanSymptomIDs, request.user, !hasJSONBody(c)),
		request.location,
	)
	if err != nil {
		return handler.failMutation(c, dayUpsertMutation, mapDayUpsertError(err))
	}

	feedback, feedbackErr := handler.applyUpsertDayAcknowledgements(c, request)

	handler.logMutationSuccess(c, dayUpsertMutation)
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

	cleanIDs, err := handler.symptomService.ValidateSymptomIDs(c.UserContext(), user.ID, payload.SymptomIDs)
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
	preserveLHTest := preserveHiddenFields && user != nil && !user.TrackLHTest
	preserveCycleFactors := preserveHiddenFields && user != nil && user.HideCycleFactors
	preserveNotes := preserveHiddenFields && user != nil && user.HideNotesField

	return services.DayEntryInput{
		IsPeriod:              payload.IsPeriod,
		Flow:                  payload.Flow,
		Mood:                  payload.Mood,
		SexActivity:           payload.SexActivity,
		BBT:                   payload.BBT,
		CervicalMucus:         payload.CervicalMucus,
		PregnancyTest:         payload.PregnancyTest,
		LHTest:                payload.LHTest,
		CycleFactorKeys:       payload.CycleFactorKeys,
		Notes:                 payload.Notes,
		SymptomIDs:            cleanSymptomIDs,
		PreserveSexActivity:   preserveSexActivity,
		PreserveBBT:           preserveBBT,
		PreserveCervicalMucus: preserveCervicalMucus,
		PreserveLHTest:        preserveLHTest,
		PreserveCycleFactors:  preserveCycleFactors,
		PreserveNotes:         preserveNotes,
	}
}

func (handler *Handler) applyUpsertDayAcknowledgements(c *fiber.Ctx, request upsertDayRequest) (services.DayFeedbackState, error) {
	if !request.user.ShownPeriodTip && request.payload.IsPeriod && services.ParseBoolLike(c.FormValue("ack_period_tip")) {
		if err := handler.dayService.AcknowledgePeriodTip(c.UserContext(), request.user.ID); err == nil { // codecov:ignore -- best-effort period-tip ack; error intentionally swallowed, happy path in e2e
			request.user.ShownPeriodTip = true
		}
	}

	feedback, feedbackErr := handler.dayService.ResolveDayFeedback(c.UserContext(), request.user, request.day, time.Now().In(request.location), request.location)
	if feedbackErr == nil && feedback.ShowLongPeriodWarning && !feedback.LongPeriodCycleStart.IsZero() {
		if err := handler.dayService.AcknowledgeLongPeriodWarning(c.UserContext(), request.user.ID, feedback.LongPeriodCycleStart, request.location); err == nil { // codecov:ignore -- best-effort long-period-warning ack; error intentionally swallowed
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
	return c.JSON(newDayResponse(entry))
}

func (handler *Handler) MarkCycleStart(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, cycleStartMarkMutation, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.failMutation(c, cycleStartMarkMutation, invalidDateErrorSpec())
	}

	cycleStartPolicy, _ := handler.dayService.ResolveManualCycleStartPolicy(c.UserContext(), user, day, time.Now().In(location), location)

	if err := handler.dayService.MarkCycleStartManually(
		c.UserContext(),
		user.ID,
		day,
		time.Now().In(location),
		location,
		services.ManualCycleStartOptions{
			ReplaceExisting: services.ParseBoolLike(c.FormValue("replace_existing")),
			MarkUncertain:   services.ParseBoolLike(c.FormValue("mark_uncertain")),
		},
	); err != nil {
		return handler.failMutation(c, cycleStartMarkMutation, mapDayUpsertError(err))
	}
	if !user.ShownPeriodTip && services.ParseBoolLike(c.FormValue("ack_period_tip")) {
		if err := handler.dayService.AcknowledgePeriodTip(c.UserContext(), user.ID); err == nil { // codecov:ignore -- best-effort period-tip ack; error intentionally swallowed, happy path in e2e
			user.ShownPeriodTip = true
		}
	}

	handler.logMutationSuccess(c, cycleStartMarkMutation)

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
