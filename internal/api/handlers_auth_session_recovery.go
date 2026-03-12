package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) ForgotPassword(c *fiber.Ctx) error {
	now := time.Now().In(handler.location)
	input, parseError := parseForgotPasswordInput(c)
	if parseError != "" {
		spec := authValidationErrorSpec(parseError)
		handler.logSecurityError(c, "auth.recovery_start", spec)
		return handler.respondMappedError(c, spec)
	}

	if strings.TrimSpace(input.RecoveryCode) == "" {
		if acceptsJSON(c) {
			return c.JSON(fiber.Map{
				"ok":        true,
				"next_step": "recovery_code",
			})
		}
		handler.setFlashCookie(c, FlashPayload{
			ForgotEmail: input.Email,
		})
		return redirectToPath(c, "/forgot-password")
	}

	token, err := handler.passwordResetSvc.StartRecovery(
		handler.secretKey,
		c.IP(),
		input.Email,
		input.RecoveryCode,
		now,
		30*time.Minute,
	)
	if err != nil {
		spec := mapPasswordRecoveryStartError(err)
		handler.logSecurityError(c, "auth.recovery_start", spec)
		return handler.respondMappedError(c, spec)
	}
	if err := handler.setResetPasswordCookie(c, token, false); err != nil {
		spec := authResetTokenCreateErrorSpec()
		handler.logSecurityError(c, "auth.recovery_start", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.logSecurityEvent(c, "auth.recovery_start", "success")

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok": true,
		})
	}

	return redirectToPath(c, "/reset-password")
}

func (handler *Handler) ResetPassword(c *fiber.Ctx) error {
	input, parseError := parseResetPasswordInput(c)
	if parseError != "" {
		spec := authValidationErrorSpec(parseError)
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}

	token, _ := handler.readResetPasswordCookie(c)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		spec := invalidResetTokenErrorSpec()
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}
	user, recoveryCode, err := handler.passwordResetSvc.CompleteReset(
		handler.secretKey,
		token,
		input.Password,
		input.ConfirmPassword,
		time.Now(),
	)
	if err != nil {
		spec := mapPasswordResetCompleteError(err)
		if spec.Key == "invalid reset token" {
			handler.clearResetPasswordCookie(c)
		}
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}

	if err := handler.setAuthCookie(c, user, true); err != nil {
		spec := authSessionCreateErrorSpec()
		handler.logSecurityError(c, "auth.reset_password", spec)
		return handler.respondMappedError(c, spec)
	}
	handler.clearResetPasswordCookie(c)
	handler.logSecurityEvent(c, "auth.reset_password", "success")

	return handler.renderRecoveryCodeResponse(c, user, recoveryCode, fiber.StatusOK)
}
