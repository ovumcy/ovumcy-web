package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func parseForgotPasswordCode(c *fiber.Ctx) (string, string) {
	input := forgotPasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return "", "invalid input"
	}

	code, err := services.NormalizeForgotPasswordCode(input.RecoveryCode)
	if err != nil {
		return "", "invalid recovery code"
	}
	return code, ""
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
	if acceptsJSON(c) {
		return c.Status(status).JSON(fiber.Map{
			"ok":            true,
			"recovery_code": recoveryCode,
		})
	}

	continuePath := "/dashboard"
	userID := uint(0)
	if user != nil {
		userID = user.ID
		continuePath = services.PostLoginRedirectPath(user)
	}
	if err := handler.setRecoveryCodePageCookie(c, userID, recoveryCode, continuePath); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to persist recovery code")
	}

	return redirectToPath(c, "/recovery-code")
}
