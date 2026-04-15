package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) ShowLoginPage(c *fiber.Ctx) error {
	redirected, err := handler.redirectAuthenticatedUserIfPresent(c)
	if err != nil {
		return err
	}
	if redirected {
		return nil
	}
	needsSetup, err := handler.setupService.RequiresInitialSetup()
	if err != nil {
		return handler.respondMappedError(c, setupStateLoadErrorSpec())
	}

	flash := handler.popFlashCookie(c)
	data := buildLoginPageData(
		currentMessages(c),
		flash,
		needsSetup,
		handler.registrationService.RegistrationOpen(),
		handler.oidcEnabled(),
		handler.localPublicAuthEnabled(),
	)
	return handler.render(c, "login", data)
}

func (handler *Handler) ShowRegisterPage(c *fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	if user := handler.optionalAuthenticatedUser(c); user != nil {
		recoveryState := handler.readRecoveryCodeDisplayState(c, user.ID, services.PostLoginRedirectPath(user))
		if recoveryState.RecoveryCode != "" && recoveryState.Surface == recoveryCodeSurfaceInlineRegister {
			flash := handler.popFlashCookie(c)
			handler.clearRecoveryCodePageCookie(c)
			data := buildRegisterPageData(currentMessages(c), flash, false, handler.registrationService.RegistrationOpen())
			data["Title"] = localizedPageTitle(currentMessages(c), "meta.title.recovery_code", "Ovumcy | Recovery Code")
			data["CurrentUser"] = user
			data["HideNavigation"] = true
			data["RecoveryCode"] = recoveryState.RecoveryCode
			data["ContinuePath"] = recoveryState.ContinuePath
			data["ContinueTarget"] = recoveryState.ContinueTarget
			data["ShowInlineRecoveryCode"] = true
			return handler.render(c, "register", data)
		}
		if redirectErr := c.Redirect(services.PostLoginRedirectPath(user), fiber.StatusSeeOther); redirectErr != nil {
			return redirectErr
		}
		return nil
	}
	needsSetup, err := handler.setupService.RequiresInitialSetup()
	if err != nil {
		return handler.respondMappedError(c, setupStateLoadErrorSpec())
	}

	flash := handler.popFlashCookie(c)
	data := buildRegisterPageData(currentMessages(c), flash, needsSetup, handler.registrationService.RegistrationOpen())
	return handler.render(c, "register", data)
}

func (handler *Handler) ShowRecoveryCodePage(c *fiber.Ctx) error {
	user, err := handler.authenticateRequest(c)
	if err != nil {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	c.Locals(contextUserKey, user)

	fallbackContinuePath := services.PostLoginRedirectPath(user)
	recoveryState := handler.readRecoveryCodeDisplayState(c, user.ID, fallbackContinuePath)
	if recoveryState.RecoveryCode == "" {
		return c.Redirect(fallbackContinuePath, fiber.StatusSeeOther)
	}
	if recoveryState.Surface == recoveryCodeSurfaceInlineRegister {
		return c.Redirect("/register", fiber.StatusSeeOther)
	}
	handler.clearRecoveryCodePageCookie(c)

	return handler.render(c, "recovery_code", fiber.Map{
		"Title":          localizedPageTitle(currentMessages(c), "meta.title.recovery_code", "Ovumcy | Recovery Code"),
		"RecoveryCode":   recoveryState.RecoveryCode,
		"ContinuePath":   recoveryState.ContinuePath,
		"ContinueTarget": recoveryState.ContinueTarget,
		"HideNavigation": true,
	})
}

func (handler *Handler) ShowForgotPasswordPage(c *fiber.Ctx) error {
	if !handler.localPublicAuthEnabled() {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	flash := handler.popFlashCookie(c)
	data := buildForgotPasswordPageData(currentMessages(c), flash)
	return handler.render(c, "forgot_password", data)
}

func (handler *Handler) ShowResetPasswordPage(c *fiber.Ctx) error {
	flash := handler.popFlashCookie(c)
	data := handler.buildResetPasswordPageData(c, currentMessages(c), flash)
	return handler.render(c, "reset_password", data)
}
