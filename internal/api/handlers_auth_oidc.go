package api

import (
	"context"
	"errors"
	"html"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const oidcExternalRequestTimeout = 10 * time.Second

func (handler *Handler) StartOIDCLogin(c fiber.Ctx) error {
	state, err := newOIDCAuthState(time.Now())
	if err != nil {
		// codecov:ignore:start -- defensive: newOIDCAuthState fails only on a crypto/rand error
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_start", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		// codecov:ignore:end
	}
	if err := handler.setOIDCStateCookie(c, state); err != nil {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_start", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	ctx, cancel := oidcRequestContext(c)
	defer cancel()

	authURL, err := handler.oidcService.StartAuth(ctx, state.State, state.Nonce, state.CodeVerifier)
	if err != nil {
		handler.clearOIDCStateCookie(c)
		spec := mapAuthOIDCError(err)
		handler.logSecurityError(c, "auth.oidc_start", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	handler.logSecurityEvent(c, "auth.oidc_start", "success")
	return c.Redirect().Status(fiber.StatusTemporaryRedirect).To(authURL)
}

func (handler *Handler) CompleteOIDCLogin(c fiber.Ctx) error {
	// Step-up re-auth (e.g. enabling local password on OIDC-only account)
	// reuses the same /auth/oidc/callback path as ordinary login but carries
	// a distinct sealed cookie identifying the purpose and the originating
	// user. Dispatching off cookie presence avoids registering a second
	// redirect URI at every provider operators have to manage.
	if stepupState := handler.popOIDCStepupCookie(c); stepupState.validAt(time.Now()) {
		// The purpose is dispatched on, never inferred: validAt has already
		// refused any payload whose purpose is unknown or whose fields do not
		// match the purpose it names, and each completion handler re-checks the
		// purpose it is written for. An unhandled purpose falls through to the
		// ordinary login path below, which finds no state cookie and refuses.
		switch stepupState.Purpose {
		case oidcStepupPurposeLocalPasswordSetup:
			return handler.completeLocalPasswordSetupReauth(c, stepupState)
		case oidcStepupPurposeErasure:
			return handler.completeErasureStepupReauth(c, stepupState)
		}
	}

	oidcState := handler.popOIDCStateCookie(c)
	callbackState := handler.oidcCallbackValue(c, "state")
	code := handler.oidcCallbackValue(c, "code")
	if !oidcState.validAt(time.Now()) || !oidcState.matchesState(callbackState) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	if handler.oidcCallbackValue(c, "error") != "" {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	ctx, cancel := oidcRequestContext(c)
	defer cancel()

	result, err := handler.oidcService.Authenticate(ctx, code, oidcState.CodeVerifier, oidcState.Nonce, time.Now())
	if errors.Is(err, services.ErrOIDCLinkRequiresConfirmation) {
		return handler.startOIDCLinkConfirmation(c, result)
	}
	if err != nil {
		spec := mapAuthOIDCError(err)
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	if result.User.MustChangePassword {
		token, issueErr := handler.passwordResetSvc.IssueResetTokenForUser(handler.secretKey, &result.User, 30*time.Minute, time.Now())
		if issueErr != nil {
			// codecov:ignore:start -- defensive: reset-token issuance fails only on an HMAC signing error
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_callback", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
			// codecov:ignore:end
		}
		if err := handler.setResetPasswordCookie(c, token, true); err != nil {
			// codecov:ignore:start -- defensive: the reset cookie setter fails only on an AEAD seal error
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_callback", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
			// codecov:ignore:end
		}
		handler.logSecurityEvent(c, "auth.oidc_callback", "reset_required")
		return c.Redirect().Status(fiber.StatusSeeOther).To("/reset-password")
	}

	sessionID, err := handler.setAuthCookie(c, &result.User, false)
	if err != nil {
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authOIDCAccountUnavailableErrorSpec()
		}
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	handler.clearOIDCLogoutBridgeCookie(c)
	if result.Logout != nil {
		if err := handler.oidcLogoutStateSvc.Save(c.Context(), sessionID, *result.Logout, time.Now()); err != nil { // codecov:ignore -- OIDC logout-state save error; covered by the e2e OIDC lanes
			spec := authSessionCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_callback", spec)
			// The session issued a few lines up is torn down again, and
			// setAuthCookie has already written `ovumcy_lang` from the account it
			// was issued for. Retracting only the sealed cookies would leave that
			// trace on the browser for a sign-in that did not happen — so this
			// teardown clears exactly what the deliberate ends clear.
			handler.clearSessionEndCookies(c)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		}
	} else {
		_ = handler.oidcLogoutStateSvc.Delete(c.Context(), sessionID, result.User.ID)
		handler.clearOIDCLogoutBridgeCookie(c)
	}

	handler.logSecurityEvent(
		c,
		"auth.oidc_callback",
		"success",
		securityEventField("newly_linked", boolString(result.NewlyLinked)),
	)
	return c.Redirect().Status(fiber.StatusSeeOther).To(services.PostLoginRedirectPath(&result.User))
}

func oidcRequestContext(c fiber.Ctx) (context.Context, context.CancelFunc) {
	base := c.Context()
	if base == nil {
		base = context.Background()
	}
	return context.WithTimeout(base, oidcExternalRequestTimeout)
}

func boolString(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

// oidcSameOriginRedirectInterstitial returns a minimal same-origin HTML
// document that bounces the browser to target via a meta-refresh. A browser
// form submission cannot 3xx-redirect straight to the cross-origin IdP: the
// page CSP pins form-action to 'self', and Chromium enforces that across the
// whole redirect chain of a form navigation, so a cross-origin hop aborts as
// net::ERR_ABORTED. Returning a same-origin 200 whose meta-refresh performs the
// hop keeps the cross-origin navigation out of the form submission — where
// form-action does not apply — the same technique the provider-logout bridge
// uses. target is server-built (config + random OIDC state), never user input,
// but is HTML-escaped as defense-in-depth for the attribute context.
func oidcSameOriginRedirectInterstitial(target string) string {
	return `<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0; url=` +
		html.EscapeString(target) + `"></head><body></body></html>`
}

func (handler *Handler) ShowOIDCLogoutBridge(c fiber.Ctx) error {
	if !handler.readOIDCLogoutBridgeCookie(c, time.Now()).validAt(time.Now()) {
		handler.clearOIDCLogoutBridgeCookie(c)
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	c.Type("html", "utf-8")
	return c.SendString(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0; url=` + oidcLogoutBridgeRedirectPath + `"></head><body></body></html>`)
}

func (handler *Handler) RedirectOIDCLogout(c fiber.Ctx) error {
	bridgePayload := handler.readOIDCLogoutBridgeCookie(c, time.Now())
	handler.clearOIDCLogoutBridgeCookie(c)
	if !bridgePayload.validAt(time.Now()) {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	// This route carries no session — it runs after the auth cookie is gone —
	// so the owner it acts for comes from the sealed bridge cookie, which names
	// it alongside the session id. The pair is what resolves the row: a payload
	// carrying one owner cannot reach another owner's end-session material, and
	// one naming no owner never got past validAt above.
	logoutState, found, err := handler.oidcLogoutStateSvc.Consume(c.Context(), bridgePayload.SessionID, bridgePayload.UserID, time.Now())
	if err != nil || !found {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	providerLogoutURL := handler.providerLogoutRedirectURLFromState(logoutState)
	if providerLogoutURL == "" {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To(providerLogoutURL)
}
