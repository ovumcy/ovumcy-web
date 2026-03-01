package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func parseForgotPasswordCode(c *fiber.Ctx) (string, string) {
	input := forgotPasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return "", "invalid input"
	}

	code := strings.TrimSpace(input.RecoveryCode)
	if code == "" {
		return "", "invalid recovery code"
	}
	return code, ""
}

func parseResetPasswordInput(c *fiber.Ctx) (resetPasswordInput, string) {
	input := resetPasswordInput{}
	if err := c.BodyParser(&input); err != nil {
		return resetPasswordInput{}, "invalid input"
	}

	input.Password = strings.TrimSpace(input.Password)
	input.ConfirmPassword = strings.TrimSpace(input.ConfirmPassword)
	if input.Password == "" || input.ConfirmPassword == "" {
		return resetPasswordInput{}, "invalid input"
	}

	return input, ""
}

func buildResetPasswordPath() string {
	return "/reset-password"
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
	handler.setRecoveryCodePageCookie(c, userID, recoveryCode, continuePath)

	return redirectToPath(c, "/recovery-code")
}
