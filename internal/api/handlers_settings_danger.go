package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func (handler *Handler) ValidateClearDataPassword(c *fiber.Ctx) error {
	_, spec, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, "settings.clear_data_validate", spec)
		return handler.respondMappedError(c, spec)
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (handler *Handler) ClearAllData(c *fiber.Ctx) error {
	user, spec, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, "settings.clear_data", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.settingsService.ClearAllData(user.ID); err != nil {
		spec := settingsClearDataErrorSpec()
		handler.logSecurityError(c, "settings.clear_data", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "settings.clear_data", "success")
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "data_cleared"})
	return redirectOrJSON(c, "/settings")
}

func (handler *Handler) DeleteAccount(c *fiber.Ctx) error {
	user, spec, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, "settings.delete_account", spec)
		return handler.respondMappedError(c, spec)
	}

	if err := handler.settingsService.DeleteAccount(user.ID); err != nil {
		spec := settingsDeleteAccountErrorSpec()
		handler.logSecurityError(c, "settings.delete_account", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.clearAuthRelatedCookies(c)
	handler.logSecurityEvent(c, "settings.delete_account", "success")
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return redirectOrJSON(c, "/login")
}

func parsePasswordProtectedSettingsAction(c *fiber.Ctx) (string, APIErrorSpec, bool) {
	input := passwordProtectedSettingsInput{}
	if err := c.BodyParser(&input); err != nil && hasJSONBody(c) {
		spec := settingsMissingPasswordErrorSpec()
		return "", spec, false
	}
	if input.Password == "" {
		spec := settingsMissingPasswordErrorSpec()
		return "", spec, false
	}
	return input.Password, APIErrorSpec{}, true
}

func (handler *Handler) validateSettingsActionPassword(c *fiber.Ctx) (*models.User, APIErrorSpec, bool) {
	user, ok := currentUser(c)
	if !ok {
		return nil, unauthorizedErrorSpec(), false
	}

	password, spec, valid := parsePasswordProtectedSettingsAction(c)
	if !valid {
		return nil, spec, false
	}
	if err := handler.settingsService.ValidateCurrentPassword(user.PasswordHash, password); err != nil {
		return nil, mapSettingsDeleteAccountPasswordError(err), false
	}

	return user, APIErrorSpec{}, true
}
