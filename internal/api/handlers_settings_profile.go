package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The display name is account identity, not health data: it is not an
// observation, and nothing in the cycle math reads it. So this mutation is
// audited — an attacker renaming the account is exactly what an incident review
// looks for — but deliberately NOT under domain="health_data", because that
// filter's promise is that it selects the actions which changed the cycle
// record. Tagging a rename as health data would answer that question wrongly in
// the safe direction, which is still wrongly.
var profileSettingsMutation = accountMutationKind{action: "settings.profile_update", target: "profile"}

func (handler *Handler) UpdateProfile(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failAccountMutation(c, profileSettingsMutation, unauthorizedErrorSpec())
	}

	input := profileSettingsInput{}
	if err := c.Bind().Body(&input); err != nil {
		return handler.failAccountMutation(c, profileSettingsMutation, settingsValidationErrorSpec("invalid profile input"))
	}
	displayName, err := handler.settingsService.NormalizeDisplayName(input.DisplayName)
	if err != nil {
		return handler.failAccountMutation(c, profileSettingsMutation, mapSettingsProfileNormalizeError(err))
	}

	if err := handler.settingsService.UpdateDisplayName(c.Context(), user.ID, displayName); err != nil {
		return handler.failAccountMutation(c, profileSettingsMutation, settingsProfileUpdateErrorSpec())
	}

	status := handler.settingsService.ResolveProfileUpdateStatus(user.DisplayName, displayName)
	// The submitted name never reaches the audit line — the kind carries a fixed
	// target, so the event records that the profile changed, not to what.
	handler.logAccountMutationSuccess(c, profileSettingsMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":           true,
			"display_name": displayName,
			"status":       status,
		})
	}
	if isHTMX(c) {
		updatedUser := userAfterProfileUpdate(user, displayName)
		responseBody := htmxSettingsSuccessMarkup(c, status, "Profile updated successfully.")
		oobMarkup, err := handler.renderPartialString(c, "current_user_identity_oob", fiber.Map{
			"CurrentUser": updatedUser,
		})
		if err == nil {
			responseBody += oobMarkup
		}
		c.Type("html", "utf-8")
		return c.SendString(responseBody)
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}

func userAfterProfileUpdate(user *models.User, displayName string) *models.User {
	if user == nil {
		return nil
	}

	updatedUser := *user
	updatedUser.DisplayName = strings.TrimSpace(displayName)
	return &updatedUser
}
