package api

import "github.com/gofiber/fiber/v2"

func (handler *Handler) ClearAllData(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "settings.clear_data", spec)
		return handler.respondMappedError(c, spec)
	}
	password, spec, valid := parsePasswordProtectedSettingsAction(c)
	if !valid {
		handler.logSecurityError(c, "settings.clear_data", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.settingsService.ValidateCurrentPassword(user.PasswordHash, password); err != nil {
		spec := mapSettingsDeleteAccountPasswordError(err)
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
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "settings.delete_account", spec)
		return handler.respondMappedError(c, spec)
	}
	password, spec, valid := parsePasswordProtectedSettingsAction(c)
	if !valid {
		handler.logSecurityError(c, "settings.delete_account", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.settingsService.ValidateCurrentPassword(user.PasswordHash, password); err != nil {
		spec := mapSettingsDeleteAccountPasswordError(err)
		handler.logSecurityError(c, "settings.delete_account", spec)
		return handler.respondMappedError(c, spec)
	}

	if err := handler.settingsService.DeleteAccount(user.ID); err != nil {
		spec := settingsDeleteAccountErrorSpec()
		handler.logSecurityError(c, "settings.delete_account", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.clearAuthCookie(c)
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
