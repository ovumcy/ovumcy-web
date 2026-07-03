package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

func (handler *Handler) buildSettingsPageData(c fiber.Ctx, user *models.User) (fiber.Map, error) {
	flash := handler.popFlashCookie(c)
	data, err := handler.buildSettingsViewData(c, user, flash)
	if err != nil {
		return nil, err
	}
	return data, nil
}
