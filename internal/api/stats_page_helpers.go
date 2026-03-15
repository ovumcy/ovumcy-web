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

func mapStatsBBTChartData(chart services.StatsBBTChartViewData, messages map[string]string) fiber.Map {
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
	if chart.HasMarker {
		payload["markerIndex"] = chart.MarkerIndex
		if chart.MarkerLabelKey != "" {
			payload["markerLabel"] = translateMessage(messages, chart.MarkerLabelKey)
		}
	}
	return payload
}

func (handler *Handler) buildStatsPageData(user *models.User, language string, messages map[string]string, now time.Time, location *time.Location) (fiber.Map, error) {
	cycleLabelPattern := translateMessage(messages, "stats.cycle_label")
	if cycleLabelPattern == "stats.cycle_label" {
		cycleLabelPattern = ""
	}

	viewData, err := handler.statsService.BuildStatsPageViewData(
		user,
		language,
		cycleLabelPattern,
		now,
		location,
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
		"PredictionSampleCount":               viewData.PredictionSampleCount,
		"PredictionSampleUsesRecentWindow":    viewData.PredictionSampleUsesRecentWindow,
		"PredictionReliabilityLabelKey":       viewData.PredictionReliabilityLabelKey,
		"PredictionReliabilityHintKey":        viewData.PredictionReliabilityHintKey,
		"ShowPredictionReliability":           viewData.ShowPredictionReliability,
		"LastCycleSymptoms":                   viewData.LastCycleSymptoms,
		"SymptomPatterns":                     viewData.SymptomPatterns,
		"SymptomCounts":                       viewData.SymptomCounts,
		"BBTChartData":                        mapStatsBBTChartData(viewData.CurrentCycleBBTChart, messages),
		"PhaseMoodInsights":                   viewData.PhaseMoodInsights,
		"PhaseSymptomInsights":                viewData.PhaseSymptomInsights,
		"HasLastCycleSymptoms":                viewData.HasLastCycleSymptoms,
		"HasSymptomPatterns":                  viewData.HasSymptomPatterns,
		"HasCurrentCycleBBTChart":             viewData.HasCurrentCycleBBTChart,
		"HasPhaseMoodInsights":                viewData.HasPhaseMoodInsights,
		"HasPhaseSymptomInsights":             viewData.HasPhaseSymptomInsights,
		"ShowIrregularityNotice":              viewData.ShowIrregularityNotice,
		"ShowIrregularInsufficientDataNotice": viewData.ShowIrregularInsufficientDataNotice,
		"ShowIrregularModeRecommendation":     viewData.ShowIrregularModeRecommendation,
		"ShowAgeVariabilityHint":              viewData.ShowAgeVariabilityHint,
		"PredictionDisabled":                  viewData.PredictionDisabled,
		"IsIrregularMode":                     viewData.IsIrregularMode,
		"IsOwner":                             viewData.IsOwner,
	}
	return data, nil
}
