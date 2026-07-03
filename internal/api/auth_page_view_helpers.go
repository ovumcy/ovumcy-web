package api

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func buildLoginPageData(messages map[string]string, flash FlashPayload, needsSetup bool, registrationOpen bool, oidcEnabled bool, localPublicAuthEnabled bool) fiber.Map {
	errorSource := services.ResolveAuthErrorSource(flash.AuthError)
	return fiber.Map{
		"Title":                  localizedPageTitle(messages, "meta.title.login", "Ovumcy | Login"),
		"ErrorKey":               services.AuthErrorTranslationKey(errorSource),
		"Email":                  "",
		"IsFirstLaunch":          needsSetup,
		"RegistrationOpen":       registrationOpen,
		"OIDCEnabled":            oidcEnabled,
		"LocalPublicAuthEnabled": localPublicAuthEnabled,
	}
}

func buildRegisterPageData(messages map[string]string, flash FlashPayload, needsSetup bool, registrationOpen bool) fiber.Map {
	errorSource := services.ResolveAuthErrorSource(flash.AuthError)
	if !registrationOpen && errorSource == "" {
		errorSource = "registration disabled"
	}
	return fiber.Map{
		"Title":            localizedPageTitle(messages, "meta.title.register", "Ovumcy | Sign Up"),
		"ErrorKey":         services.AuthErrorTranslationKey(errorSource),
		"Email":            "",
		"IsFirstLaunch":    needsSetup,
		"RegistrationOpen": registrationOpen,
	}
}

func buildForgotPasswordPageData(messages map[string]string, flash FlashPayload) fiber.Map {
	errorSource := services.ResolveAuthErrorSource(flash.AuthError)
	email := services.ResolveAuthPageEmail(flash.ForgotEmail)
	return fiber.Map{
		"Title":                localizedPageTitle(messages, "meta.title.forgot_password", "Ovumcy | Password Recovery"),
		"ErrorKey":             services.AuthErrorTranslationKey(errorSource),
		"Email":                email,
		"ShowRecoveryCodeStep": email != "",
	}
}

func (handler *Handler) buildResetPasswordPageData(c fiber.Ctx, messages map[string]string, flash FlashPayload) fiber.Map {
	token, forcedReset := handler.readResetPasswordCookie(c)
	invalidToken := false
	if token != "" {
		if !services.IsResetPasswordTokenValid(handler.secretKey, token, time.Now()) {
			invalidToken = true
			handler.clearResetPasswordCookie(c)
		}
	}

	errorSource := services.ResolveAuthErrorSource(flash.AuthError)
	return fiber.Map{
		"Title":        localizedPageTitle(messages, "meta.title.reset_password", "Ovumcy | Reset Password"),
		"InvalidToken": invalidToken,
		"ForcedReset":  forcedReset,
		"ErrorKey":     services.AuthErrorTranslationKey(errorSource),
	}
}
