package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func (handler *Handler) buildDashboardViewData(user *models.User, language string, messages map[string]string, now time.Time, location *time.Location) (fiber.Map, error) {
	viewData, err := handler.dashboardViewService.BuildDashboardViewData(user, language, now, location)
	if err != nil {
		return nil, err
	}

	data := fiber.Map{
		"Title":                       localizedPageTitle(messages, "meta.title.dashboard", "Ovumcy | Dashboard"),
		"CurrentUser":                 user,
		"Stats":                       viewData.Stats,
		"CycleDayReference":           viewData.CycleContext.CycleDayReference,
		"CycleDayWarning":             viewData.CycleContext.CycleDayWarning,
		"CycleDataStale":              viewData.CycleContext.CycleDataStale,
		"DisplayNextPeriodStart":      viewData.CycleContext.DisplayNextPeriodStart,
		"DisplayNextPeriodRangeStart": viewData.CycleContext.DisplayNextPeriodRangeStart,
		"DisplayNextPeriodRangeEnd":   viewData.CycleContext.DisplayNextPeriodRangeEnd,
		"DisplayNextPeriodUseRange":   viewData.CycleContext.DisplayNextPeriodUseRange,
		"DisplayNextPeriodDelayed":    viewData.CycleContext.DisplayNextPeriodDelayed,
		"DisplayOvulationDate":        viewData.CycleContext.DisplayOvulationDate,
		"DisplayOvulationExact":       viewData.CycleContext.DisplayOvulationExact,
		"DisplayOvulationImpossible":  viewData.CycleContext.DisplayOvulationImpossible,
		"NextPeriodInPast":            viewData.CycleContext.NextPeriodInPast,
		"OvulationInPast":             viewData.CycleContext.OvulationInPast,
		"Today":                       viewData.Today.Format("2006-01-02"),
		"Yesterday":                   viewData.Yesterday.Format("2006-01-02"),
		"YesterdayMonth":              viewData.YesterdayMonth,
		"FormattedDate":               viewData.FormattedDate,
		"TodayEntry":                  viewData.TodayLog,
		"TodayLog":                    viewData.TodayLog,
		"TodayHasData":                viewData.TodayHasData,
		"TodayEntryExists":            viewData.TodayEntryExists,
		"Symptoms":                    viewData.Symptoms,
		"PrimarySymptoms":             viewData.PrimarySymptoms,
		"ExtraSymptoms":               viewData.ExtraSymptoms,
		"HasExtraSymptoms":            viewData.HasExtraSymptoms,
		"SelectedSymptomID":           viewData.SelectedSymptomID,
		"ShowYesterdayJump":           viewData.ShowYesterdayJump,
		"ShowSexChip":                 viewData.ShowSexChip,
		"ShowBBTField":                viewData.ShowBBTField,
		"ShowCervicalMucus":           viewData.ShowCervicalMucus,
		"IsOwner":                     viewData.IsOwner,
	}
	return data, nil
}

func (handler *Handler) buildDayEditorPartialData(user *models.User, language string, messages map[string]string, day time.Time, now time.Time, location *time.Location, editMode bool) (fiber.Map, error) {
	viewData, err := handler.dashboardViewService.BuildDayEditorViewData(user, language, day, now, location)
	if err != nil {
		return nil, err
	}

	payload := fiber.Map{
		"Date":              viewData.Date,
		"DateString":        viewData.DateString,
		"DateLabel":         viewData.DateLabel,
		"IsFutureDate":      viewData.IsFutureDate,
		"NoDataLabel":       translateMessage(messages, "common.not_available"),
		"Log":               viewData.Log,
		"Symptoms":          viewData.Symptoms,
		"PrimarySymptoms":   viewData.PrimarySymptoms,
		"ExtraSymptoms":     viewData.ExtraSymptoms,
		"HasExtraSymptoms":  viewData.HasExtraSymptoms,
		"SelectedSymptomID": viewData.SelectedSymptomID,
		"HasDayData":        viewData.HasDayData,
		"ShowSexChip":       viewData.ShowSexChip,
		"ShowBBTField":      viewData.ShowBBTField,
		"ShowCervicalMucus": viewData.ShowCervicalMucus,
		"EditMode":          editMode,
		"IsOwner":           viewData.IsOwner,
	}
	return payload, nil
}
