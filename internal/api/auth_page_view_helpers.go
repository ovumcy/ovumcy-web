package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func authErrorKeyFromFlashOrQuery(c *fiber.Ctx, flashAuthError string) string {
	errorSource := services.ResolveAuthErrorSource(flashAuthError, c.Query("error"))
	return services.AuthErrorTranslationKey(errorSource)
}

func loginEmailFromFlashOrQuery(c *fiber.Ctx, flashEmail string) string {
	return services.ResolveAuthPageEmail(flashEmail, c.Query("email"))
}

func buildLoginPageData(c *fiber.Ctx, messages map[string]string, flash FlashPayload, needsSetup bool) fiber.Map {
	return fiber.Map{
		"Title":         localizedPageTitle(messages, "meta.title.login", "Ovumcy | Login"),
		"ErrorKey":      authErrorKeyFromFlashOrQuery(c, flash.AuthError),
		"Email":         loginEmailFromFlashOrQuery(c, flash.LoginEmail),
		"IsFirstLaunch": needsSetup,
	}
}

func buildRegisterPageData(c *fiber.Ctx, messages map[string]string, flash FlashPayload, needsSetup bool) fiber.Map {
	return fiber.Map{
		"Title":         localizedPageTitle(messages, "meta.title.register", "Ovumcy | Sign Up"),
		"ErrorKey":      authErrorKeyFromFlashOrQuery(c, flash.AuthError),
		"Email":         loginEmailFromFlashOrQuery(c, flash.RegisterEmail),
		"IsFirstLaunch": needsSetup,
	}
}

func buildForgotPasswordPageData(c *fiber.Ctx, messages map[string]string, flash FlashPayload) fiber.Map {
	return fiber.Map{
		"Title":    localizedPageTitle(messages, "meta.title.forgot_password", "Ovumcy | Password Recovery"),
		"ErrorKey": authErrorKeyFromFlashOrQuery(c, flash.AuthError),
	}
}

func (handler *Handler) buildResetPasswordPageData(c *fiber.Ctx, messages map[string]string, flash FlashPayload) fiber.Map {
	token, forcedReset := handler.readResetPasswordCookie(c)
	invalidToken := false
	if token != "" {
		if !services.IsResetPasswordTokenValid(handler.secretKey, token, time.Now()) {
			invalidToken = true
			handler.clearResetPasswordCookie(c)
		}
	}

	return fiber.Map{
		"Title":        localizedPageTitle(messages, "meta.title.reset_password", "Ovumcy | Reset Password"),
		"InvalidToken": invalidToken,
		"ForcedReset":  forcedReset,
		"ErrorKey":     authErrorKeyFromFlashOrQuery(c, flash.AuthError),
	}
}
