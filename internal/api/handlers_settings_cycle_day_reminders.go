package api

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Cycle-day reminder rules (WEB-383) — transport only. The rules live in the
// egress card beside the webhook they deliver through, so every answer is that
// card rebuilt from a read taken after the write, refusals included.

var (
	cycleDayReminderCreateMutation = healthMutationKind{action: "settings.cycle_day_reminder_create", target: "cycle_day_reminder"}
	cycleDayReminderUpdateMutation = healthMutationKind{action: "settings.cycle_day_reminder_update", target: "cycle_day_reminder"}
	cycleDayReminderDeleteMutation = healthMutationKind{action: "settings.cycle_day_reminder_delete", target: "cycle_day_reminder"}
)

// cycleDayReminderInput is the transport shape of one rule.
type cycleDayReminderInput struct {
	CycleDay     int    `json:"cycle_day" form:"cycle_day"`
	RemindAtHour int    `json:"remind_at_hour" form:"remind_at_hour"`
	Label        string `json:"label" form:"label"`
	SendLabel    bool   `json:"send_label" form:"send_label"`
}

// errCycleDayReminderMalformedBody marks a JSON body that does not bind: the
// caller's fault, answered as invalid input rather than an internal error.
var errCycleDayReminderMalformedBody = errors.New("malformed cycle-day reminder body")

func parseCycleDayReminderInput(c fiber.Ctx) (models.CycleDayReminderInput, error) {
	input := cycleDayReminderInput{}
	if hasJSONBody(c) {
		if err := c.Bind().Body(&input); err != nil {
			return models.CycleDayReminderInput{}, errCycleDayReminderMalformedBody
		}
	} else {
		day, err := strconv.Atoi(strings.TrimSpace(c.FormValue("cycle_day")))
		if err != nil {
			return models.CycleDayReminderInput{}, services.ErrCycleDayReminderInvalidDay
		}
		hour, err := strconv.Atoi(strings.TrimSpace(c.FormValue("remind_at_hour")))
		if err != nil {
			return models.CycleDayReminderInput{}, services.ErrCycleDayReminderInvalidHour
		}
		input = cycleDayReminderInput{
			CycleDay:     day,
			RemindAtHour: hour,
			Label:        c.FormValue("label"),
			SendLabel:    services.ParseBoolLike(c.FormValue("send_label")),
		}
	}
	return models.CycleDayReminderInput{
		CycleDay:     input.CycleDay,
		RemindAtHour: input.RemindAtHour,
		Label:        input.Label,
		SendLabel:    input.SendLabel,
	}, nil
}

// CreateCycleDayReminder adds a rule.
func (handler *Handler) CreateCycleDayReminder(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, cycleDayReminderCreateMutation, unauthorizedErrorSpec())
	}
	input, err := parseCycleDayReminderInput(c)
	if err == nil {
		err = handler.cycleDayReminderSvc.Create(c.Context(), user.ID, input)
	}
	if err != nil {
		return handler.failEgressMutation(c, cycleDayReminderCreateMutation, user, mapCycleDayReminderError(err), egressResultCycleDay, "")
	}
	handler.logMutationSuccess(c, cycleDayReminderCreateMutation)
	return handler.respondEgressMutation(c, user, egressResultCycleDay, services.SettingsCycleDayReminderAddedStatus)
}

// UpdateCycleDayReminder rewrites one of the owner's rules.
func (handler *Handler) UpdateCycleDayReminder(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, cycleDayReminderUpdateMutation, unauthorizedErrorSpec())
	}
	ruleID, err := parseRequestUint(c.Params("id"))
	if err != nil {
		err = services.ErrCycleDayReminderNotFound
	}
	var input models.CycleDayReminderInput
	if err == nil {
		input, err = parseCycleDayReminderInput(c)
	}
	if err == nil {
		err = handler.cycleDayReminderSvc.Update(c.Context(), user.ID, ruleID, input)
	}
	if err != nil {
		return handler.failEgressMutation(c, cycleDayReminderUpdateMutation, user, mapCycleDayReminderError(err), egressResultCycleDay, "")
	}
	handler.logMutationSuccess(c, cycleDayReminderUpdateMutation)
	return handler.respondEgressMutation(c, user, egressResultCycleDay, services.SettingsCycleDayReminderSavedStatus)
}

// DeleteCycleDayReminder removes one of the owner's rules.
func (handler *Handler) DeleteCycleDayReminder(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, cycleDayReminderDeleteMutation, unauthorizedErrorSpec())
	}
	ruleID, err := parseRequestUint(c.Params("id"))
	if err != nil {
		err = services.ErrCycleDayReminderNotFound
	} else {
		err = handler.cycleDayReminderSvc.Delete(c.Context(), user.ID, ruleID)
	}
	if err != nil {
		return handler.failEgressMutation(c, cycleDayReminderDeleteMutation, user, mapCycleDayReminderDeleteError(err), egressResultCycleDay, "")
	}
	handler.logMutationSuccess(c, cycleDayReminderDeleteMutation)
	return handler.respondEgressMutation(c, user, egressResultCycleDay, services.SettingsCycleDayReminderRemovedStatus)
}
