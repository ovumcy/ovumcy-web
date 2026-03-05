package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) Register(c *fiber.Ctx) error {
	credentials, err := parseCredentials(c)
	if err != nil {
		return handler.respondMappedError(c, authInvalidInputErrorSpec())
	}

	user, recoveryCode, err := handler.registrationService.RegisterOwnerAccount(
		credentials.Email,
		credentials.Password,
		credentials.ConfirmPassword,
		time.Now().In(handler.location),
	)
	if err != nil {
		return handler.respondMappedError(c, mapAuthRegisterError(err))
	}

	if err := handler.setAuthCookie(c, &user, true); err != nil {
		return handler.respondMappedError(c, authSessionCreateErrorSpec())
	}

	return handler.renderRecoveryCodeResponse(c, &user, recoveryCode, fiber.StatusCreated)
}

func (handler *Handler) Login(c *fiber.Ctx) error {
	credentials, err := parseCredentials(c)
	if err != nil {
		return handler.respondMappedError(c, authInvalidInputErrorSpec())
	}
	result, err := handler.loginService.Authenticate(
		handler.secretKey,
		credentials.Email,
		credentials.Password,
		30*time.Minute,
		time.Now(),
	)
	if err != nil {
		return handler.respondMappedError(c, mapAuthLoginError(err))
	}

	if result.RequiresPasswordReset {
		if err := handler.setResetPasswordCookie(c, result.ResetToken, true); err != nil {
			return handler.respondMappedError(c, authResetTokenCreateErrorSpec())
		}
		if acceptsJSON(c) {
			return handler.respondMappedError(c, passwordChangeRequiredErrorSpec())
		}
		return redirectToPath(c, "/reset-password")
	}

	user := result.User
	if err := handler.setAuthCookie(c, &user, credentials.RememberMe); err != nil {
		return handler.respondMappedError(c, authSessionCreateErrorSpec())
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
