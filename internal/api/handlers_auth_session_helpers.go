package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func parseForgotPasswordInput(c *fiber.Ctx) (forgotPasswordInput, string) {
	input := forgotPasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return forgotPasswordInput{}, "invalid input"
	}
	input.Email = services.NormalizeAuthEmail(input.Email)
	if input.Email == "" {
		return forgotPasswordInput{}, "invalid input"
	}

	rawCode := strings.TrimSpace(input.RecoveryCode)
	if rawCode == "" {
		input.RecoveryCode = ""
		return input, ""
	}

	code, err := services.NormalizeForgotPasswordCode(rawCode)
	if err != nil {
		return forgotPasswordInput{}, "invalid recovery code"
	}
	input.RecoveryCode = code
	return input, ""
}

func parseResetPasswordInput(c *fiber.Ctx) (resetPasswordInput, string) {
	input := resetPasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return resetPasswordInput{}, "invalid input"
	}

	password, confirmPassword, err := services.NormalizeResetPasswordInput(input.Password, input.ConfirmPassword)
	if err != nil {
		return resetPasswordInput{}, "invalid input"
	}
	input.Password = password
	input.ConfirmPassword = confirmPassword

	return input, ""
}

func redirectToPath(c *fiber.Ctx, path string) error {
	if isHTMX(c) {
		c.Set("HX-Redirect", path)
		return c.SendStatus(fiber.StatusOK)
	}
	return c.Redirect(path, fiber.StatusSeeOther)
}

func (handler *Handler) renderRecoveryCodeResponse(c *fiber.Ctx, user *models.User, recoveryCode string, status int) error {
	continuePath := "/dashboard"
	if user != nil {
		continuePath = services.PostLoginRedirectPath(user)
	}
	return handler.renderRecoveryCodeResponseWithSurface(c, user, recoveryCode, status, continuePath, recoveryCodeSurfaceDedicated)
}

func (handler *Handler) renderRegisterInlineRecoveryResponse(c *fiber.Ctx, user *models.User, recoveryCode string, status int) error {
	continuePath := "/dashboard"
	if user != nil {
		continuePath = services.PostLoginRedirectPath(user)
	}
	return handler.renderRecoveryCodeResponseWithSurface(c, user, recoveryCode, status, continuePath, recoveryCodeSurfaceInlineRegister)
}

func (handler *Handler) renderRecoveryCodeResponseWithContinuePath(c *fiber.Ctx, user *models.User, recoveryCode string, status int, continuePath string) error {
	return handler.renderRecoveryCodeResponseWithSurface(c, user, recoveryCode, status, continuePath, recoveryCodeSurfaceDedicated)
}

func (handler *Handler) renderRecoveryCodeResponseWithSurface(c *fiber.Ctx, user *models.User, recoveryCode string, status int, continuePath string, surface string) error {
	userID := uint(0)
	if user != nil {
		userID = user.ID
	}
	if err := handler.setRecoveryCodeIssuanceCookie(c, userID, recoveryCode, continuePath, surface); err != nil {
		return handler.respondMappedError(c, authRecoveryCodePersistErrorSpec())
	}

	nextPath := recoveryCodeSurfacePath(surface)
	if acceptsJSON(c) {
		return c.Status(status).JSON(fiber.Map{
			"ok":        true,
			"next_step": "recovery_code",
			"next_path": nextPath,
		})
	}

	return redirectToPath(c, nextPath)
}

func recoveryCodeSurfacePath(surface string) string {
	if sanitizeRecoveryCodeSurface(surface) == recoveryCodeSurfaceInlineRegister {
		return "/register"
	}
	return "/recovery-code"
}
