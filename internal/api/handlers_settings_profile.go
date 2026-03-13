package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) UpdateProfile(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input := profileSettingsInput{}
	if err := c.BodyParser(&input); err != nil {
		return handler.respondMappedError(c, settingsValidationErrorSpec("invalid profile input"))
	}
	displayName, err := handler.settingsService.NormalizeDisplayName(input.DisplayName)
	if err != nil {
		return handler.respondMappedError(c, mapSettingsProfileNormalizeError(err))
	}

	if err := handler.settingsService.UpdateDisplayName(user.ID, displayName); err != nil {
		return handler.respondMappedError(c, settingsProfileUpdateErrorSpec())
	}

	status := handler.settingsService.ResolveProfileUpdateStatus(user.DisplayName, displayName)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{
			"ok":           true,
			"display_name": displayName,
			"status":       status,
		})
	}
	if isHTMX(c) {
		updatedUser := userAfterProfileUpdate(user, displayName)
		messageKey := services.SettingsStatusTranslationKey(status)
		message := translateMessage(currentMessages(c), messageKey)
		if message == "" || message == messageKey {
			message = "Profile updated successfully."
		}
		responseBody := htmxDismissibleSuccessStatusMarkup(currentMessages(c), message)
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
