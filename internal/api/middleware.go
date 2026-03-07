package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

const (
	authCookieName          = "ovumcy_auth"
	languageCookieName      = "ovumcy_lang"
	timezoneCookieName      = "ovumcy_tz"
	timezoneHeaderName      = "X-Ovumcy-Timezone"
	flashCookieName         = "ovumcy_flash"
	recoveryCodeCookieName  = "ovumcy_recovery_code"
	resetPasswordCookieName = "ovumcy_reset_password" // #nosec G101 -- cookie name contains "password" but is not a secret or credential.
	contextUserKey          = "current_user"
	contextLanguageKey      = "current_language"
	contextMessagesKey      = "current_messages"
	contextLocationKey      = "current_location"
)

func currentUser(c *fiber.Ctx) (*models.User, bool) {
	user, ok := c.Locals(contextUserKey).(*models.User)
	return user, ok
}
