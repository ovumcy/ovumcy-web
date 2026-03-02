package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ShowLoginPage(c *fiber.Ctx) error {
	redirected, err := handler.redirectAuthenticatedUserIfPresent(c)
	if err != nil {
		return err
	}
	if redirected {
		return nil
	}

	handler.ensureDependencies()
	needsSetup, err := handler.setupService.RequiresInitialSetup()
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to load setup state")
	}

	flash := handler.popFlashCookie(c)
	data := buildLoginPageData(c, currentMessages(c), flash, needsSetup)
	return handler.render(c, "login", data)
}

func (handler *Handler) ShowRegisterPage(c *fiber.Ctx) error {
	redirected, err := handler.redirectAuthenticatedUserIfPresent(c)
	if err != nil {
		return err
	}
	if redirected {
		return nil
	}

	handler.ensureDependencies()
	needsSetup, err := handler.setupService.RequiresInitialSetup()
	if err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to load setup state")
	}

	flash := handler.popFlashCookie(c)
	data := buildRegisterPageData(c, currentMessages(c), flash, needsSetup)
	return handler.render(c, "register", data)
}

func (handler *Handler) ShowRecoveryCodePage(c *fiber.Ctx) error {
	user, err := handler.authenticateRequest(c)
	if err != nil {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	c.Locals(contextUserKey, user)

	fallbackContinuePath := services.PostLoginRedirectPath(user)
	recoveryCode, continuePath := handler.readRecoveryCodePageCookie(c, user.ID, fallbackContinuePath)
	if recoveryCode == "" {
		return c.Redirect(fallbackContinuePath, fiber.StatusSeeOther)
	}

	return handler.render(c, "recovery_code", fiber.Map{
		"Title":          localizedPageTitle(currentMessages(c), "meta.title.recovery_code", "Ovumcy | Recovery Code"),
		"RecoveryCode":   recoveryCode,
		"ContinuePath":   continuePath,
		"HideNavigation": true,
	})
}

func (handler *Handler) ShowForgotPasswordPage(c *fiber.Ctx) error {
	flash := handler.popFlashCookie(c)
	data := buildForgotPasswordPageData(c, currentMessages(c), flash)
	return handler.render(c, "forgot_password", data)
}

func (handler *Handler) ShowResetPasswordPage(c *fiber.Ctx) error {
	flash := handler.popFlashCookie(c)
	data := handler.buildResetPasswordPageData(c, currentMessages(c), flash)
	return handler.render(c, "reset_password", data)
}
