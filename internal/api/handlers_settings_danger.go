package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
	if err := handler.settingsService.ClearAllData(c.UserContext(), user.ID); err != nil {
		spec := settingsClearDataErrorSpec()
		handler.logSecurityError(c, "settings.clear_data", spec)
		return handler.respondMappedError(c, spec)
	}

	// ClearAllDataAndResetSettings bumps auth_session_version atomically;
	// mirror the bump in memory and re-issue the auth cookie so this device
	// stays signed in while every other session that existed before the
	// wipe is invalidated on its next request. Matches the contract used by
	// password change, recovery-code regen, and 2FA toggle.
	user.AuthSessionVersion = services.NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	if err := handler.refreshCurrentSession(c, user, "settings.clear_data"); err != nil {
		return err
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

	if err := handler.settingsService.DeleteAccount(c.UserContext(), user.ID); err != nil {
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
