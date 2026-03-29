package api

import (
	"context"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const oidcExternalRequestTimeout = 10 * time.Second

func (handler *Handler) StartOIDCLogin(c *fiber.Ctx) error {
	state, err := newOIDCAuthState(time.Now())
	if err != nil {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_start", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	if err := handler.setOIDCStateCookie(c, state); err != nil {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_start", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	ctx, cancel := oidcRequestContext(c)
	defer cancel()

	authURL, err := handler.oidcService.StartAuth(ctx, state.State, state.Nonce, state.CodeVerifier)
	if err != nil {
		handler.clearOIDCStateCookie(c)
		spec := mapAuthOIDCError(err)
		handler.logSecurityError(c, "auth.oidc_start", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	handler.logSecurityEvent(c, "auth.oidc_start", "success")
	return c.Redirect(authURL, fiber.StatusTemporaryRedirect)
}

func (handler *Handler) CompleteOIDCLogin(c *fiber.Ctx) error {
	oidcState := handler.popOIDCStateCookie(c)
	callbackState := c.FormValue("state")
	code := c.FormValue("code")
	if !oidcState.validAt(time.Now()) || !oidcState.matchesState(callbackState) {
		spec := authOIDCAuthenticationFailedErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	if c.FormValue("error") != "" {
		spec := authOIDCUnavailableErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	ctx, cancel := oidcRequestContext(c)
	defer cancel()

	result, err := handler.oidcService.Authenticate(ctx, code, oidcState.CodeVerifier, oidcState.Nonce, time.Now())
	if err != nil {
		spec := mapAuthOIDCError(err)
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	if result.User.MustChangePassword {
		token, issueErr := handler.passwordResetSvc.IssueResetTokenForUser(handler.secretKey, &result.User, 30*time.Minute, time.Now())
		if issueErr != nil {
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_callback", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect("/login", fiber.StatusSeeOther)
		}
		if err := handler.setResetPasswordCookie(c, token, true); err != nil {
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_callback", spec)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect("/login", fiber.StatusSeeOther)
		}
		handler.logSecurityEvent(c, "auth.oidc_callback", "reset_required")
		return c.Redirect("/reset-password", fiber.StatusSeeOther)
	}

	sessionID, err := handler.setAuthCookie(c, &result.User, false)
	if err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.oidc_callback", spec)
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	handler.clearOIDCLogoutBridgeCookie(c)
	if result.Logout != nil {
		if err := handler.oidcLogoutStateSvc.Save(sessionID, *result.Logout, time.Now()); err != nil {
			spec := authSessionCreateErrorSpec()
			handler.logSecurityError(c, "auth.oidc_callback", spec)
			handler.clearAuthRelatedCookies(c)
			handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
			return c.Redirect("/login", fiber.StatusSeeOther)
		}
	} else {
		_ = handler.oidcLogoutStateSvc.Delete(sessionID)
		handler.clearOIDCLogoutTransportCookies(c)
	}

	handler.logSecurityEvent(
		c,
		"auth.oidc_callback",
		"success",
		securityEventField("newly_linked", boolString(result.NewlyLinked)),
	)
	return c.Redirect(services.PostLoginRedirectPath(&result.User), fiber.StatusSeeOther)
}

func oidcRequestContext(c *fiber.Ctx) (context.Context, context.CancelFunc) {
	base := c.UserContext()
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

func (handler *Handler) ShowOIDCLogoutBridge(c *fiber.Ctx) error {
	if !handler.readOIDCLogoutBridgeCookie(c, time.Now()).validAt(time.Now()) {
		handler.clearOIDCLogoutBridgeCookie(c)
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	c.Type("html", "utf-8")
	return c.SendString(`<!doctype html><html><head><meta charset="utf-8"><meta http-equiv="refresh" content="0; url=` + oidcLogoutBridgeRedirectPath + `"></head><body></body></html>`)
}

func (handler *Handler) RedirectOIDCLogout(c *fiber.Ctx) error {
	bridgePayload := handler.readOIDCLogoutBridgeCookie(c, time.Now())
	handler.clearOIDCLogoutBridgeCookie(c)
	if !bridgePayload.validAt(time.Now()) {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	logoutState, found, err := handler.oidcLogoutStateSvc.Consume(bridgePayload.SessionID, time.Now())
	if err != nil || !found {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	providerLogoutURL := handler.providerLogoutRedirectURLFromState(logoutState)
	if providerLogoutURL == "" {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	return c.Redirect(providerLogoutURL, fiber.StatusSeeOther)
}
