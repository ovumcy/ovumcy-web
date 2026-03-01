package api

import (
	"errors"
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

	handler.ensureDependencies()
	token, err := handler.passwordResetSvc.StartRecovery(
		handler.secretKey,
		requestLimiterKey(c),
		rawRecoveryCode,
		now,
		30*time.Minute,
	)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrPasswordRecoveryRateLimited):
			return handler.respondAuthError(c, fiber.StatusTooManyRequests, "too many recovery attempts")
		case errors.Is(err, services.ErrPasswordRecoveryCodeInvalid):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid recovery code")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to create reset token")
		}
	}
	handler.setResetPasswordCookie(c, token, false)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok": true,
		})
	}

	return redirectToPath(c, buildResetPasswordPath())
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

	handler.ensureDependencies()
	user, recoveryCode, err := handler.passwordResetSvc.CompleteReset(
		handler.secretKey,
		token,
		input.Password,
		input.ConfirmPassword,
		time.Now(),
	)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrAuthPasswordMismatch):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "password mismatch")
		case errors.Is(err, services.ErrAuthWeakPassword):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "weak password")
		case errors.Is(err, services.ErrAuthResetInvalid):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
		case errors.Is(err, services.ErrInvalidResetToken):
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
