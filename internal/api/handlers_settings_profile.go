package api

import (
	"errors"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) UpdateProfile(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	input := profileSettingsInput{}
	if err := c.BodyParser(&input); err != nil {
		return handler.respondSettingsError(c, fiber.StatusBadRequest, "invalid profile input")
	}
	displayName, err := handler.settingsService.NormalizeDisplayName(input.DisplayName)
	if err != nil {
		if errors.Is(err, services.ErrSettingsDisplayNameTooLong) {
			return handler.respondSettingsError(c, fiber.StatusBadRequest, "display name too long")
		}
		return handler.respondSettingsError(c, fiber.StatusBadRequest, "invalid profile input")
	}

	if err := handler.settingsService.UpdateDisplayName(user.ID, displayName); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to update profile")
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
		messageKey := services.SettingsStatusTranslationKey(status)
		message := translateMessage(currentMessages(c), messageKey)
		if message == "" || message == messageKey {
			message = "Profile updated successfully."
		}
		return c.SendString(htmxDismissibleSuccessStatusMarkup(currentMessages(c), message))
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}
