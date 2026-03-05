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
		"Title":                      localizedPageTitle(messages, "meta.title.dashboard", "Ovumcy | Dashboard"),
		"CurrentUser":                user,
		"Stats":                      viewData.Stats,
		"CycleDayReference":          viewData.CycleContext.CycleDayReference,
		"CycleDayWarning":            viewData.CycleContext.CycleDayWarning,
		"CycleDataStale":             viewData.CycleContext.CycleDataStale,
		"DisplayNextPeriodStart":     viewData.CycleContext.DisplayNextPeriodStart,
		"DisplayOvulationDate":       viewData.CycleContext.DisplayOvulationDate,
		"DisplayOvulationExact":      viewData.CycleContext.DisplayOvulationExact,
		"DisplayOvulationImpossible": viewData.CycleContext.DisplayOvulationImpossible,
		"NextPeriodInPast":           viewData.CycleContext.NextPeriodInPast,
		"OvulationInPast":            viewData.CycleContext.OvulationInPast,
		"Today":                      viewData.Today.Format("2006-01-02"),
		"FormattedDate":              viewData.FormattedDate,
		"TodayEntry":                 viewData.TodayLog,
		"TodayLog":                   viewData.TodayLog,
		"TodayHasData":               viewData.TodayHasData,
		"Symptoms":                   viewData.Symptoms,
		"SelectedSymptomID":          viewData.SelectedSymptomID,
		"IsOwner":                    viewData.IsOwner,
	}
	return data, nil
}

func (handler *Handler) buildDayEditorPartialData(user *models.User, language string, messages map[string]string, day time.Time, now time.Time, location *time.Location) (fiber.Map, error) {
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
		"SelectedSymptomID": viewData.SelectedSymptomID,
		"HasDayData":        viewData.HasDayData,
		"IsOwner":           viewData.IsOwner,
	}
	return payload, nil
}
