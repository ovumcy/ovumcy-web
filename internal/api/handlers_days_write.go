package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

type upsertDayRequest struct {
	user            *models.User
	location        *time.Location
	day             time.Time
	payload         dayPayload
	cleanSymptomIDs []uint
	hidden          preservedDayFields
	// fields names what a partial write (PatchDay) states; a full write
	// (UpsertDay) states every field and leaves it zero.
	fields services.DayEntryFields
}

// preservedDayFields names the day fields a request must leave to the value
// already stored: the ones the account keeps hidden, which the day form does
// not show and therefore does not speak for. It is resolved once per request
// (hiddenDayFields) and read twice — parseDayPayload skips reading each hidden
// field off the wire, buildUpsertDayEntryInput marks it Preserve* for the save
// — so the two halves cannot disagree about which fields those are. That holds
// by construction rather than by care: resolveUpsertDayRequest reads the
// transport once and hands the same formBody, and the same resolved set, to
// both. A partial write reads it a third time: parseDayPayloadFields never
// names a hidden field as stated.
type preservedDayFields struct {
	SexActivity   bool
	BBT           bool
	CervicalMucus bool
	CycleFactors  bool
	Notes         bool
}

// hiddenDayFields resolves that set for one request. formBody carries the
// transport condition: a form body is the day editor, which posts the fields
// the account shows and nothing else, so a value arriving in a hidden one is
// not this write's subject; a JSON body replaces the record with exactly what
// the client states, so it is granted no preservation and every field it sends
// is read. Read is not the same as validated: a temperature outside the
// accepted range is refused with 400, an unknown spelling of sex_activity,
// cervical_mucus or pregnancy_test collapses to "none", and notes are trimmed.
//
// Which fields an account hides is the services layer's answer, and all five
// come from one TrackingVisibility so that no tracking column is negated in
// transport. Whether the caller may write this owner's day at all is the
// route's question — days.Put and days.Patch declare OwnerOnly (routes.go) — and not this
// set's. Regression: TestUpsertDayRefusesAnImpossibleTemperatureSentAsJSON,
// TestParseDayPayloadSkipsEveryFieldTheAccountHides.
func hiddenDayFields(user *models.User, formBody bool) preservedDayFields {
	if !formBody || user == nil {
		return preservedDayFields{}
	}
	visibility := services.TrackingVisibilityForUser(user)
	return preservedDayFields{
		SexActivity:   visibility.SexChipHidden(),
		BBT:           visibility.BBTFieldHidden(),
		CervicalMucus: visibility.CervicalMucusHidden(),
		CycleFactors:  visibility.CycleFactorsHidden(),
		Notes:         visibility.NotesFieldHidden(),
	}
}

var (
	dayUpsertMutation      = healthMutationKind{action: "health.day_upsert", target: "day_entry"}
	cycleStartMarkMutation = healthMutationKind{action: "health.cycle_start_mark", target: "cycle_start"}
)

// UpsertDay is the full-replace day write (PUT): the body states the whole
// day, and a field it omits is cleared.
func (handler *Handler) UpsertDay(c fiber.Ctx) error {
	request, spec, ok := handler.resolveUpsertDayRequest(c, false)
	if !ok {
		return handler.failDayMutation(c, dayUpsertMutation, spec)
	}

	entry, err := handler.dayService.UpsertDayEntryWithAutoFill(
		c.Context(),
		request.user.ID,
		request.day,
		buildUpsertDayEntryInput(request.payload, request.cleanSymptomIDs, request.hidden),
		request.location,
	)
	if err != nil {
		return handler.failDayMutation(c, dayUpsertMutation, mapDayUpsertError(err))
	}
	return handler.completeDayWrite(c, request, entry)
}

// PatchDay is the partial day write (PATCH): only the fields the body names
// change, and every other field — the cycle start included — keeps its
// stored value. The merge is the services layer's (PatchDayEntryWithAutoFill);
// this handler only reports which fields arrived. Validation, ownership and
// the answer are UpsertDay's, and it audits under the same action.
func (handler *Handler) PatchDay(c fiber.Ctx) error {
	request, spec, ok := handler.resolveUpsertDayRequest(c, true)
	if !ok {
		return handler.failDayMutation(c, dayUpsertMutation, spec)
	}

	entry, err := handler.dayService.PatchDayEntryWithAutoFill(
		c.Context(),
		request.user.ID,
		request.day,
		buildUpsertDayEntryInput(request.payload, request.cleanSymptomIDs, request.hidden),
		request.fields,
		request.location,
	)
	if err != nil {
		return handler.failDayMutation(c, dayUpsertMutation, mapDayUpsertError(err))
	}
	return handler.completeDayWrite(c, request, entry)
}

// completeDayWrite records and answers a day write that has been saved.
func (handler *Handler) completeDayWrite(c fiber.Ctx, request upsertDayRequest, entry models.DailyLog) error {
	feedback, feedbackErr := handler.applyUpsertDayAcknowledgements(c, request)

	handler.logMutationSuccess(c, dayUpsertMutation)
	// The inline question's answer can turn this save into a cycle-start mark
	// too. Audit it under the same action the dedicated endpoint uses, so an
	// operator filtering on health.cycle_start_mark still sees every mark; the
	// saved entry — not the request — decides whether one happened.
	if request.payload.ConfirmCycleStart && entry.CycleStart {
		handler.logMutationSuccess(c, cycleStartMarkMutation)
	}
	return handler.respondUpsertDaySuccess(c, request.day, entry, feedback, feedbackErr)
}

func (handler *Handler) resolveUpsertDayRequest(c fiber.Ctx, partial bool) (upsertDayRequest, APIErrorSpec, bool) {
	user, ok := currentUser(c)
	if !ok {
		return upsertDayRequest{}, unauthorizedErrorSpec(), false
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return upsertDayRequest{}, invalidDateErrorSpec(), false
	}

	formBody := !hasJSONBody(c)
	hidden := hiddenDayFields(user, formBody)
	payload, err := parseDayPayload(c, user, formBody, hidden)
	if err != nil {
		return upsertDayRequest{}, invalidPayloadErrorSpec(), false
	}
	// Read after the payload: the form reads above fold the Content-Type
	// before fasthttp parses the body, so the presence checks see what they
	// read.
	var fields services.DayEntryFields
	if partial {
		fields, err = parseDayPayloadFields(c, formBody, hidden)
		if err != nil {
			return upsertDayRequest{}, invalidPayloadErrorSpec(), false // codecov:ignore -- the bind above already decoded this body with the same decoder; a RawMessage target refuses nothing it accepted
		}
	}

	cleanIDs, err := handler.symptomService.ValidateSymptomIDs(c.Context(), user.ID, payload.SymptomIDs)
	if err != nil {
		return upsertDayRequest{}, invalidSymptomIDsErrorSpec(), false
	}

	return upsertDayRequest{
		user:            user,
		location:        location,
		day:             day,
		payload:         payload,
		cleanSymptomIDs: cleanIDs,
		hidden:          hidden,
		fields:          fields,
	}, APIErrorSpec{}, true
}

func buildUpsertDayEntryInput(payload dayPayload, cleanSymptomIDs []uint, hidden preservedDayFields) services.DayEntryInput {
	return services.DayEntryInput{
		IsPeriod:              payload.IsPeriod,
		ConfirmCycleStart:     payload.ConfirmCycleStart,
		Flow:                  payload.Flow,
		Mood:                  payload.Mood,
		SexActivity:           payload.SexActivity,
		BBT:                   payload.BBT,
		CervicalMucus:         payload.CervicalMucus,
		PregnancyTest:         payload.PregnancyTest,
		CycleFactorKeys:       payload.CycleFactorKeys,
		Notes:                 payload.Notes,
		SymptomIDs:            cleanSymptomIDs,
		PreserveSexActivity:   hidden.SexActivity,
		PreserveBBT:           hidden.BBT,
		PreserveCervicalMucus: hidden.CervicalMucus,
		PreserveCycleFactors:  hidden.CycleFactors,
		PreserveNotes:         hidden.Notes,
	}
}

func (handler *Handler) applyUpsertDayAcknowledgements(c fiber.Ctx, request upsertDayRequest) (services.DayFeedbackState, error) {
	if !request.user.ShownPeriodTip && request.payload.IsPeriod && services.ParseBoolLike(c.FormValue("ack_period_tip")) {
		if err := handler.dayService.AcknowledgePeriodTip(c.Context(), request.user.ID); err == nil { // codecov:ignore -- best-effort period-tip ack; error intentionally swallowed, happy path in e2e
			request.user.ShownPeriodTip = true
		}
	}

	feedback, feedbackErr := handler.dayService.ResolveDayFeedback(c.Context(), request.user, request.day, handler.clockNow().In(request.location), request.location)
	// The no-JS redirect carries no notice, so the warning is not recorded as
	// shown there; it stays pending for a save that can display it.
	if feedbackErr == nil && feedback.ShowLongPeriodWarning && !feedback.LongPeriodCycleStart.IsZero() && !dayFormNavigation(c) {
		if err := handler.dayService.AcknowledgeLongPeriodWarning(c.Context(), request.user.ID, feedback.LongPeriodCycleStart, request.location); err == nil { // codecov:ignore -- best-effort long-period-warning ack; error intentionally swallowed
			acknowledgedCycleStart := feedback.LongPeriodCycleStart
			request.user.LongPeriodWarningCycleStart = &acknowledgedCycleStart
		}
	}
	return feedback, feedbackErr
}

func (handler *Handler) respondUpsertDaySuccess(c fiber.Ctx, day time.Time, entry models.DailyLog, feedback services.DayFeedbackState, feedbackErr error) error {
	if dayFormNavigation(c) {
		// A day form submitted without JavaScript: send the browser back to the
		// page the form was on. The calendar editor names itself with
		// source=calendar, a hidden field (FormValue reads the query string
		// first, so the ?source= DeleteDay uses works here too); the dashboard
		// form is the default.
		if c.FormValue("source") == "calendar" {
			return redirectOrJSON(c, calendarDayPath(day))
		}
		return redirectOrJSON(c, "/dashboard")
	}
	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		if feedbackErr == nil {
			if feedback.ShowSpottingCycleWarning {
				setEncodedResponseNotice(c, "dashboard.spotting_cycle_warning", translateMessage(currentMessages(c), "dashboard.spotting_cycle_warning"))
			} else if feedback.ShowLongPeriodWarning {
				setEncodedResponseNotice(c, "dashboard.long_period_warning", translateMessage(currentMessages(c), "dashboard.long_period_warning"))
			}
			return handler.sendDaySaveStatus(c, feedback.MessageKey)
		}
		return handler.sendDaySaveStatus(c, "")
	}
	return c.JSON(newDayResponse(entry))
}

func (handler *Handler) MarkCycleStart(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, cycleStartMarkMutation, unauthorizedErrorSpec())
	}

	location := handler.requestLocation(c)
	day, err := services.ParseDayDate(c.Params("date"), location)
	if err != nil {
		return handler.failMutation(c, cycleStartMarkMutation, invalidDateErrorSpec())
	}

	// The policy decides whether this mark gets the implantation caution. When
	// it cannot be resolved the zero value suppresses the caution, which is the
	// safe direction for a prediction claim — but a suppressed caution and a
	// caution that was never warranted look identical from outside. The
	// outcome therefore rides the audit line this handler already emits
	// (no new action, no new stream), so an operator can see that the check
	// did not run on a request that still answers 204. Regression:
	// TestMarkCycleStartAuditsAnUnresolvedImplantationPolicy.
	cycleStartPolicy, policyErr := handler.dayService.ResolveManualCycleStartPolicy(c.Context(), user, day, handler.clockNow().In(location), location)
	var auditFields []SecurityEventField
	if policyErr != nil {
		auditFields = append(auditFields, securityEventField("cycle_start_policy", "unresolved"))
	}

	if err := handler.dayService.MarkCycleStartManually(
		c.Context(),
		user.ID,
		day,
		handler.clockNow().In(location),
		location,
		services.ManualCycleStartOptions{
			ReplaceExisting: services.ParseBoolLike(c.FormValue("replace_existing")),
			MarkUncertain:   services.ParseBoolLike(c.FormValue("mark_uncertain")),
		},
	); err != nil {
		return handler.failMutation(c, cycleStartMarkMutation, mapDayUpsertError(err))
	}
	if !user.ShownPeriodTip && services.ParseBoolLike(c.FormValue("ack_period_tip")) {
		if err := handler.dayService.AcknowledgePeriodTip(c.Context(), user.ID); err == nil { // codecov:ignore -- best-effort period-tip ack; error intentionally swallowed, happy path in e2e
			user.ShownPeriodTip = true
		}
	}

	handler.logMutationSuccess(c, cycleStartMarkMutation, auditFields...)

	if isHTMX(c) {
		c.Set("HX-Trigger", "calendar-day-updated")
		c.Set("HX-Refresh", "true")
		if cycleStartPolicy.PotentialImplantation {
			setEncodedResponseNotice(c, "dashboard.implantation_warning", translateMessage(currentMessages(c), "dashboard.implantation_warning"))
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}

	if c.Query("source") == "calendar" {
		return redirectOrJSON(c, calendarDayPath(day))
	}
	return redirectOrJSON(c, "/dashboard")
}

// dayFormNavigation reports whether a day write came from a day form submitted
// without JavaScript. A client that sends PUT or DELETE itself, form body or
// not, keeps its JSON or 204.
func dayFormNavigation(c fiber.Ctx) bool {
	return arrivedAsOverriddenFormPost(c) && responseFormat(c) == httpx.ResponseFormatHTML
}

// failDayMutation records and answers a refused day write or delete. A day form
// submitted without JavaScript has no inline error to fill, so its validation
// refusal is answered 422 on the same page-shaped refusal as a refusal before
// the handler: the localized message and a link back to the page the form is
// on. Only the status changes, and only when that page answers
// (plainPageFormBackPath): a JSON or HTMX client, and a form POST whose Accept
// does not name text/html, keep the 400 and the envelope. The long-period
// acknowledgement stays off on this path (dayFormNavigation).
func (handler *Handler) failDayMutation(c fiber.Ctx, kind healthMutationKind, spec APIErrorSpec) error {
	if _, page := plainPageFormBackPath(c); page && dayFormNavigation(c) && spec.Category == APIErrorCategoryValidation && spec.Status == fiber.StatusBadRequest {
		spec.Status = fiber.StatusUnprocessableEntity
	}
	return handler.failMutation(c, kind, spec)
}

// calendarDayPath is the calendar page opened on day, where a calendar form
// submitted without JavaScript lands.
func calendarDayPath(day time.Time) string {
	return "/calendar?month=" + day.Format("2006-01") + "&day=" + day.Format("2006-01-02")
}
