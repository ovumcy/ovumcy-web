package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) RegenerateRecoveryCode(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	recoveryCode, err := handler.authService.RegenerateRecoveryCode(user.ID)
	if err != nil {
		return handler.respondMappedError(c, mapRecoveryCodeRegenerationError(err))
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":            true,
			"recovery_code": recoveryCode,
		})
	}

	data, err := handler.buildSettingsViewData(c, user, FlashPayload{})
	if err != nil {
		return handler.respondMappedError(c, settingsLoadErrorSpec())
	}
	data["SuccessKey"] = "settings.success.recovery_code_regenerated"
	data["GeneratedRecoveryCode"] = recoveryCode
	return handler.render(c, "settings", data)
}
