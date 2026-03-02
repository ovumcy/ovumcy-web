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
	handler.ensureDependencies()

	status := handler.notificationService.ResolveSettingsStatus(
		flash.SettingsSuccess,
		c.Query("success"),
		c.Query("status"),
	)
	errorKey := ""
	changePasswordErrorKey := ""
	if status == "" {
		errorSource := handler.notificationService.ResolveSettingsErrorSource(flash.SettingsError, c.Query("error"))
		translatedErrorKey := services.AuthErrorTranslationKey(errorSource)
		if translatedErrorKey != "" {
			if handler.notificationService.ClassifySettingsErrorSource(errorSource) == services.SettingsErrorTargetChangePassword {
				changePasswordErrorKey = translatedErrorKey
			} else {
				errorKey = translatedErrorKey
			}
		}
	}

	persisted, err := handler.settingsService.LoadSettings(user.ID)
	if err != nil {
		return nil, err
	}

	cycleLength, periodLength := services.ResolveCycleAndPeriodDefaults(persisted.CycleLength, persisted.PeriodLength)
	autoPeriodFill := persisted.AutoPeriodFill
	user.CycleLength = cycleLength
	user.PeriodLength = periodLength
	user.AutoPeriodFill = autoPeriodFill
	user.LastPeriodStart = persisted.LastPeriodStart

	lastPeriodStart := ""
	if persisted.LastPeriodStart != nil {
		lastPeriodStart = services.DateAtLocation(*persisted.LastPeriodStart, handler.location).Format("2006-01-02")
	}
	minCycleStart, today := services.SettingsCycleStartDateBounds(time.Now().In(handler.location), handler.location)

	data := fiber.Map{
		"Title":                  localizedPageTitle(messages, "meta.title.settings", "Ovumcy | Settings"),
		"CurrentUser":            user,
		"ErrorKey":               errorKey,
		"ChangePasswordErrorKey": changePasswordErrorKey,
		"SuccessKey":             services.SettingsStatusTranslationKey(status),
		"CycleLength":            cycleLength,
		"PeriodLength":           periodLength,
		"AutoPeriodFill":         autoPeriodFill,
		"LastPeriodStart":        lastPeriodStart,
		"TodayISO":               today.Format("2006-01-02"),
		"CycleStartMinISO":       minCycleStart.Format("2006-01-02"),
	}

	if user.Role == models.RoleOwner {
		summary, err := handler.exportService.BuildSummary(user.ID, nil, nil, handler.location)
		if err != nil {
			return nil, err
		}
		data["ExportTotalEntries"] = summary.TotalEntries
		data["HasExportData"] = summary.HasData
		data["ExportDateFrom"] = summary.DateFrom
		data["ExportDateTo"] = summary.DateTo
		displayFrom := summary.DateFrom
		if parsedFrom, parseErr := services.ParseDayDate(summary.DateFrom, handler.location); parseErr == nil {
			displayFrom = services.LocalizedDateDisplay(language, parsedFrom)
		}
		displayTo := summary.DateTo
		if parsedTo, parseErr := services.ParseDayDate(summary.DateTo, handler.location); parseErr == nil {
			displayTo = services.LocalizedDateDisplay(language, parsedTo)
		}
		data["ExportDateFromDisplay"] = displayFrom
		data["ExportDateToDisplay"] = displayTo
	}

	return data, nil
}
