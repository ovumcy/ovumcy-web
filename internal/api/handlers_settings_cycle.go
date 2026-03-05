package api

import (
	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) UpdateCycleSettings(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input, parseError := handler.parseCycleSettingsInput(c)
	if parseError != "" {
		return handler.respondMappedError(c, settingsValidationErrorSpec(parseError))
	}
	if err := handler.settingsService.SaveCycleSettings(user.ID, input); err != nil {
		return handler.respondMappedError(c, settingsCycleUpdateErrorSpec())
	}

	handler.settingsService.ApplyCycleSettings(user, input)

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
