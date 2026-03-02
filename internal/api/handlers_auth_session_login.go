package api

import (
	"errors"
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
		switch {
		case errors.Is(err, services.ErrAuthPasswordMismatch):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "password mismatch")
		case errors.Is(err, services.ErrAuthWeakPassword):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "weak password")
		case errors.Is(err, services.ErrAuthEmailExists):
			return handler.respondAuthError(c, fiber.StatusConflict, "email already exists")
		case errors.Is(err, services.ErrAuthRegisterInvalid):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
		case errors.Is(err, services.ErrAuthRegisterFailed):
			return apiError(c, fiber.StatusInternalServerError, "failed to create account")
		case errors.Is(err, services.ErrRegistrationSeedSymptoms):
			return apiError(c, fiber.StatusInternalServerError, "failed to seed symptoms")
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
		if errors.Is(err, services.ErrLoginResetTokenIssue) {
			return apiError(c, fiber.StatusInternalServerError, "failed to create reset token")
		}
		return handler.respondAuthError(c, fiber.StatusUnauthorized, "invalid credentials")
	}

	if result.RequiresPasswordReset {
		handler.setResetPasswordCookie(c, result.ResetToken, true)
		if acceptsJSON(c) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "password change required",
			})
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
