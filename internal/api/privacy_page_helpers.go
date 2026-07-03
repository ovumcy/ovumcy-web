package api

import (
	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func buildPrivacyPageData(messages map[string]string, backQuery string, user *models.User) fiber.Map {
	navigation := services.BuildPrivacyBackNavigation(backQuery, user != nil)
	data := fiber.Map{
		"Title":                  localizedPageTitle(messages, "meta.title.privacy", "Ovumcy | Privacy Policy"),
		"MetaDescription":        services.ResolvePrivacyMetaDescription(translateMessage(messages, "meta.description.privacy")),
		"BackPath":               navigation.BackPath,
		"BreadcrumbBackLabelKey": navigation.BreadcrumbBackLabelKey,
	}

	if user != nil {
		data["CurrentUser"] = user
	}
	return data
}
