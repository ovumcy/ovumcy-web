package api

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
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

func buildStatsCycleChartSummary(messages map[string]string, viewData services.StatsPageViewData) string {
	if !viewData.Flags.HasTrendData || len(viewData.ChartData.Values) == 0 {
		return translateMessage(messages, "stats.no_cycle_data")
	}

	daysShort := translateMessage(messages, "common.days_short")
	latestCycleLength := viewData.ChartData.Values[len(viewData.ChartData.Values)-1]
	minCycleLength := viewData.Stats.MinCycleLength
	maxCycleLength := viewData.Stats.MaxCycleLength
	if minCycleLength <= 0 || maxCycleLength <= 0 {
		minCycleLength = latestCycleLength
		maxCycleLength = latestCycleLength
	}

	if viewData.ChartBaseline > 0 {
		pattern := translateMessage(messages, "stats.cycle_chart_summary")
		if pattern == "" || pattern == "stats.cycle_chart_summary" {
			pattern = "%d completed cycles shown. Latest cycle %d %s. Average %d %s. Range %d to %d %s."
		}
		return fmt.Sprintf(
			pattern,
			len(viewData.ChartData.Values),
			latestCycleLength,
			daysShort,
			viewData.ChartBaseline,
			daysShort,
			minCycleLength,
			maxCycleLength,
			daysShort,
		)
	}

	pattern := translateMessage(messages, "stats.cycle_chart_summary_no_baseline")
	if pattern == "" || pattern == "stats.cycle_chart_summary_no_baseline" {
		pattern = "%d completed cycles shown. Latest cycle %d %s. Range %d to %d %s."
	}
	return fmt.Sprintf(
		pattern,
		len(viewData.ChartData.Values),
		latestCycleLength,
		daysShort,
		minCycleLength,
		maxCycleLength,
		daysShort,
	)
}

func buildStatsBBTChartSummary(messages map[string]string, chart services.StatsBBTChartViewData) string {
	readingsCount := 0
	for _, value := range chart.Values {
		if value != nil {
			readingsCount++
		}
	}
	if readingsCount == 0 {
		return translateMessage(messages, "stats.no_cycle_data")
	}

	unit := translateMessage(messages, "stats.bbt_unit")
	if chart.HasMarker && chart.MarkerLabelKey != "" {
		pattern := translateMessage(messages, "stats.bbt_chart_summary_with_marker")
		if pattern == "" || pattern == "stats.bbt_chart_summary_with_marker" {
			pattern = "%d readings this cycle. Baseline %.2f %s. Marker: %s."
		}
		return fmt.Sprintf(pattern, readingsCount, chart.Baseline, unit, translateMessage(messages, chart.MarkerLabelKey))
	}

	pattern := translateMessage(messages, "stats.bbt_chart_summary")
	if pattern == "" || pattern == "stats.bbt_chart_summary" {
		pattern = "%d readings this cycle. Baseline %.2f %s."
	}
	return fmt.Sprintf(pattern, readingsCount, chart.Baseline, unit)
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

	usageGoalLabelKey := services.UsageGoalTranslationKey(user.UsageGoal)
	usageGoalSummaryKey := services.UsageGoalSummaryTranslationKey(user.UsageGoal)
	cycleChartSummary := buildStatsCycleChartSummary(messages, viewData)
	bbtChartSummary := buildStatsBBTChartSummary(messages, viewData.CurrentCycleBBTChart)

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
		"PredictionExplanationPrimaryKey":     viewData.PredictionExplanationPrimaryKey,
		"PredictionExplanationSecondaryKey":   viewData.PredictionExplanationSecondaryKey,
		"HasPredictionExplanationPrimary":     viewData.HasPredictionExplanationPrimary,
		"HasPredictionExplanationSecondary":   viewData.HasPredictionExplanationSecondary,
		"RecentCycleFactors":                  viewData.RecentCycleFactors,
		"HasRecentCycleFactors":               viewData.HasRecentCycleFactors,
		"CycleFactorPatternSummaries":         viewData.CycleFactorPatternSummaries,
		"HasCycleFactorPatternSummaries":      viewData.HasCycleFactorPatternSummaries,
		"RecentFactorCycles":                  viewData.RecentFactorCycles,
		"HasRecentFactorCycles":               viewData.HasRecentFactorCycles,
		"PredictionFactorHintKeys":            viewData.PredictionFactorHintKeys,
		"HasPredictionFactorHint":             viewData.HasPredictionFactorHint,
		"CycleFactorWindowDays":               services.StatsCycleFactorContextWindowDays(),
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
		"UsageGoalLabelKey":                   usageGoalLabelKey,
		"UsageGoalSummaryKey":                 usageGoalSummaryKey,
		"CycleChartSummary":                   cycleChartSummary,
		"BBTChartSummary":                     bbtChartSummary,
		"IsOwner":                             viewData.IsOwner,
	}
	return data, nil
}
