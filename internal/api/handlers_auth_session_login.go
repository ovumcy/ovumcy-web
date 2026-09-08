package api

import (
	"errors"
	"strconv"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) Register(c fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		spec := authLocalSignInDisabledErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		if acceptsJSON(c) || isHTMX(c) {
			return handler.respondMappedError(c, spec)
		}
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
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
		c.Context(),
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

func (handler *Handler) Login(c fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		spec := authLocalSignInDisabledErrorSpec()
		handler.logSecurityError(c, "auth.login", spec)
		if acceptsJSON(c) || isHTMX(c) {
			return handler.respondMappedError(c, spec)
		}
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}
	credentials, err := parseCredentials(c)
	if err != nil {
		spec := authInvalidInputErrorSpec()
		handler.logSecurityError(c, "auth.login", spec)
		return handler.respondMappedError(c, spec)
	}
	result, err := handler.loginService.Authenticate(
		c.Context(),
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
		if err := handler.setResetPasswordCookie(c, result.ResetToken); err != nil {
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
		if err := handler.setTOTPPendingCookie(c, result.User.ID, credentials.RememberMe, ""); err != nil {
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

func (handler *Handler) Logout(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		spec := unauthorizedErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		return handler.respondMappedError(c, spec)
	}
	sessionClaims, hasSession := currentAuthSession(c)
	// The revoke comes first and the budget check second: a sign-out the owner
	// asked for must end the session even when the budget is spent, or an
	// exhausted budget would keep a session alive on a device the owner is
	// leaving. What the refusal below still withholds is the provider sign-out
	// bridge and the success answer — the cookies are already gone.
	if err := handler.authService.RevokeAuthSessions(c.Context(), user.ID); err != nil {
		// codecov:ignore:start -- the revoke fails only on a storage error, which no
		// request-shaped input can provoke. The branch stays in step with the
		// success path below: the auth cookie goes either way, so the session end is
		// just as deliberate and the language cache goes with it.
		handler.clearSessionEndCookies(c)
		spec := authSessionRevokeErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		return handler.respondMappedError(c, spec)
		// codecov:ignore:end
	}
	handler.clearSessionEndCookies(c)
	// The session has ended by this line on every branch below, so the audit
	// record of it is written here rather than on the success answer alone: a
	// refused sign-out still terminated a session and must not read as a bare 429.
	handler.logSecurityEvent(c, "auth.logout", "success")
	if handler.authService.CheckAndRecordLogoutAttempt(
		handler.secretKey,
		c.IP(),
		services.LogoutAttemptIdentity(user.ID),
		time.Now(),
	) {
		spec := tooManyLogoutAttemptsErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		if acceptsJSON(c) || isHTMX(c) {
			return handler.respondMappedError(c, spec)
		}
		// The session is already gone, so a browser lands where a signed-out
		// browser belongs, with the refusal as a flash, not on a JSON body.
		handler.setFlashCookie(c, FlashPayload{AuthError: spec.Key})
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	logoutTransportPath := ""
	if hasSession && sessionClaims != nil && handler.oidcLogoutStateSvc != nil {
		logoutState, found, err := handler.oidcLogoutStateSvc.Load(c.Context(), sessionClaims.SessionID, sessionClaims.UserID, time.Now())
		if err != nil {
			handler.logSecurityEvent(c, "auth.logout", "provider_logout_state_unavailable")
		} else if found && validOIDCLogoutState(logoutState) {
			if err := handler.setOIDCLogoutBridgeCookie(c, sessionClaims.SessionID, sessionClaims.UserID, time.Now()); err == nil {
				logoutTransportPath = oidcLogoutBridgePath
			}
		}
	}
	if logoutTransportPath != "" {
		if isHTMX(c) {
			c.Set("HX-Redirect", logoutTransportPath)
			return c.SendStatus(fiber.StatusOK)
		}
		if acceptsJSON(c) {
			return c.JSON(fiber.Map{"ok": true, "redirect": logoutTransportPath})
		}
		return c.Redirect().Status(fiber.StatusSeeOther).To(logoutTransportPath)
	}
	if isHTMX(c) {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusOK)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
}
