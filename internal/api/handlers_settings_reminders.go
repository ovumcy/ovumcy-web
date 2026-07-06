package api

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// UpdateReminderSettings persists the authenticated owner's shared reminder
// lead window (users.reminder_lead_days, issue #123): how many days ahead the
// dashboard period/ovulation banner (and, when configured, the webhook
// reminder) surfaces.
//
// This is a dedicated state-mutating /api/v1 endpoint — chained behind
// handler.AuthRequired + handler.OwnerOnly and CSRF-protected by the global
// middleware (it is NOT in the CSRF exemption list). The submitted value is
// clamped into the shared 0–14 bound in the service via the SAME
// services.NormalizeReminderLeadDays helper the webhook-settings path uses, so
// an out-of-range value is clamped rather than rejected. The write is scoped to
// the session user_id (user.ID from the sealed auth cookie), never an id from
// the request, and SaveReminderLeadDays skips the DB UPDATE when the clamped
// value is unchanged. It deliberately does NOT bump auth_session_version — a
// reminder preference is not a change to the account's security posture.
func (handler *Handler) UpdateReminderSettings(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input, err := parseReminderSettingsInput(c)
	if err != nil {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}

	changed, err := handler.settingsService.SaveReminderLeadDays(c.Context(), user.ID, user.ReminderLeadDays, input.ReminderLeadDays)
	if err != nil {
		return handler.respondMappedError(c, settingsRemindersUpdateErrorSpec())
	}
	if changed {
		user.ReminderLeadDays = services.NormalizeReminderLeadDays(input.ReminderLeadDays)
	}

	status := services.SettingsReminderUpdatedStatus
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":                 true,
			"status":             status,
			"reminder_lead_days": user.ReminderLeadDays,
		})
	}
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, status, "Reminder settings updated."))
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}

func parseReminderSettingsInput(c fiber.Ctx) (reminderSettingsInput, error) {
	if hasJSONBody(c) {
		input := reminderSettingsInput{}
		if err := c.Bind().Body(&input); err != nil {
			return reminderSettingsInput{}, err
		}
		return input, nil
	}

	leadDays, err := strconv.Atoi(strings.TrimSpace(c.FormValue("reminder_lead_days")))
	if err != nil {
		return reminderSettingsInput{}, err
	}
	return reminderSettingsInput{ReminderLeadDays: leadDays}, nil
}
