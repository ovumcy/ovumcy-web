package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func buildLoginPageData(c *fiber.Ctx, messages map[string]string, flash FlashPayload, needsSetup bool) fiber.Map {
	errorSource := services.ResolveAuthErrorSource(flash.AuthError, c.Query("error"))
	return fiber.Map{
		"Title":         localizedPageTitle(messages, "meta.title.login", "Ovumcy | Login"),
		"ErrorKey":      services.AuthErrorTranslationKey(errorSource),
		"Email":         services.ResolveAuthPageEmail(flash.LoginEmail, c.Query("email")),
		"IsFirstLaunch": needsSetup,
	}
}

func buildRegisterPageData(c *fiber.Ctx, messages map[string]string, flash FlashPayload, needsSetup bool) fiber.Map {
	errorSource := services.ResolveAuthErrorSource(flash.AuthError, "")
	return fiber.Map{
		"Title":         localizedPageTitle(messages, "meta.title.register", "Ovumcy | Sign Up"),
		"ErrorKey":      services.AuthErrorTranslationKey(errorSource),
		"Email":         services.ResolveAuthPageEmail(flash.RegisterEmail, ""),
		"IsFirstLaunch": needsSetup,
	}
}

func buildForgotPasswordPageData(c *fiber.Ctx, messages map[string]string, flash FlashPayload) fiber.Map {
	errorSource := services.ResolveAuthErrorSource(flash.AuthError, c.Query("error"))
	return fiber.Map{
		"Title":    localizedPageTitle(messages, "meta.title.forgot_password", "Ovumcy | Password Recovery"),
		"ErrorKey": services.AuthErrorTranslationKey(errorSource),
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

	errorSource := services.ResolveAuthErrorSource(flash.AuthError, c.Query("error"))
	return fiber.Map{
		"Title":        localizedPageTitle(messages, "meta.title.reset_password", "Ovumcy | Reset Password"),
		"InvalidToken": invalidToken,
		"ForcedReset":  forcedReset,
		"ErrorKey":     services.AuthErrorTranslationKey(errorSource),
	}
}
