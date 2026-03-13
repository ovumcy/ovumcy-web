package api

import (
	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) UpdateCycleSettings(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logHealthDataMutationError(c, "settings.cycle_update", spec, "cycle_settings")
		return handler.respondMappedError(c, spec)
	}

	input, parseError := handler.parseCycleSettingsInput(c)
	if parseError != "" {
		spec := settingsValidationErrorSpec(parseError)
		handler.logHealthDataMutationError(c, "settings.cycle_update", spec, "cycle_settings")
		return handler.respondMappedError(c, spec)
	}
	if err := handler.settingsService.SaveCycleSettings(user.ID, input); err != nil {
		spec := settingsCycleUpdateErrorSpec()
		handler.logHealthDataMutationError(c, "settings.cycle_update", spec, "cycle_settings")
		return handler.respondMappedError(c, spec)
	}

	handler.settingsService.ApplyCycleSettings(user, input)
	handler.logHealthDataMutation(c, "settings.cycle_update", "success", "cycle_settings")

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		message := translateMessage(currentMessages(c), "settings.success.cycle_updated")
		if message == "settings.success.cycle_updated" {
			message = "Cycle settings updated successfully."
		}
		return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "cycle_updated"})
	return redirectOrJSON(c, "/settings")
}
