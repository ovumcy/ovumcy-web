package api

import (
	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) ChangePassword(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input := changePasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return handler.respondMappedError(c, settingsInvalidInputErrorSpec())
	}
	if err := handler.settingsService.ChangePassword(
		user.ID,
		user.PasswordHash,
		input.CurrentPassword,
		input.NewPassword,
		input.ConfirmPassword,
	); err != nil {
		return handler.respondMappedError(c, mapSettingsPasswordChangeError(err))
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "password_changed"})
	return redirectOrJSON(c, "/settings")
}
