package api

import (
	"context"
	"errors"
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
		return handler.completeLocalPasswordSetupReauth(c, stepupState)
	}

	oidcState := handler.popOIDCStateCookie(c)
	callbackState := c.FormValue("state")
	code := c.FormValue("code")
	if !oidcState.validAt(time.Now()) || !oidcState.matchesState(callbackState) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	if c.FormValue("error") != "" {
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
			handler.clearAuthRelatedCookies(c)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
		}
	} else {
		_ = handler.oidcLogoutStateSvc.Delete(c.Context(), sessionID)
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

	logoutState, found, err := handler.oidcLogoutStateSvc.Consume(c.Context(), bridgePayload.SessionID, time.Now())
	if err != nil || !found {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	providerLogoutURL := handler.providerLogoutRedirectURLFromState(logoutState)
	if providerLogoutURL == "" {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To(providerLogoutURL)
}
