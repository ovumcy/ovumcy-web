package api

import (
	"strconv"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) Register(c *fiber.Ctx) error {
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

	if err := handler.setAuthCookie(c, &user, true); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.register", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.register", "success")
	return handler.renderRegisterInlineRecoveryResponse(c, &user, recoveryCode, fiber.StatusCreated)
}

func (handler *Handler) Login(c *fiber.Ctx) error {
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
	if err := handler.setAuthCookie(c, &user, credentials.RememberMe); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.login", spec)
		return handler.respondMappedError(c, spec)
	}

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
		handler.clearAuthCookie(c)
		handler.clearRecoveryCodePageCookie(c)
		handler.clearResetPasswordCookie(c)
		spec := authSessionRevokeErrorSpec()
		handler.logSecurityError(c, "auth.logout", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.clearAuthCookie(c)
	handler.clearRecoveryCodePageCookie(c)
	handler.clearResetPasswordCookie(c)
	handler.logSecurityEvent(c, "auth.logout", "success")
	if isHTMX(c) {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusOK)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/login", fiber.StatusSeeOther)
}
