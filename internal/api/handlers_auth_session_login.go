package api

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) Register(c *fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		spec := authLocalSignInDisabledErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		if acceptsJSON(c) || isHTMX(c) {
			return handler.respondMappedError(c, spec)
		}
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	credentials, err := parseCredentials(c)
	if err != nil {
		spec := authInvalidInputErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondMappedError(c, spec)
	}

	user, recoveryCode, err := handler.registrationService.RegisterOwnerAccount(
		credentials.Email,
		credentials.Password,
		credentials.ConfirmPassword,
		time.Now().In(handler.location),
	)
	if err != nil {
		spec := mapAuthRegisterError(err)
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondMappedError(c, spec)
	}

	if _, err := handler.setAuthCookie(c, &user, true); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.clearOIDCLogoutTransportCookies(c)

	handler.logSecurityEvent(c, "auth.register", "success")
	return handler.renderRegisterInlineRecoveryResponse(c, &user, recoveryCode, fiber.StatusCreated)
}

func (handler *Handler) Login(c *fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		spec := authLocalSignInDisabledErrorSpec()
		handler.logSecurityError(c, "auth.login", spec)
		if acceptsJSON(c) || isHTMX(c) {
			return handler.respondMappedError(c, spec)
		}
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	credentials, err := parseCredentials(c)
	if err != nil {
		spec := authInvalidInputErrorSpec()
		handler.logSecurityError(c, "auth.login", spec)
		return handler.respondMappedError(c, spec)
	}
	result, err := handler.loginService.Authenticate(
		handler.secretKey,
		c.IP(),
		credentials.Email,
		credentials.Password,
		30*time.Minute,
		time.Now(),
	)
	if err != nil {
		spec := mapAuthLoginError(err)
		handler.logSecurityError(c, "auth.login", spec)
		return handler.respondMappedError(c, spec)
	}

	if result.RequiresPasswordReset {
		if err := handler.setResetPasswordCookie(c, result.ResetToken, true); err != nil {
			spec := authResetTokenCreateErrorSpec()
			handler.logSecurityError(c, "auth.login", spec)
			return handler.respondMappedError(c, spec)
		}
		handler.logSecurityEvent(c, "auth.login", "reset_required")
		if acceptsJSON(c) {
			return handler.respondMappedError(c, passwordChangeRequiredErrorSpec())
		}
		return redirectToPath(c, "/reset-password")
	}

	user := result.User
	if _, err := handler.setAuthCookie(c, &user, credentials.RememberMe); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.login", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.clearOIDCLogoutTransportCookies(c)

	handler.logSecurityEvent(
		c,
		"auth.login",
		"success",
		securityEventField("remember_me", strconv.FormatBool(credentials.RememberMe)),
	)
	return redirectOrJSON(c, services.PostLoginRedirectPath(&user))
}

func (handler *Handler) Logout(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.authService.RevokeAuthSessions(user.ID); err != nil {
		handler.clearAuthRelatedCookies(c)
		spec := authSessionRevokeErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		return handler.respondMappedError(c, spec)
	}

	logoutTransportPath := ""
	sessionClaims, hasSession := currentAuthSession(c)
	handler.clearAuthRelatedCookies(c)
	if hasSession && sessionClaims != nil {
		logoutState, found, err := handler.oidcLogoutStateSvc.Load(sessionClaims.SessionID, time.Now())
		if err != nil {
			handler.logSecurityEvent(c, "auth.logout", "provider_logout_state_unavailable")
		} else if found && validOIDCLogoutState(logoutState) {
			if err := handler.setOIDCLogoutBridgeCookie(c, sessionClaims.SessionID, time.Now()); err == nil {
				logoutTransportPath = oidcLogoutBridgePath
			}
		}
	}
	handler.logSecurityEvent(c, "auth.logout", "success")
	if logoutTransportPath != "" {
		if isHTMX(c) {
			c.Set("HX-Redirect", logoutTransportPath)
			return c.SendStatus(fiber.StatusOK)
		}
		if acceptsJSON(c) {
			return c.JSON(fiber.Map{"ok": true, "redirect": logoutTransportPath})
		}
		return c.Redirect(logoutTransportPath, fiber.StatusSeeOther)
	}
	if isHTMX(c) {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusOK)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/login", fiber.StatusSeeOther)
}
