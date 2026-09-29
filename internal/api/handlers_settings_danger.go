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
	_, spec, cause, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, clearDataValidateAction, spec, cause)
		return handler.respondMappedError(c, spec)
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (handler *Handler) ClearAllData(c fiber.Ctx) error {
	user, spec, cause, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		return handler.failMutation(c, clearDataMutation, spec, cause)
	}
	// The wipe itself lives in applyClearData, shared with the OIDC step-up
	// callback, so the session-version bump has exactly one implementation.
	spec, outcome := handler.applyClearData(c, user)
	switch outcome {
	case clearDataRefusedSignedOut:
		return handler.respondSignedOutRefusal(c, spec)
	case clearDataRefused:
		return handler.respondMappedError(c, spec)
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "data_cleared"})
	return redirectOrJSON(c, "/settings")
}

func (handler *Handler) DeleteAccount(c fiber.Ctx) error {
	user, spec, cause, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		return handler.failMutation(c, deleteAccountMutation, spec, cause)
	}

	// Shared with the OIDC step-up callback; see applyClearData above.
	if spec, applied := handler.applyDeleteAccount(c, user); !applied {
		return handler.respondMappedError(c, spec)
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return redirectOrJSON(c, "/login")
}

func parsePasswordProtectedSettingsAction(c fiber.Ctx) (string, APIErrorSpec, bool) {
	input := passwordProtectedSettingsInput{}
	// A body the binder rejected is refused whole, whatever its type: a decoder
	// may have filled the password before it stopped, and a password taken from
	// half a body is not one the client sent.
	if err := bindRequestBody(c, &input); err != nil {
		spec := settingsMissingPasswordErrorSpec()
		return "", spec, false
	}
	if input.Password == "" {
		spec := settingsMissingPasswordErrorSpec()
		return "", spec, false
	}
	return input.Password, APIErrorSpec{}, true
}

// validateSettingsActionPassword returns, alongside the mapped spec, a
// SecurityEventField naming the underlying VerifyReauthPassword cause (WEB-54:
// the mapped spec no longer distinguishes "no local password" from "wrong
// password", but the caller should still log which one happened). The field
// is the zero value — silently dropped by emitSecurityEvent — on every other
// refusal (missing password, rate limited) and on success.
func (handler *Handler) validateSettingsActionPassword(c fiber.Ctx) (*models.User, APIErrorSpec, SecurityEventField, bool) {
	user, ok := currentUser(c)
	if !ok {
		// codecov:ignore:start -- every caller hangs off the usersCurrent group,
		// which carries AuthRequired, so a request reaching this helper always
		// has a resolved session. Kept for the same reason the OIDC identity-link
		// step-up's own duplicate check does: the helper must stay safe if it is
		// ever called from somewhere that is not behind AuthRequired.
		return nil, unauthorizedErrorSpec(), SecurityEventField{}, false
		// codecov:ignore:end
	}

	password, spec, valid := parsePasswordProtectedSettingsAction(c)
	if !valid {
		return nil, spec, SecurityEventField{}, false
	}
	// Budgeted re-auth: the erasure gate is a password check reachable with a
	// session already in hand, so it must not be a faster oracle than the login
	// form. VerifyReauthPassword refuses even a correct password once the budget
	// is spent.
	attempt := services.ReauthAttempt{ClientKey: c.IP(), UserID: user.ID, Now: time.Now()}
	if err := handler.settingsService.VerifyReauthPassword(attempt, user, password); err != nil {
		return nil, mapSettingsDeleteAccountPasswordError(err), settingsReauthCauseField(err), false
	}

	return user, APIErrorSpec{}, SecurityEventField{}, true
}
