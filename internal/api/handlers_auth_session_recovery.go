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
		return handler.respondMappedError(c, authValidationErrorSpec(parseError))
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
		return handler.respondMappedError(c, mapPasswordRecoveryStartError(err))
	}
	if err := handler.setResetPasswordCookie(c, token, false); err != nil {
		return handler.respondMappedError(c, authResetTokenCreateErrorSpec())
	}

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
		return handler.respondMappedError(c, authValidationErrorSpec(parseError))
	}

	token, _ := handler.readResetPasswordCookie(c)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		return handler.respondMappedError(c, invalidResetTokenErrorSpec())
	}
	user, recoveryCode, err := handler.passwordResetSvc.CompleteReset(
		handler.secretKey,
		token,
		input.Password,
		input.ConfirmPassword,
		time.Now(),
	)
	if err != nil {
		if spec := mapPasswordResetCompleteError(err); spec.Key == "invalid reset token" {
			handler.clearResetPasswordCookie(c)
		}
		return handler.respondMappedError(c, mapPasswordResetCompleteError(err))
	}

	if err := handler.setAuthCookie(c, user, true); err != nil {
		return handler.respondMappedError(c, authSessionCreateErrorSpec())
	}
	handler.clearResetPasswordCookie(c)

	return handler.renderRecoveryCodeResponse(c, user, recoveryCode, fiber.StatusOK)
}
