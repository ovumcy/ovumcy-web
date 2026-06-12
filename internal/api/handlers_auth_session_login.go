package api

import (
	"errors"
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
	if !services.ParseBoolLike(credentials.Consent) {
		spec := authConsentRequiredErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondAuthError(c, spec)
	}

	// Cookie-less register: do not issue ovumcy_auth or ovumcy_recovery_code
	// directly. Instead, build a sealed pickup cookie whose ciphertext shape
	// is identical for new-email success and duplicate-email collision, and
	// redirect to GET /register/welcome. That endpoint dispatches to either
	// the inline recovery surface (real pickup) or /login (decoy / expired).
	// See SECURITY.md "Register enumeration" for the residual two-step oracle.
	now := time.Now().In(handler.location)
	user, recoveryCode, err := handler.registrationService.RegisterOwnerAccount(
		c.UserContext(),
		credentials.Email,
		credentials.Password,
		credentials.ConfirmPassword,
		now,
	)
	if err != nil {
		if errors.Is(err, services.ErrAuthEmailExists) {
			handler.logSecurityEvent(c, "auth.register", "duplicate_silenced")
			return handler.respondRegisterPickup(c, registerPickupOutcomeDecoy(now))
		}
		spec := mapAuthRegisterError(err)
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.register", "success")
	return handler.respondRegisterPickup(c, registerPickupOutcomeReal(now, user.ID, recoveryCode))
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
		c.UserContext(),
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

	if result.RequiresTOTP {
		if err := handler.setTOTPPendingCookie(c, result.User.ID, credentials.RememberMe); err != nil {
			spec := authSessionCreateErrorSpec()
			handler.logSecurityError(c, "auth.login", spec)
			return handler.respondMappedError(c, spec)
		}
		handler.logSecurityEvent(c, "auth.login", "totp_required")
		if acceptsJSON(c) {
			return c.Status(fiber.StatusOK).JSON(fiber.Map{"requires_totp": true})
		}
		return redirectToPath(c, "/auth/2fa")
	}

	user := result.User
	if _, err := handler.setAuthCookie(c, &user, credentials.RememberMe); err != nil {
		spec := authSessionCreateErrorSpec()
		if errors.Is(err, services.ErrAuthUnsupportedRole) {
			spec = authWebSignInUnavailableErrorSpec()
		}
		handler.logSecurityError(c, "auth.login", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.clearOIDCLogoutBridgeCookie(c)

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
	if handler.authService.CheckAndRecordLogoutAttempt(
		[]byte(handler.secretKey),
		c.IP(),
		strconv.FormatUint(uint64(user.ID), 10),
		time.Now(),
	) {
		spec := tooManyLogoutAttemptsErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.authService.RevokeAuthSessions(c.UserContext(), user.ID); err != nil {
		handler.clearAuthRelatedCookies(c)
		spec := authSessionRevokeErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		return handler.respondMappedError(c, spec)
	}

	logoutTransportPath := ""
	sessionClaims, hasSession := currentAuthSession(c)
	handler.clearAuthRelatedCookies(c)
	if hasSession && sessionClaims != nil {
		logoutState, found, err := handler.oidcLogoutStateSvc.Load(c.UserContext(), sessionClaims.SessionID, time.Now())
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
