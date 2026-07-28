package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Erasure on an account that has no local password.
//
// Erasing health data costs a fresh re-authentication — that requirement is not
// relaxed here. What changes is which credential can satisfy it: an owner
// provisioned through OIDC has no local password to confirm with, so the same
// step-up primitive that gates local-password enrollment stands in for the
// password prompt. An account that HAS a local password never reaches this
// flow; its gate stays the password, and the refusal below is what keeps that
// true. Without it a hijacked session on a password-holding account with a
// linked identity could erase everything without ever knowing the password —
// the SSO path would become a way around the password gate rather than a
// substitute for owners who have none.
const (
	clearDataStepupAction     = "settings.clear_data.step_up"
	deleteAccountStepupAction = "settings.delete_account.step_up"
)

// erasureStepupFlow binds one erasure operation to the audit identities it logs
// under, so neither the start nor the completion handler re-derives which
// mutation it is performing from anything except the sealed state.
type erasureStepupFlow struct {
	kind         healthMutationKind
	stepupAction string
}

func erasureStepupFlowFor(operation oidcStepupErasureOperation) (erasureStepupFlow, bool) {
	switch operation {
	case oidcStepupErasureClearData:
		return erasureStepupFlow{kind: clearDataMutation, stepupAction: clearDataStepupAction}, true
	case oidcStepupErasureDeleteAccount:
		return erasureStepupFlow{kind: deleteAccountMutation, stepupAction: deleteAccountStepupAction}, true
	default:
		return erasureStepupFlow{}, false
	}
}

// StartClearDataStepupReauth begins the step-up that authorizes a data wipe.
func (handler *Handler) StartClearDataStepupReauth(c fiber.Ctx) error {
	return handler.startErasureStepupReauth(c, oidcStepupErasureClearData)
}

// StartDeleteAccountStepupReauth begins the step-up that authorizes an account
// deletion.
func (handler *Handler) StartDeleteAccountStepupReauth(c fiber.Ctx) error {
	return handler.startErasureStepupReauth(c, oidcStepupErasureDeleteAccount)
}

func (handler *Handler) startErasureStepupReauth(c fiber.Ctx, operation oidcStepupErasureOperation) error {
	flow, known := erasureStepupFlowFor(operation)
	if !known {
		// codecov:ignore:start -- both exported entry points pass a known
		// operation; this guards a future third one added without a flow.
		spec := settingsInvalidInputErrorSpec()
		return handler.respondMappedError(c, spec)
		// codecov:ignore:end
	}

	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		return handler.respondMappedError(c, spec)
	}
	// The no-downgrade gate. See the file comment: an account with a local
	// password confirms an erasure with that password, never with SSO.
	if user.LocalAuthEnabled {
		spec := settingsInvalidInputErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		return handler.respondMappedError(c, spec)
	}
	if handler.oidcService == nil || !handler.oidcService.Enabled() {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		return handler.respondMappedError(c, spec)
	}

	state, err := newOIDCErasureStepupState(time.Now(), user.ID, operation)
	if err != nil {
		// codecov:ignore:start -- defensive: state minting fails only on a crypto/rand error
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		return handler.respondMappedError(c, spec)
		// codecov:ignore:end
	}
	if err := handler.setOIDCStepupCookie(c, state); err != nil {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		return handler.respondMappedError(c, spec)
	}
	// Drop any in-flight ordinary login state, exactly as the local-password
	// step-up does: two competing flows for one user are confusion at best.
	handler.clearOIDCStateCookie(c)

	ctx, cancel := oidcRequestContext(c)
	defer cancel()

	authURL, err := handler.oidcService.StartReauth(ctx, state.State, state.Nonce, state.CodeVerifier)
	if err != nil {
		handler.clearOIDCStepupCookie(c)
		spec := mapAuthOIDCError(err)
		handler.logSecurityError(c, flow.stepupAction, spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, flow.stepupAction, "redirect_issued")
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "redirect_url": authURL})
	}
	// Same reason as the local-password step-up: the settings page CSP pins
	// form-action to 'self' across the whole redirect chain, so a form submit
	// cannot 303 straight to the provider. Hand back a same-origin interstitial.
	c.Type("html", "utf-8")
	return c.SendString(oidcSameOriginRedirectInterstitial(authURL))
}

// completeErasureStepupReauth is dispatched from CompleteOIDCLogin when the
// callback carries a step-up cookie whose purpose is erasure. It executes the
// operation the owner confirmed BEFORE leaving for the provider: the operation
// rides in the sealed payload, never in the callback request, which arrives
// from the provider carrying no body of its own.
func (handler *Handler) completeErasureStepupReauth(c fiber.Ctx, state oidcStepupState) error {
	flow, known := erasureStepupFlowFor(state.Operation)
	if state.Purpose != oidcStepupPurposeErasure || !known {
		// codecov:ignore:start -- validAt already refused a payload whose purpose
		// and operation do not agree; this is the dispatch-side half of that check.
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
		// codecov:ignore:end
	}

	// /auth/oidc/callback runs without AuthRequired (ordinary login has to work
	// for unauthenticated visitors), so resolve the session here and require it
	// to be the account the flow was started for.
	user, err := handler.authenticateRequest(c)
	if err != nil || user == nil || user.ID != state.UserID {
		spec := settingsOIDCReauthMismatchErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
	// A local password enrolled between start and callback moves this account
	// back onto the password gate, so the step-up no longer authorizes anything.
	if user.LocalAuthEnabled {
		spec := settingsInvalidInputErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}

	callbackState := handler.oidcCallbackValue(c, "state")
	code := handler.oidcCallbackValue(c, "code")
	if !state.matchesState(callbackState) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
	if handler.oidcCallbackValue(c, "error") != "" {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, flow.stepupAction, spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}

	ctx, cancel := oidcRequestContext(c)
	defer cancel()
	if err := handler.validateLocalPasswordSetupReauth(ctx, code, state.CodeVerifier, state.Nonce, user.ID, time.Now()); err != nil {
		spec := mapLocalPasswordSetupReauthError(err)
		handler.logSecurityError(c, flow.stepupAction, spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}

	handler.logSecurityEvent(c, flow.stepupAction, "success")
	switch state.Operation {
	case oidcStepupErasureClearData:
		if spec, ok := handler.applyClearData(c, user); !ok {
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
		}
		handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "data_cleared"})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	default:
		if spec, ok := handler.applyDeleteAccount(c, user); !ok {
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
}

// applyClearData performs the wipe and re-issues this device's session. It is
// the single implementation shared by the password-gated handler and the
// step-up callback: the auth_session_version bump below has to happen exactly
// once and identically on both paths, and two copies of it would drift.
func (handler *Handler) applyClearData(c fiber.Ctx, user *models.User) (APIErrorSpec, bool) {
	if err := handler.settingsService.ClearAllData(c.Context(), user.ID); err != nil {
		spec := settingsClearDataErrorSpec()
		handler.logMutationError(c, clearDataMutation, spec)
		return spec, false
	}

	// ClearAllDataAndResetSettings bumps auth_session_version atomically;
	// mirror the bump in memory and re-issue the auth cookie so this device
	// stays signed in while every other session that existed before the wipe is
	// invalidated on its next request. Matches the contract used by password
	// change, recovery-code regen, and 2FA toggle.
	user.AuthSessionVersion = services.NormalizeAuthSessionVersion(user.AuthSessionVersion) + 1
	// The session-refresh failures are auth-plumbing events, not the erasure
	// itself, so they keep the plain path under the same action name.
	if err := handler.refreshCurrentSession(c, user, clearDataMutation.action); err != nil {
		return authSessionCreateErrorSpec(), false
	}

	handler.logMutationSuccess(c, clearDataMutation)
	return APIErrorSpec{}, true
}

// applyDeleteAccount removes the account and retracts this device's session.
// Shared by the password-gated handler and the step-up callback for the same
// reason as applyClearData.
func (handler *Handler) applyDeleteAccount(c fiber.Ctx, user *models.User) (APIErrorSpec, bool) {
	if err := handler.settingsService.DeleteAccount(c.Context(), user.ID); err != nil {
		spec := settingsDeleteAccountErrorSpec()
		handler.logMutationError(c, deleteAccountMutation, spec)
		return spec, false
	}

	handler.clearAuthRelatedCookies(c)
	handler.logMutationSuccess(c, deleteAccountMutation)
	return APIErrorSpec{}, true
}
