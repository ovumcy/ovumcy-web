package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

const maxStatsTrendPoints = 12

func mapStatsChartData(chart services.StatsChartViewData) fiber.Map {
	payload := fiber.Map{
		"labels": chart.Labels,
		"values": chart.Values,
	}
	if chart.Kind != "" {
		payload["kind"] = chart.Kind
	}
	if chart.HasBaseline {
		payload["baseline"] = chart.Baseline
	}
	return payload
}

func (handler *Handler) buildStatsPageData(user *models.User, language string, messages map[string]string, now time.Time) (fiber.Map, error) {
	cycleLabelPattern := translateMessage(messages, "stats.cycle_label")
	if cycleLabelPattern == "stats.cycle_label" {
		cycleLabelPattern = ""
	}

	viewData, err := handler.statsService.BuildStatsPageViewData(
		user,
		language,
		cycleLabelPattern,
		now,
		handler.location,
		maxStatsTrendPoints,
	)
	if err != nil {
		return nil, err
	}

	data := fiber.Map{
		"Title":                               localizedPageTitle(messages, "meta.title.stats", "Ovumcy | Stats"),
		"CurrentUser":                         user,
		"Stats":                               viewData.Stats,
		"ChartData":                           mapStatsChartData(viewData.ChartData),
		"ChartBaseline":                       viewData.ChartBaseline,
		"TrendPointCount":                     viewData.TrendPointCount,
		"HasObservedCycleData":                viewData.Flags.HasObservedCycleData,
		"HasTrendData":                        viewData.Flags.HasTrendData,
		"HasInsights":                         viewData.Flags.HasInsights,
		"HasReliableTrend":                    viewData.Flags.HasReliableTrend,
		"CycleDataStale":                      viewData.Flags.CycleDataStale,
		"CompletedCycleCount":                 viewData.Flags.CompletedCycleCount,
		"InsightProgress":                     viewData.Flags.InsightProgress,
		"SymptomCounts":                       viewData.SymptomCounts,
		"PhaseMoodInsights":                   viewData.PhaseMoodInsights,
		"PhaseSymptomInsights":                viewData.PhaseSymptomInsights,
		"HasPhaseMoodInsights":                viewData.HasPhaseMoodInsights,
		"HasPhaseSymptomInsights":             viewData.HasPhaseSymptomInsights,
		"ShowIrregularityNotice":              viewData.ShowIrregularityNotice,
		"ShowIrregularInsufficientDataNotice": viewData.ShowIrregularInsufficientDataNotice,
		"IsIrregularMode":                     viewData.IsIrregularMode,
		"IsOwner":                             viewData.IsOwner,
	}
	return data, nil
}
