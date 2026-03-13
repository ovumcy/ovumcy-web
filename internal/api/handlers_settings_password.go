package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ChangePassword(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.password_change", spec)
		return handler.respondMappedError(c, spec)
	}

	input := changePasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		spec := settingsInvalidInputErrorSpec()
		handler.logSecurityError(c, "auth.password_change", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.settingsService.ChangePassword(user, input.CurrentPassword, input.NewPassword, input.ConfirmPassword); err != nil {
		spec := mapSettingsPasswordChangeError(err)
		handler.logSecurityError(c, "auth.password_change", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.setAuthCookie(c, user, false); err != nil {
		handler.clearAuthCookie(c)
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.password_change", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.password_change", "success")
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	if isHTMX(c) {
		messageKey := services.SettingsStatusTranslationKey("password_changed")
		message := translateMessage(currentMessages(c), messageKey)
		if message == "" || message == messageKey {
			message = "Password changed successfully."
		}
		return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "password_changed"})
	return redirectOrJSON(c, "/settings")
}
