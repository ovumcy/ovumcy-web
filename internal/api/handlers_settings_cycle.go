package api

import (
	"github.com/gofiber/fiber/v2"
)

var cycleSettingsMutation = healthMutationKind{action: "settings.cycle_update", target: "cycle_settings"}

func (handler *Handler) UpdateCycleSettings(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, cycleSettingsMutation, unauthorizedErrorSpec())
	}

	input, parseError := handler.parseCycleSettingsInput(c)
	if parseError != "" {
		return handler.failMutation(c, cycleSettingsMutation, settingsValidationErrorSpec(parseError))
	}
	if err := handler.settingsService.SaveCycleSettings(c.UserContext(), user.ID, input); err != nil {
		return handler.failMutation(c, cycleSettingsMutation, settingsCycleUpdateErrorSpec())
	}

	handler.settingsService.ApplyCycleSettings(user, input)
	handler.logMutationSuccess(c, cycleSettingsMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, "cycle_updated", "Cycle settings updated successfully."))
	}

	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "cycle_updated"})
	return redirectOrJSON(c, "/settings")
}
