package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func (handler *Handler) buildSettingsPageData(c *fiber.Ctx, user *models.User) (fiber.Map, error) {
	flash := handler.popFlashCookie(c)
	data, err := handler.buildSettingsViewData(c, user, flash)
	if err != nil {
		return nil, err
	}
	return data, nil
}
