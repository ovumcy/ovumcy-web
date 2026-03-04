package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) RegenerateRecoveryCode(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	recoveryCode, err := handler.authService.RegenerateRecoveryCode(user.ID)
	if err != nil {
		switch services.ClassifyRecoveryCodeRegenerationError(err) {
		case services.RecoveryCodeRegenerationErrorGenerateFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to create recovery code")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to update recovery code")
		}
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":            true,
			"recovery_code": recoveryCode,
		})
	}

	data, err := handler.buildSettingsViewData(c, user, FlashPayload{})
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to load settings")
	}
	data["SuccessKey"] = "settings.success.recovery_code_regenerated"
	data["GeneratedRecoveryCode"] = recoveryCode
	return handler.render(c, "settings", data)
}
