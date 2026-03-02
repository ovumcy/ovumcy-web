package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) ClearAllData(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}
	if err := handler.settingsService.ClearAllData(user.ID); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to clear data")
	}

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: "data_cleared"})
	return redirectOrJSON(c, "/settings")
}

func (handler *Handler) DeleteAccount(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return apiError(c, fiber.StatusUnauthorized, "unauthorized")
	}

	input := deleteAccountInput{}
	if err := c.BodyParser(&input); err != nil && acceptsJSON(c) {
		return handler.respondSettingsError(c, fiber.StatusBadRequest, "invalid password")
	}

	input.Password = strings.TrimSpace(input.Password)
	if input.Password == "" {
		input.Password = strings.TrimSpace(c.FormValue("password"))
	}
	if input.Password == "" {
		return handler.respondSettingsError(c, fiber.StatusBadRequest, "invalid password")
	}
	if err := handler.settingsService.ValidateDeleteAccountPassword(user.PasswordHash, input.Password); err != nil {
		switch services.ClassifySettingsDeleteAccountPasswordError(err) {
		case services.SettingsDeleteAccountPasswordErrorMissing:
			return handler.respondSettingsError(c, fiber.StatusBadRequest, "invalid password")
		case services.SettingsDeleteAccountPasswordErrorInvalid:
			return handler.respondSettingsError(c, fiber.StatusUnauthorized, "invalid password")
		default:
			return apiError(c, fiber.StatusInternalServerError, "failed to validate password")
		}
	}

	if err := handler.settingsService.DeleteAccount(user.ID); err != nil {
		return apiError(c, fiber.StatusInternalServerError, "failed to delete account")
	}

	handler.clearAuthCookie(c)
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return redirectOrJSON(c, "/login")
}
