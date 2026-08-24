package api

import (
	"context"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
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
	if dates, valueTexts := statsBBTChartPointColumns(chart); len(dates) > 0 {
		payload["dates"] = dates
		payload["valueTexts"] = valueTexts
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
			// The rendered label rides along for the chart script, but a test
			// asserting the marker is the ovulation marker must not have to
			// re-type English copy that the catalogue owns and translation can
			// change. The key is the stable half of that pair, exactly as the
			// data-explainer-key attributes do for rendered notices.
			payload["markerLabelKey"] = chart.MarkerLabelKey
		}
	}
	return payload
}

// statsBBTChartPointColumns flattens the point list into the two per-index
// columns the chart script reads for its crosshair: the calendar date the x
// axis has no room for, and the reading already rendered as text. The readout
// prints the server's string instead of re-rounding the float in the browser,
// so it cannot disagree with the table twin printed underneath it. A day with
// no reading carries an empty string and the readout says so.
//
// With no points there are no columns, and the keys stay off the payload
// entirely rather than shipping a row of blanks.
func statsBBTChartPointColumns(chart services.StatsBBTChartViewData) ([]string, []string) {
	if len(chart.Points) == 0 {
		return nil, nil
	}

	dates := make([]string, 0, len(chart.Points))
	valueTexts := make([]string, 0, len(chart.Points))
	for _, point := range chart.Points {
		dates = append(dates, point.Date)
		valueTexts = append(valueTexts, point.ValueText)
	}
	return dates, valueTexts
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
		pattern, translated := lookupMessage(messages, "stats.cycle_chart_summary")
		if !translated {
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

	pattern, translated := lookupMessage(messages, "stats.cycle_chart_summary_no_baseline")
	if !translated {
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

	if !chart.HasBaseline {
		pattern, translated := lookupMessage(messages, "stats.bbt_chart_summary_no_shift")
		if !translated {
			pattern = "%d readings this cycle. No temperature shift detected yet."
		}
		return fmt.Sprintf(pattern, readingsCount)
	}

	unit := translateMessage(messages, "stats.bbt_unit")
	if chart.HasMarker && chart.MarkerLabelKey != "" {
		pattern, translated := lookupMessage(messages, "stats.bbt_chart_summary_with_marker")
		if !translated {
			pattern = "%d readings this cycle. Coverline %.2f %s. Marker: %s."
		}
		return fmt.Sprintf(pattern, readingsCount, chart.Baseline, unit, translateMessage(messages, chart.MarkerLabelKey))
	}

	pattern, translated := lookupMessage(messages, "stats.bbt_chart_summary")
	if !translated {
		pattern = "%d readings this cycle. Coverline %.2f %s."
	}
	return fmt.Sprintf(pattern, readingsCount, chart.Baseline, unit)
}

func (handler *Handler) buildStatsPageData(ctx context.Context, user *models.User, language string, messages map[string]string, now time.Time, location *time.Location) (fiber.Map, error) {
	cycleLabelPattern, translated := lookupMessage(messages, "stats.cycle_label")
	if !translated {
		cycleLabelPattern = ""
	}

	viewData, err := handler.statsService.BuildStatsPageViewData(
		ctx,
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
		"BBTChartPoints":                      viewData.CurrentCycleBBTChart.Points,
		"CycleRibbon":                         viewData.CycleRibbon,
		"PhaseMoodInsights":                   viewData.PhaseMoodInsights,
		"PhaseSymptomInsights":                viewData.PhaseSymptomInsights,
		"Statements":                          viewData.Statements,
		"HasStatements":                       viewData.HasStatements,
		"HasLastCycleSymptoms":                viewData.HasLastCycleSymptoms,
		"HasSymptomPatterns":                  viewData.HasSymptomPatterns,
		"HasCurrentCycleBBTChart":             viewData.HasCurrentCycleBBTChart,
		"HasPhaseMoodInsights":                viewData.HasPhaseMoodInsights,
		"HasPhaseSymptomInsights":             viewData.HasPhaseSymptomInsights,
		"ShowIrregularityNotice":              viewData.ShowIrregularityNotice,
		"ShowIrregularInsufficientDataNotice": viewData.ShowIrregularInsufficientDataNotice,
		"ShowShortCycleNotice":                viewData.ShowShortCycleNotice,
		"ShowLongCycleNotice":                 viewData.ShowLongCycleNotice,
		"ShowPerimenopauseHint":               viewData.ShowPerimenopauseHint,
		"PredictionDisabled":                  viewData.PredictionDisabled,
		"IsIrregularMode":                     viewData.IsIrregularMode,
		"CycleChartSummary":                   cycleChartSummary,
		"BBTChartSummary":                     bbtChartSummary,
		"IsOwner":                             viewData.IsOwner,
	}
	return data, nil
}
