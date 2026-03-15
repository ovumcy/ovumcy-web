package api

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) buildDashboardViewData(user *models.User, language string, messages map[string]string, now time.Time, location *time.Location) (fiber.Map, error) {
	viewData, err := handler.dashboardViewService.BuildDashboardViewData(user, language, now, location)
	if err != nil {
		return nil, err
	}
	bbtView := buildBBTFieldViewData(messages, user.TemperatureUnit)

	data := fiber.Map{
		"Title":                                 localizedPageTitle(messages, "meta.title.dashboard", "Ovumcy | Dashboard"),
		"CurrentUser":                           user,
		"Stats":                                 viewData.Stats,
		"CycleDayReference":                     viewData.CycleContext.CycleDayReference,
		"CycleDayWarning":                       viewData.CycleContext.CycleDayWarning,
		"CycleDataStale":                        viewData.CycleContext.CycleDataStale,
		"PredictionDisabled":                    viewData.CycleContext.PredictionDisabled,
		"DisplayNextPeriodStart":                viewData.CycleContext.DisplayNextPeriodStart,
		"DisplayNextPeriodRangeStart":           viewData.CycleContext.DisplayNextPeriodRangeStart,
		"DisplayNextPeriodRangeEnd":             viewData.CycleContext.DisplayNextPeriodRangeEnd,
		"DisplayNextPeriodUseRange":             viewData.CycleContext.DisplayNextPeriodUseRange,
		"DisplayNextPeriodPrompt":               viewData.CycleContext.DisplayNextPeriodPrompt,
		"DisplayNextPeriodNeedsData":            viewData.CycleContext.DisplayNextPeriodNeedsData,
		"DisplayOvulationDate":                  viewData.CycleContext.DisplayOvulationDate,
		"DisplayOvulationRangeStart":            viewData.CycleContext.DisplayOvulationRangeStart,
		"DisplayOvulationRangeEnd":              viewData.CycleContext.DisplayOvulationRangeEnd,
		"DisplayOvulationUseRange":              viewData.CycleContext.DisplayOvulationUseRange,
		"DisplayOvulationNeedsData":             viewData.CycleContext.DisplayOvulationNeedsData,
		"DisplayOvulationExact":                 viewData.CycleContext.DisplayOvulationExact,
		"DisplayOvulationImpossible":            viewData.CycleContext.DisplayOvulationImpossible,
		"NextPeriodInPast":                      viewData.CycleContext.NextPeriodInPast,
		"OvulationInPast":                       viewData.CycleContext.OvulationInPast,
		"Today":                                 viewData.Today.Format("2006-01-02"),
		"TodayDateRaw":                          viewData.Today,
		"Yesterday":                             viewData.Yesterday.Format("2006-01-02"),
		"YesterdayMonth":                        viewData.YesterdayMonth,
		"FormattedDate":                         viewData.FormattedDate,
		"TodayEntry":                            viewData.TodayLog,
		"TodayLog":                              viewData.TodayLog,
		"TodayHasData":                          viewData.TodayHasData,
		"TodayEntryExists":                      viewData.TodayEntryExists,
		"Symptoms":                              viewData.Symptoms,
		"PrimarySymptoms":                       viewData.PrimarySymptoms,
		"ExtraSymptoms":                         viewData.ExtraSymptoms,
		"HasExtraSymptoms":                      viewData.HasExtraSymptoms,
		"SelectedSymptomID":                     viewData.SelectedSymptomID,
		"CycleFactorKeys":                       services.SupportedDayCycleFactorKeys(),
		"SelectedCycleFactorKey":                services.DayCycleFactorKeySet(viewData.TodayLog.CycleFactorKeys),
		"ShowYesterdayJump":                     viewData.ShowYesterdayJump,
		"ShowSexChip":                           viewData.ShowSexChip,
		"ShowBBTField":                          viewData.ShowBBTField,
		"TemperatureUnit":                       bbtView.Unit,
		"TemperatureUnitSymbol":                 bbtView.Symbol,
		"TemperatureInputMin":                   bbtView.Min,
		"TemperatureInputMax":                   bbtView.Max,
		"TemperatureRangeHint":                  bbtView.RangeHint,
		"TemperatureRangeError":                 bbtView.RangeError,
		"ShowCervicalMucus":                     viewData.ShowCervicalMucus,
		"AllowManualCycleStart":                 viewData.AllowManualCycleStart,
		"ManualCycleStartConflictDate":          viewData.ManualCycleStartPolicy.ConflictDate,
		"ManualCycleStartConflict":              !viewData.ManualCycleStartPolicy.ConflictDate.IsZero(),
		"ManualCycleStartConflictISO":           viewData.ManualCycleStartPolicy.ConflictDate.Format("2006-01-02"),
		"ManualCycleStartPreviousDate":          viewData.ManualCycleStartPolicy.PreviousStart,
		"ManualCycleStartShortGap":              viewData.ManualCycleStartPolicy.ShortGapDays,
		"ManualCycleStartPreviousISO":           viewData.ManualCycleStartPolicy.PreviousStart.Format("2006-01-02"),
		"ManualCycleStartPotentialImplantation": viewData.ManualCycleStartPolicy.PotentialImplantation,
		"ShowHighFertilityBadge":                viewData.ShowHighFertilityBadge,
		"ShowMissedDaysLink":                    viewData.ShowMissedDaysLink,
		"MissedDay":                             viewData.MissedDay.Format("2006-01-02"),
		"MissedDayMonth":                        viewData.MissedDay.Format("2006-01"),
		"ShowCycleStartSuggestion":              viewData.ShowCycleStartSuggestion,
		"ShowSpottingCycleWarning":              viewData.ShowSpottingCycleWarning,
		"PredictionExplanationPrimaryKey":       viewData.PredictionExplanationPrimaryKey,
		"PredictionExplanationSecondaryKey":     viewData.PredictionExplanationSecondaryKey,
		"HasPredictionExplanationPrimary":       viewData.HasPredictionExplanationPrimary,
		"HasPredictionExplanationSecondary":     viewData.HasPredictionExplanationSecondary,
		"PredictionFactorHintKeys":              viewData.PredictionFactorHintKeys,
		"HasPredictionFactorHint":               viewData.HasPredictionFactorHint,
		"IsOwner":                               viewData.IsOwner,
	}
	return data, nil
}

func (handler *Handler) buildDayEditorPartialData(user *models.User, language string, messages map[string]string, day time.Time, now time.Time, location *time.Location, editMode bool) (fiber.Map, error) {
	viewData, err := handler.dashboardViewService.BuildDayEditorViewData(user, language, day, now, location)
	if err != nil {
		return nil, err
	}
	bbtView := buildBBTFieldViewData(messages, user.TemperatureUnit)

	payload := fiber.Map{
		"Date":                                  viewData.Date,
		"DateString":                            viewData.DateString,
		"DateLabel":                             viewData.DateLabel,
		"IsFutureDate":                          viewData.IsFutureDate,
		"NoDataLabel":                           translateMessage(messages, "common.not_available"),
		"Log":                                   viewData.Log,
		"Symptoms":                              viewData.Symptoms,
		"PrimarySymptoms":                       viewData.PrimarySymptoms,
		"ExtraSymptoms":                         viewData.ExtraSymptoms,
		"HasExtraSymptoms":                      viewData.HasExtraSymptoms,
		"SelectedSymptomID":                     viewData.SelectedSymptomID,
		"CycleFactorKeys":                       services.SupportedDayCycleFactorKeys(),
		"SelectedCycleFactorKey":                services.DayCycleFactorKeySet(viewData.Log.CycleFactorKeys),
		"HasDayData":                            viewData.HasDayData,
		"ShowSexChip":                           viewData.ShowSexChip,
		"ShowBBTField":                          viewData.ShowBBTField,
		"TemperatureUnit":                       bbtView.Unit,
		"TemperatureUnitSymbol":                 bbtView.Symbol,
		"TemperatureInputMin":                   bbtView.Min,
		"TemperatureInputMax":                   bbtView.Max,
		"TemperatureRangeHint":                  bbtView.RangeHint,
		"TemperatureRangeError":                 bbtView.RangeError,
		"ShowCervicalMucus":                     viewData.ShowCervicalMucus,
		"AllowManualCycleStart":                 viewData.AllowManualCycleStart,
		"ManualCycleStartConflictDate":          viewData.ManualCycleStartPolicy.ConflictDate,
		"ManualCycleStartConflict":              !viewData.ManualCycleStartPolicy.ConflictDate.IsZero(),
		"ManualCycleStartConflictISO":           viewData.ManualCycleStartPolicy.ConflictDate.Format("2006-01-02"),
		"ManualCycleStartPreviousDate":          viewData.ManualCycleStartPolicy.PreviousStart,
		"ManualCycleStartShortGap":              viewData.ManualCycleStartPolicy.ShortGapDays,
		"ManualCycleStartPreviousISO":           viewData.ManualCycleStartPolicy.PreviousStart.Format("2006-01-02"),
		"ManualCycleStartPotentialImplantation": viewData.ManualCycleStartPolicy.PotentialImplantation,
		"ShowFutureCycleStartNotice":            viewData.ShowFutureCycleStartNotice,
		"ShowCycleStartSuggestion":              viewData.ShowCycleStartSuggestion,
		"ShowSpottingCycleWarning":              viewData.ShowSpottingCycleWarning,
		"EditMode":                              editMode,
		"IsOwner":                               viewData.IsOwner,
	}
	return payload, nil
}

type bbtFieldViewData struct {
	Unit       string
	Symbol     string
	Min        string
	Max        string
	RangeHint  string
	RangeError string
}

func buildBBTFieldViewData(messages map[string]string, unit string) bbtFieldViewData {
	resolvedUnit := services.NormalizeTemperatureUnit(unit)
	min, max := services.TemperatureUnitRange(resolvedUnit)
	symbol := services.TemperatureUnitSymbol(resolvedUnit)
	minLabel := fmt.Sprintf("%.2f", min)
	maxLabel := fmt.Sprintf("%.2f", max)

	return bbtFieldViewData{
		Unit:       resolvedUnit,
		Symbol:     symbol,
		Min:        minLabel,
		Max:        maxLabel,
		RangeHint:  formatBBTLocalizedMessage(messages, "dashboard.bbt_range_hint", "Allowed range: %s-%s %s.", minLabel, maxLabel, symbol),
		RangeError: formatBBTLocalizedMessage(messages, "dashboard.bbt_range_error", "Enter a value between %s and %s %s.", minLabel, maxLabel, symbol),
	}
}

func formatBBTLocalizedMessage(messages map[string]string, key string, fallback string, min string, max string, symbol string) string {
	pattern := translateMessage(messages, key)
	if pattern == "" || pattern == key {
		pattern = fallback
	}
	return fmt.Sprintf(pattern, min, max, symbol)
}
