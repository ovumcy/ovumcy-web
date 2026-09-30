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
	reauth, spec, cause, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, clearDataValidateAction, spec, cause)
		return handler.respondMappedError(c, spec)
	}
	// The pre-check writes nothing, so the correct password is the whole of what
	// it authorised and the budget clears at once. The wipe itself re-asks the
	// password and clears the budget again only once it has committed.
	reauth.resetBudget()

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (handler *Handler) ClearAllData(c fiber.Ctx) error {
	reauth, spec, cause, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		return handler.failMutation(c, clearDataMutation, spec, cause)
	}
	// The wipe itself lives in applyClearData, shared with the OIDC step-up
	// callback, so the session-version bump has exactly one implementation.
	spec, outcome := handler.applyClearData(c, reauth.user)
	switch outcome {
	case clearDataRefusedSignedOut:
		return handler.respondSignedOutRefusal(c, spec)
	case clearDataRefused:
		return handler.respondMappedError(c, spec)
	}
	// Only a wipe that committed and carried this session past it clears the
	// budget: a correct password whose wipe was refused proved nothing lasting.
	reauth.resetBudget()

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "data_cleared"})
	return redirectOrJSON(c, "/settings")
}

func (handler *Handler) DeleteAccount(c fiber.Ctx) error {
	reauth, spec, cause, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		return handler.failMutation(c, deleteAccountMutation, spec, cause)
	}

	// Shared with the OIDC step-up callback; see applyClearData above.
	if spec, applied := handler.applyDeleteAccount(c, reauth.user); !applied {
		return handler.respondMappedError(c, spec)
	}
	// After the delete committed, like every other settings.reauth caller.
	reauth.resetBudget()

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

// settingsReauth is what a passed settings re-auth hands its caller: the
// session user it verified and the attempt it booked against settings.reauth.
// The check does not clear the budget. A caller that writes calls resetBudget
// once that write has committed, where its success response is decided, so a
// correct password whose write was then refused (a revocation, a storage fault,
// a failed delivery) keeps the count it found. A caller that writes nothing
// resets at once, and says so where it does.
type settingsReauth struct {
	user    *models.User
	budget  services.ReauthBudget
	attempt services.ReauthAttempt
}

func (reauth settingsReauth) resetBudget() {
	reauth.budget.Reset(reauth.attempt)
}

// validateSettingsActionPassword returns, alongside the mapped spec, a
// SecurityEventField naming the underlying VerifyReauthPassword cause (WEB-54:
// the mapped spec no longer distinguishes "no local password" from "wrong
// password", but the caller should still log which one happened). The field
// is the zero value — silently dropped by emitSecurityEvent — on every other
// refusal (missing password, rate limited) and on success. On success it
// returns the settingsReauth whose resetBudget the caller owes (see above).
func (handler *Handler) validateSettingsActionPassword(c fiber.Ctx) (settingsReauth, APIErrorSpec, SecurityEventField, bool) {
	user, ok := currentUser(c)
	if !ok {
		// codecov:ignore:start -- every caller hangs off the usersCurrent group,
		// which carries AuthRequired, so a request reaching this helper always
		// has a resolved session. Kept for the same reason the OIDC identity-link
		// step-up's own duplicate check does: the helper must stay safe if it is
		// ever called from somewhere that is not behind AuthRequired.
		return settingsReauth{}, unauthorizedErrorSpec(), SecurityEventField{}, false
		// codecov:ignore:end
	}

	password, spec, valid := parsePasswordProtectedSettingsAction(c)
	if !valid {
		return settingsReauth{}, spec, SecurityEventField{}, false
	}
	// Budgeted re-auth: the erasure gate is a password check reachable with a
	// session already in hand, so it must not be a faster oracle than the login
	// form. VerifyReauthPassword refuses even a correct password once the budget
	// is spent, and never clears it: that is the caller's step, after its write.
	attempt := services.ReauthAttempt{ClientKey: c.IP(), UserID: user.ID, Now: time.Now()}
	if err := handler.settingsService.VerifyReauthPassword(attempt, user, password); err != nil {
		return settingsReauth{}, mapSettingsDeleteAccountPasswordError(err), settingsReauthCauseField(err), false
	}

	return settingsReauth{user: user, budget: handler.settingsService.SettingsReauthBudget(), attempt: attempt}, APIErrorSpec{}, SecurityEventField{}, true
}
