package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

var webhookSettingsMutation = healthMutationKind{action: "settings.webhook_update", target: "webhook_settings"}

// UpdateWebhookSettings persists an owner's webhook notification settings
// (issue #124): the enable toggle, the two per-kind notify toggles, and the
// WRITE-ONLY endpoint URL. The URL is a secret (it can embed an ntfy/Gotify
// token), so this handler treats it like a password field:
//
//   - it never renders the stored URL back (the settings page shows only
//     status + host);
//   - a blank URL means "leave the stored endpoint unchanged"; a distinct
//     remove affordance clears it;
//   - all validation, the keep/replace/remove decision, and encryption live in
//     WebhookSettingsService — the transport layer only parses and forwards;
//   - nothing about the URL is logged (the mutation audit records action +
//     outcome only, never the value).
//
// It is OwnerOnly + CSRF-protected at the route/middleware layer and scoped to
// the authenticated session's user id. It does not bump auth_session_version:
// a notification-preference change is not a security-posture change.
func (handler *Handler) UpdateWebhookSettings(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, webhookSettingsMutation, unauthorizedErrorSpec())
	}

	input, err := parseWebhookSettingsInput(c)
	if err != nil {
		return handler.failMutation(c, webhookSettingsMutation, settingsInvalidInputErrorSpec())
	}

	form := services.WebhookSettingsFormUpdate{
		Enabled:         input.Enabled,
		NotifyPeriod:    input.NotifyPeriod,
		NotifyOvulation: input.NotifyOvulation,
		URL:             input.URL,
		URLProvided:     input.URL != "",
		RemoveURL:       input.RemoveURL,
	}
	if err := handler.webhookSettingsSvc.SaveWebhookSettingsFromForm(c.Context(), user.ID, form); err != nil {
		return handler.failMutation(c, webhookSettingsMutation, mapSettingsWebhookSaveError(err))
	}

	status := services.SettingsWebhookUpdatedStatus
	handler.logMutationSuccess(c, webhookSettingsMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":               true,
			"status":           status,
			"webhook_enabled":  form.Enabled,
			"notify_period":    form.NotifyPeriod,
			"notify_ovulation": form.NotifyOvulation,
		})
	}
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, status, "Webhook settings updated successfully."))
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}

func parseWebhookSettingsInput(c fiber.Ctx) (webhookSettingsInput, error) {
	input := webhookSettingsInput{}
	if hasJSONBody(c) {
		if err := c.Bind().Body(&input); err != nil {
			return webhookSettingsInput{}, err
		}
		return input, nil
	}

	return webhookSettingsInput{
		Enabled:         services.ParseBoolLike(c.FormValue("webhook_enabled")),
		URL:             c.FormValue("webhook_url"),
		NotifyPeriod:    services.ParseBoolLike(c.FormValue("webhook_notify_period")),
		NotifyOvulation: services.ParseBoolLike(c.FormValue("webhook_notify_ovulation")),
		RemoveURL:       services.ParseBoolLike(c.FormValue("webhook_remove_url")),
	}, nil
}
