package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) buildSettingsViewData(c *fiber.Ctx, user *models.User, flash FlashPayload) (fiber.Map, error) {
	messages := currentMessages(c)
	language := currentLanguage(c)

	viewData, err := handler.settingsViewService.BuildSettingsPageViewData(
		user,
		language,
		services.SettingsViewInput{
			FlashSuccess: flash.SettingsSuccess,
			FlashError:   flash.SettingsError,
			QuerySuccess: c.Query("success"),
			QueryStatus:  c.Query("status"),
			QueryError:   c.Query("error"),
		},
		time.Now().In(handler.location),
		handler.location,
	)
	if err != nil {
		return nil, err
	}

	*user = viewData.CurrentUser

	data := fiber.Map{
		"Title":                  localizedPageTitle(messages, "meta.title.settings", "Ovumcy | Settings"),
		"CurrentUser":            user,
		"ErrorKey":               viewData.ErrorKey,
		"ChangePasswordErrorKey": viewData.ChangePasswordErrorKey,
		"SuccessKey":             viewData.SuccessKey,
		"CycleLength":            viewData.CycleLength,
		"PeriodLength":           viewData.PeriodLength,
		"AutoPeriodFill":         viewData.AutoPeriodFill,
		"LastPeriodStart":        viewData.LastPeriodStart,
		"TodayISO":               viewData.TodayISO,
		"CycleStartMinISO":       viewData.CycleStartMinISO,
	}

	if viewData.HasOwnerExportViewState {
		data["ExportTotalEntries"] = viewData.Export.TotalEntries
		data["HasExportData"] = viewData.Export.HasData
		data["ExportDateFrom"] = viewData.Export.DateFrom
		data["ExportDateTo"] = viewData.Export.DateTo
		data["ExportDateFromDisplay"] = viewData.Export.DateFromDisplay
		data["ExportDateToDisplay"] = viewData.Export.DateToDisplay
	}

	return data, nil
}
