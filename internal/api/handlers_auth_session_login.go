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

	handler.ensureDependencies()
	if err := handler.authService.ValidateRegistrationCredentials(credentials.Password, credentials.ConfirmPassword); err != nil {
		switch {
		case errors.Is(err, services.ErrAuthPasswordMismatch):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "password mismatch")
		case errors.Is(err, services.ErrAuthWeakPassword):
			return handler.respondAuthError(c, fiber.StatusBadRequest, "weak password")
		default:
			return handler.respondAuthError(c, fiber.StatusBadRequest, "invalid input")
		}
	}

	exists, err := handler.registrationEmailExists(credentials.Email)
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to create account")
	}
	if exists {
		return handler.respondAuthError(c, fiber.StatusConflict, "email already exists")
	}

	user, recoveryCode, err := handler.authService.BuildOwnerUserWithRecovery(credentials.Email, credentials.Password, time.Now().In(handler.location))
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to create account")
	}
	if err := handler.authService.CreateUser(&user); err != nil {
		return handler.respondAuthError(c, fiber.StatusConflict, "email already exists")
	}

	if err := handler.seedBuiltinSymptoms(user.ID); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to seed symptoms")
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

	handler.ensureDependencies()
	user, err := handler.authService.AuthenticateCredentials(credentials.Email, credentials.Password)
	if err != nil {
		return handler.respondAuthError(c, fiber.StatusUnauthorized, "invalid credentials")
	}

	if user.MustChangePassword {
		token, err := handler.passwordResetSvc.IssueResetTokenForUser(handler.secretKey, &user, 30*time.Minute, time.Now())
		if err != nil {
			return apiError(c, fiber.StatusInternalServerError, "failed to create reset token")
		}
		handler.setResetPasswordCookie(c, token, true)
		if acceptsJSON(c) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"error": "password change required",
			})
		}
		return redirectToPath(c, buildResetPasswordPath())
	}

	if err := handler.setAuthCookie(c, &user, credentials.RememberMe); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to create session")
	}

	return redirectOrJSON(c, postLoginRedirectPath(&user))
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
