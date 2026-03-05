package api

import (
	"strings"

	"github.com/gofiber/fiber/v2"
)

func (handler *Handler) ClearAllData(c *fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}
	if err := handler.settingsService.ClearAllData(user.ID); err != nil {
		return handler.respondMappedError(c, settingsClearDataErrorSpec())
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
		return handler.respondMappedError(c, unauthorizedErrorSpec())
	}

	input := deleteAccountInput{}
	if err := c.BodyParser(&input); err != nil && acceptsJSON(c) {
		return handler.respondMappedError(c, settingsMissingPasswordErrorSpec())
	}

	input.Password = strings.TrimSpace(input.Password)
	if input.Password == "" {
		input.Password = strings.TrimSpace(c.FormValue("password"))
	}
	if input.Password == "" {
		return handler.respondMappedError(c, settingsMissingPasswordErrorSpec())
	}
	if err := handler.settingsService.ValidateDeleteAccountPassword(user.PasswordHash, input.Password); err != nil {
		return handler.respondMappedError(c, mapSettingsDeleteAccountPasswordError(err))
	}

	if err := handler.settingsService.DeleteAccount(user.ID); err != nil {
		return handler.respondMappedError(c, settingsDeleteAccountErrorSpec())
	}

	handler.clearAuthCookie(c)
	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true})
	}
	return redirectOrJSON(c, "/login")
}
