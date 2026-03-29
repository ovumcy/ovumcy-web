package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

const (
	authCookieName               = "ovumcy_auth"
	languageCookieName           = "ovumcy_lang"
	timezoneCookieName           = "ovumcy_tz"
	timezoneHeaderName           = "X-Ovumcy-Timezone"
	flashCookieName              = "ovumcy_flash"
	recoveryCodeCookieName       = "ovumcy_recovery_code"
	resetPasswordCookieName      = "ovumcy_reset_password" // #nosec G101 -- cookie name contains "password" but is not a secret or credential.
	oidcStateCookieName          = "ovumcy_oidc_auth"
	oidcLogoutBridgeCookieName   = "ovumcy_oidc_logout_bridge"
	oidcLogoutBridgePath         = "/auth/oidc/logout"
	oidcLogoutBridgeRedirectPath = "/auth/oidc/logout/redirect"
	contextUserKey               = "current_user"
	contextAuthSessionKey        = "current_auth_session"
	contextLanguageKey           = "current_language"
	contextMessagesKey           = "current_messages"
	contextLocationKey           = "current_location"
)

func currentUser(c *fiber.Ctx) (*models.User, bool) {
	user, ok := c.Locals(contextUserKey).(*models.User)
	return user, ok
}

func currentAuthSession(c *fiber.Ctx) (*services.AuthSessionClaims, bool) {
	session, ok := c.Locals(contextAuthSessionKey).(*services.AuthSessionClaims)
	return session, ok
}
