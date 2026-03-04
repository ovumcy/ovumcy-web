package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) Register(c *fiber.Ctx) error {
	credentials, err := parseCredentials(c)
	if err != nil {
		return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
	}

	user, recoveryCode, err := handler.registrationService.RegisterOwnerAccount(
		credentials.Email,
		credentials.Password,
		credentials.ConfirmPassword,
		time.Now().In(handler.location),
	)
	if err != nil {
		switch services.ClassifyAuthRegisterError(err) {
		case services.AuthRegisterErrorPasswordMismatch:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "password mismatch")
		case services.AuthRegisterErrorWeakPassword:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "weak password")
		case services.AuthRegisterErrorEmailExists:
			// Keep duplicate registration responses generic to avoid account existence disclosure.
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
		case services.AuthRegisterErrorInvalidInput:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
		case services.AuthRegisterErrorSeedSymptoms:
			return apiError(c, fiber.StatusInternalServerError, "failed to seed symptoms")
		case services.AuthRegisterErrorFailed:
			return apiError(c, fiber.StatusInternalServerError, "failed to create account")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to create account")
		}
	}

	if err := handler.setAuthCookie(c, &user, true); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to create session")
	}

	return handler.renderRecoveryCodeResponse(c, &user, recoveryCode, fiber.StatusCreated)
}

func (handler *Handler) Login(c *fiber.Ctx) error {
	credentials, err := parseCredentials(c)
	if err != nil {
		return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
	}
	result, err := handler.loginService.Authenticate(
		handler.secretKey,
		credentials.Email,
		credentials.Password,
		30*time.Minute,
		time.Now(),
	)
	if err != nil {
		switch services.ClassifyAuthLoginError(err) {
		case services.AuthLoginErrorResetTokenIssue:
			return apiError(c, fiber.StatusInternalServerError, "failed to create reset token")
		case services.AuthLoginErrorInvalidCredentials:
			return handler.respondAuthError(c, fiber.StatusUnauthorized, "invalid credentials")
		default:
			return handler.respondAuthError(c, fiber.StatusUnauthorized, "invalid credentials")
		}
	}

	if result.RequiresPasswordReset {
		if err := handler.setResetPasswordCookie(c, result.ResetToken, true); err != nil {
			return apiError(c, fiber.StatusInternalServerError, "failed to create reset token")
		}
		if acceptsJSON(c) {
			return apiError(c, fiber.StatusForbidden, "password change required")
		}
		return redirectToPath(c, "/reset-password")
	}

	user := result.User
	if err := handler.setAuthCookie(c, &user, credentials.RememberMe); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to create session")
	}

	return redirectOrJSON(c, services.PostLoginRedirectPath(&user))
}

func (handler *Handler) Logout(c *fiber.Ctx) error {
	handler.clearAuthCookie(c)
	handler.clearRecoveryCodePageCookie(c)
	handler.clearResetPasswordCookie(c)
	if isHTMX(c) {
		c.Set("HX-Redirect", "/login")
		return c.SendStatus(fiber.StatusOK)
	}
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return c.Redirect("/login", fiber.StatusSeeOther)
}
