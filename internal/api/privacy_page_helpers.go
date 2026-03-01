package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func buildPrivacyMetaDescription(messages map[string]string) string {
	metaDescription := translateMessage(messages, "meta.description.privacy")
	if metaDescription == "meta.description.privacy" {
		metaDescription = "Ovumcy Privacy Policy - Zero data collection, self-hosted period tracker."
	}
	return metaDescription
}

func buildPrivacyPageData(messages map[string]string, backQuery string, user *models.User) fiber.Map {
	backFallback := "/login"
	breadcrumbBackLabelKey := "common.home"
	data := fiber.Map{
		"Title":           localizedPageTitle(messages, "meta.title.privacy", "Ovumcy | Privacy Policy"),
		"MetaDescription": buildPrivacyMetaDescription(messages),
	}

	if user != nil {
		data["CurrentUser"] = user
		backFallback = "/dashboard"
		breadcrumbBackLabelKey = "nav.dashboard"
	}
	data["BackPath"] = services.SanitizeRedirectPath(backQuery, backFallback)
	data["BreadcrumbBackLabelKey"] = breadcrumbBackLabelKey
	return data
}
