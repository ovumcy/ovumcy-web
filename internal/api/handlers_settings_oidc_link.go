package api

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Linking a NEW OIDC identity to the currently authenticated account
// (issue #701).
//
// Linking is a permanent (issuer, subject) -> account binding, the same
// weight as a password change, so it is authorised the same way every other
// step-up in this file is: a fresh interactive re-authentication at the
// provider (prompt=login, max_age=0), never a form on a page reachable
// without a session. The public /auth/oidc/link-confirm route that used to
// authorise this with a password alone, on an unauthenticated page, stays
// closed (handlers_auth_oidc_link_confirm.go) — this is the replacement, and
// the only other way in is the operator CLI's `link-oidc-identity` command
// for the no-session recovery case (internal/cli).
const oidcIdentityLinkStepupAction = "settings.oidc_identity_link.step_up"

// StartOIDCIdentityLinkStepup begins the step-up that authorises linking a new
// OIDC identity to the current account. It mints a sealed step-up cookie
// carrying this account's id and hands back a redirect (or same-origin
// interstitial for a plain form submit) to the provider's authorize endpoint.
// Nothing is written to the database here — ConfirmAndLinkIdentity only runs
// once the callback proves a fresh authentication.
func (handler *Handler) StartOIDCIdentityLinkStepup(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		// codecov:ignore:start -- this route hangs off the usersCurrent group,
		// which carries AuthRequired, so a request reaching this handler always
		// has a resolved session. Kept for the same reason the sibling step-ups
		// keep it: the handler must stay safe if it is ever mounted elsewhere.
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.respondMappedError(c, spec)
		// codecov:ignore:end
	}
	if handler.oidcService == nil || !handler.oidcService.Enabled() {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.respondMappedError(c, spec)
	}
	// Fresh proof of the ACCOUNT, not only of the provider account being
	// linked: the provider re-authentication below proves whoever holds this
	// session controls the identity they are about to bind — which an attacker
	// with a hijacked session does, for their own provider account. The
	// current local password (budgeted, like every re-auth) is what proves the
	// account holder is present. An account without one is refused here and
	// sets a local password first.
	if _, spec, valid := handler.validateSettingsActionPassword(c); !valid {
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.respondMappedError(c, spec)
	}

	state, err := newOIDCIdentityLinkStepupState(time.Now(), user.ID)
	if err != nil {
		// codecov:ignore:start -- defensive: state minting fails only on a crypto/rand error
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.respondMappedError(c, spec)
		// codecov:ignore:end
	}
	if err := handler.setOIDCStepupCookie(c, state); err != nil {
		// codecov:ignore:start -- the setter's only failure is a non-secure
		// cookie posture, and boot refuses OIDC_ENABLED=true without
		// COOKIE_SECURE=true, so an instance that can reach this line cannot be
		// configured to fail it.
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.respondMappedError(c, spec)
		// codecov:ignore:end
	}
	// Drop any in-flight ordinary login state, exactly as the other step-ups
	// do: two competing flows for one user are confusion at best.
	handler.clearOIDCStateCookie(c)

	ctx, cancel := oidcRequestContext(c)
	defer cancel()

	authURL, err := handler.oidcService.StartReauth(ctx, state.State, state.Nonce, state.CodeVerifier)
	if err != nil {
		handler.clearOIDCStepupCookie(c)
		spec := mapAuthOIDCError(err)
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, oidcIdentityLinkStepupAction, "redirect_issued")
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "redirect_url": authURL})
	}
	// Same reason as the sibling step-ups: the settings page CSP pins
	// form-action to 'self' across the whole redirect chain, so a form submit
	// cannot 303 straight to the provider. Hand back a same-origin interstitial.
	return respondOIDCSameOriginHandoff(c, authURL)
}

// completeOIDCIdentityLinkStepup is dispatched from CompleteOIDCLogin when the
// callback carries a step-up cookie whose purpose is identity_link. It proves
// the account that started the flow is still the one that owns this session,
// then hands the exchange straight to CompleteIdentityLinkReauth, which
// verifies freshness and persists the link via ConfirmAndLinkIdentity — the
// same service method the (now closed) public link-confirm route used to
// call, and the same one the operator CLI command calls for the no-session
// recovery case.
func (handler *Handler) completeOIDCIdentityLinkStepup(c fiber.Ctx, state oidcStepupState, exchange oidcCallbackExchange) error {
	if state.Purpose != oidcStepupPurposeIdentityLink {
		// codecov:ignore:start -- forward-compat guard: validAt already refused a
		// payload whose purpose does not match its own shape, so a mismatching
		// sealed payload cannot be minted and dispatched here.
		spec := authOIDCAuthenticationFailedErrorSpec()
		return handler.redirectSettingsRefusal(c, spec)
		// codecov:ignore:end
	}

	// /auth/oidc/callback runs without AuthRequired (ordinary login has to work
	// for unauthenticated visitors), so resolve the session here and require it
	// to be the same account that started the step-up.
	user, err := handler.authenticateRequest(c)
	if err != nil || user == nil || user.ID != state.UserID {
		spec := settingsOIDCReauthMismatchErrorSpec()
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.redirectSettingsRefusal(c, spec)
	}

	// The callback state is matched at the dispatch seam every completion
	// passes through (dispatchStepupCompletion), not here: a copy per purpose
	// fixed the class at N of N+1, because the next purpose inherits nothing.
	code := exchange.Code
	if exchange.Error != "" {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.redirectSettingsRefusal(c, spec)
	}

	ctx, cancel := oidcRequestContext(c)
	defer cancel()
	if err := handler.oidcService.CompleteIdentityLinkReauth(ctx, code, state.CodeVerifier, state.Nonce, user.ID, stepupReauthMaxAge, time.Now()); err != nil {
		spec := mapOIDCIdentityLinkReauthError(err)
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.redirectSettingsRefusal(c, spec)
	}

	handler.logSecurityEvent(c, oidcIdentityLinkStepupAction, "linked")
	// The link bumped AuthSessionVersion in the same write, revoking every
	// earlier session; this device keeps signing in on a re-issued one.
	if spec, ok := handler.reissueSessionAfterIdentityChange(c, user.ID, oidcIdentityLinkStepupAction); !ok {
		return handler.redirectSettingsRefusal(c, spec)
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "oidc_identity_linked"})
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}

const oidcIdentityUnlinkAction = "settings.oidc_identity_unlink"

// UnlinkOIDCIdentity removes one OIDC identity from the current account
// (DELETE /api/v1/users/current/oidc/identities/:id). It requires the current
// local password — the same budgeted re-auth every password-gated settings
// action uses — and the service refuses to remove the account's last way in.
// The delete bumps AuthSessionVersion in the same write, so every earlier
// session (including one the removed identity minted) is revoked; this device
// is re-issued a session.
func (handler *Handler) UnlinkOIDCIdentity(c fiber.Ctx) error {
	if handler.oidcService == nil || !handler.oidcService.Enabled() {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, oidcIdentityUnlinkAction, spec)
		return handler.respondMappedError(c, spec)
	}
	identityID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || identityID == 0 {
		spec := notFoundErrorSpec()
		handler.logSecurityError(c, oidcIdentityUnlinkAction, spec)
		return handler.respondMappedError(c, spec)
	}
	user, spec, valid := handler.validateSettingsActionPassword(c)
	if !valid {
		handler.logSecurityError(c, oidcIdentityUnlinkAction, spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.oidcService.UnlinkIdentity(c.Context(), *user, uint(identityID)); err != nil {
		spec := mapOIDCIdentityUnlinkError(err)
		handler.logSecurityError(c, oidcIdentityUnlinkAction, spec)
		return handler.respondMappedError(c, spec)
	}
	handler.logSecurityEvent(c, oidcIdentityUnlinkAction, "unlinked")
	if spec, ok := handler.reissueSessionAfterIdentityChange(c, user.ID, oidcIdentityUnlinkAction); !ok {
		return handler.respondMappedError(c, spec)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "oidc_identity_unlinked"})
	if isHTMX(c) {
		c.Set("HX-Redirect", "/settings")
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}

// reissueSessionAfterIdentityChange reloads the account after a link or unlink
// bumped its AuthSessionVersion and re-issues this device's session at the new
// version. It reloads rather than incrementing in memory: an already-linked
// pair is a no-op that bumps nothing, and the stored version is the only one a
// session can be checked against.
func (handler *Handler) reissueSessionAfterIdentityChange(c fiber.Ctx, userID uint, scope string) (APIErrorSpec, bool) {
	fresh, err := handler.authService.FindByID(c.Context(), userID)
	if err != nil {
		// codecov:ignore:start -- the account was resolved by this same request;
		// only a storage fault between the two reads reaches this line.
		handler.clearAuthCookie(c)
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, scope, spec)
		return spec, false
		// codecov:ignore:end
	}
	return handler.refreshCurrentSession(c, &fresh, scope)
}
