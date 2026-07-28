package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The two erasure endpoints are the most destructive health-data mutations the
// product exposes, so they are audited through the typed mechanism like every
// other one. Neither acts on a single record: clear-data wipes the account's
// tracked data and resets its settings, delete-account removes the account with
// everything attached to it. The target therefore names that scope — a fixed
// designator that never carries an email, an id, or any free text.
var (
	clearDataMutation     = healthMutationKind{action: "settings.clear_data", target: "account_data"}
	deleteAccountMutation = healthMutationKind{action: "settings.delete_account", target: "account"}
)

// clearDataValidateAction names the password pre-check behind the clear-data
// confirmation dialog. It answers whether the password is right and mutates
// nothing, so it stays on the plain security-event path rather than claiming a
// health-data domain — but the name still lives here, not at the call site.
const clearDataValidateAction = "settings.clear_data_validate"

func (handler *Handler) ValidateClearDataPassword(c fiber.Ctx) error {
	_, spec, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, clearDataValidateAction, spec)
		return handler.respondMappedError(c, spec)
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (handler *Handler) ClearAllData(c fiber.Ctx) error {
	user, spec, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		return handler.failMutation(c, clearDataMutation, spec)
	}
	if err := handler.settingsService.ClearAllData(c.Context(), user.ID); err != nil {
		return handler.failMutation(c, clearDataMutation, settingsClearDataErrorSpec())
	}

	// ClearAllDataAndResetSettings bumps auth_session_version atomically;
	// mirror the bump in memory and re-issue the auth cookie so this device
	// stays signed in while every other session that existed before the
	// wipe is invalidated on its next request. Matches the contract used by
	// password change, recovery-code regen, and 2FA toggle.
	user.AuthSessionVersion = services.NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	// The session-refresh failures below are auth-plumbing events, not the
	// erasure itself, so they keep the plain path under the same action name.
	if err := handler.refreshCurrentSession(c, user, clearDataMutation.action); err != nil {
		return err
	}

	handler.logMutationSuccess(c, clearDataMutation)
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "data_cleared"})
	return redirectOrJSON(c, "/settings")
}

func (handler *Handler) DeleteAccount(c fiber.Ctx) error {
	user, spec, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		return handler.failMutation(c, deleteAccountMutation, spec)
	}

	if err := handler.settingsService.DeleteAccount(c.Context(), user.ID); err != nil {
		return handler.failMutation(c, deleteAccountMutation, settingsDeleteAccountErrorSpec())
	}

	handler.clearAuthRelatedCookies(c)
	handler.logMutationSuccess(c, deleteAccountMutation)
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return redirectOrJSON(c, "/login")
}

func parsePasswordProtectedSettingsAction(c fiber.Ctx) (string, APIErrorSpec, bool) {
	input := passwordProtectedSettingsInput{}
	if err := c.Bind().Body(&input); err != nil && hasJSONBody(c) {
		spec := settingsMissingPasswordErrorSpec()
		return "", spec, false
	}
	if input.Password == "" {
		spec := settingsMissingPasswordErrorSpec()
		return "", spec, false
	}
	return input.Password, APIErrorSpec{}, true
}

func (handler *Handler) validateSettingsActionPassword(c fiber.Ctx) (*models.User, APIErrorSpec, bool) {
	user, ok := currentUser(c)
	if !ok {
		return nil, unauthorizedErrorSpec(), false
	}

	password, spec, valid := parsePasswordProtectedSettingsAction(c)
	if !valid {
		return nil, spec, false
	}
	// Budgeted re-auth: the erasure gate is a password check reachable with a
	// session already in hand, so it must not be a faster oracle than the login
	// form. VerifyReauthPassword refuses even a correct password once the budget
	// is spent.
	attempt := services.ReauthAttempt{ClientKey: c.IP(), UserID: user.ID, Now: time.Now()}
	if err := handler.settingsService.VerifyReauthPassword(attempt, user.PasswordHash, password); err != nil {
		return nil, mapSettingsDeleteAccountPasswordError(err), false
	}

	return user, APIErrorSpec{}, true
}
