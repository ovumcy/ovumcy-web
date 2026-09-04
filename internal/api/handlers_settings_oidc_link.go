package api

import (
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
	c.Type("html", "utf-8")
	return c.SendString(oidcSameOriginRedirectInterstitial(authURL))
}

// completeOIDCIdentityLinkStepup is dispatched from CompleteOIDCLogin when the
// callback carries a step-up cookie whose purpose is identity_link. It proves
// the account that started the flow is still the one that owns this session,
// then hands the exchange straight to CompleteIdentityLinkReauth, which
// verifies freshness and persists the link via ConfirmAndLinkIdentity — the
// same service method the (now closed) public link-confirm route used to
// call, and the same one the operator CLI command calls for the no-session
// recovery case.
func (handler *Handler) completeOIDCIdentityLinkStepup(c fiber.Ctx, state oidcStepupState) error {
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

	callbackState := handler.oidcCallbackValue(c, "state")
	code := handler.oidcCallbackValue(c, "code")
	if !state.matchesState(callbackState) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, oidcIdentityLinkStepupAction, spec)
		return handler.redirectSettingsRefusal(c, spec)
	}
	if handler.oidcCallbackValue(c, "error") != "" {
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
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "oidc_identity_linked"})
	return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
}
