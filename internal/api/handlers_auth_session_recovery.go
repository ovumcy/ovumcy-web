package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ForgotPassword(c *fiber.Ctx) error {
	now := time.Now().In(handler.location)
	rawRecoveryCode, parseError := parseForgotPasswordCode(c)
	if parseError != "" {
		return handler.respondAuthError(c, fiber.StatusBadRequest, parseError)
	}
	token, err := handler.passwordResetSvc.StartRecovery(
		handler.secretKey,
		c.IP(),
		rawRecoveryCode,
		now,
		30*time.Minute,
	)
	if err != nil {
		switch services.ClassifyPasswordRecoveryStartError(err) {
		case services.PasswordRecoveryStartErrorRateLimited:
			return handler.respondAuthError(c, fiber.StatusTooManyRequests, "too many recovery attempts")
		case services.PasswordRecoveryStartErrorInvalidCode:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid recovery code")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to create reset token")
		}
	}
	if err := handler.setResetPasswordCookie(c, token, false); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to create reset token")
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
		return handler.respondAuthError(c, fiber.StatusBadRequest, parseError)
	}

	token, _ := handler.readResetPasswordCookie(c)
	if token == "" {
		handler.clearResetPasswordCookie(c)
		return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid reset token")
	}
	user, recoveryCode, err := handler.passwordResetSvc.CompleteReset(
		handler.secretKey,
		token,
		input.Password,
		input.ConfirmPassword,
		time.Now(),
	)
	if err != nil {
		switch services.ClassifyPasswordResetCompleteError(err) {
		case services.PasswordResetCompleteErrorPasswordMismatch:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "password mismatch")
		case services.PasswordResetCompleteErrorWeakPassword:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "weak password")
		case services.PasswordResetCompleteErrorInvalidInput:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
		case services.PasswordResetCompleteErrorInvalidToken:
			handler.clearResetPasswordCookie(c)
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid reset token")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to reset password")
		}
	}

	if err := handler.setAuthCookie(c, user, true); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to create session")
	}
	handler.clearResetPasswordCookie(c)

	return handler.renderRecoveryCodeResponse(c, user, recoveryCode, fiber.StatusOK)
}
