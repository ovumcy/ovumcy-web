package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ChangePassword(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	input := changePasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return handler.respondSettingsError(c, fiber.StatusBadRequest, "invalid settings input")
	}
	if err := handler.settingsService.ChangePassword(
		user.ID,
		user.PasswordHash,
		input.CurrentPassword,
		input.NewPassword,
		input.ConfirmPassword,
	); err != nil {
		switch services.ClassifySettingsPasswordChangeError(err) {
		case services.SettingsPasswordChangeErrorInvalidInput:
			return handler.respondSettingsError(c, fiber.StatusBadRequest, "invalid settings input")
		case services.SettingsPasswordChangeErrorPasswordMismatch:
			return handler.respondSettingsError(c, fiber.StatusBadRequest, "password mismatch")
		case services.SettingsPasswordChangeErrorInvalidCurrentPassword:
			return handler.respondSettingsError(c, fiber.StatusUnauthorized, "invalid current password")
		case services.SettingsPasswordChangeErrorNewPasswordMustDiffer:
			return handler.respondSettingsError(c, fiber.StatusBadRequest, "new password must differ")
		case services.SettingsPasswordChangeErrorWeakPassword:
			return handler.respondSettingsError(c, fiber.StatusBadRequest, "weak password")
		case services.SettingsPasswordChangeErrorHashFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to secure password")
		case services.SettingsPasswordChangeErrorUpdateFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to update password")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to update password")
		}
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "password_changed"})
	return redirectOrJSON(c, "/settings")
}
