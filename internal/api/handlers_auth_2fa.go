package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// ShowTOTPChallengePage renders the 2FA code entry page after a successful
// password login when the user has TOTP enabled.
func (handler *Handler) ShowTOTPChallengePage(c *fiber.Ctx) error {
	_, _, err := handler.parseTOTPPendingCookie(c)
	if err != nil {
		// No valid pending cookie — send back to login.
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	flash := handler.popFlashCookie(c)
	messages := currentMessages(c)
	data := fiber.Map{
		"Title":    localizedPageTitle(messages, "auth.2fa.title", "Ovumcy | Two-Factor Authentication"),
		"ErrorKey": flash.AuthError,
	}
	return handler.render(c, "auth_2fa", data)
}

// VerifyTOTPLogin validates the 6-digit TOTP code submitted on the challenge page.
// On success it issues the auth session cookie and redirects to the dashboard.
func (handler *Handler) VerifyTOTPLogin(c *fiber.Ctx) error {
	userID, rememberMe, err := handler.parseTOTPPendingCookie(c)
	if err != nil {
		spec := totpSessionExpiredErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	if err := handler.totpService.CheckRateLimit(handler.secretKey, c.IP(), userID, time.Now()); err != nil {
		spec := totpRateLimitedErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	code := c.FormValue("code")
	if len(code) != 6 {
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	user, err := handler.authService.FindByID(userID)
	if err != nil || !user.TOTPEnabled {
		spec := totpSessionExpiredErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	valid, err := handler.totpService.ValidateCode(userID, user.TOTPSecret, code)
	if err != nil {
		spec := totpInternalErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}
	if !valid {
		handler.totpService.RecordFailure(handler.secretKey, c.IP(), userID, time.Now())
		spec := totpInvalidCodeErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.totpService.ResetAttempts(handler.secretKey, c.IP(), userID)
	handler.clearTOTPPendingCookie(c)

	if _, err := handler.setAuthCookie(c, &user, rememberMe); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.2fa", spec)
		return handler.respondMappedError(c, spec)
	}

	handler.logSecurityEvent(c, "auth.2fa", "success")
	return redirectOrJSON(c, "/")
}
