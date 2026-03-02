package api

import (
	"errors"
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
	if chart.HasBaseline {
		payload["baseline"] = chart.Baseline
	}
	return payload
}

func (handler *Handler) buildStatsPageData(user *models.User, language string, messages map[string]string, now time.Time) (fiber.Map, string, error) {
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
		switch {
		case errors.Is(err, services.ErrStatsPageViewLoadSymptoms):
			return nil, "failed to load symptom stats", err
		default:
			return nil, "failed to load stats", err
		}
	}

	data := fiber.Map{
		"Title":                localizedPageTitle(messages, "meta.title.stats", "Ovumcy | Stats"),
		"CurrentUser":          user,
		"Stats":                viewData.Stats,
		"ChartData":            mapStatsChartData(viewData.ChartData),
		"ChartBaseline":        viewData.ChartBaseline,
		"TrendPointCount":      viewData.TrendPointCount,
		"HasObservedCycleData": viewData.Flags.HasObservedCycleData,
		"HasTrendData":         viewData.Flags.HasTrendData,
		"HasReliableTrend":     viewData.Flags.HasReliableTrend,
		"CycleDataStale":       viewData.Flags.CycleDataStale,
		"SymptomCounts":        viewData.SymptomCounts,
		"IsOwner":              viewData.IsOwner,
	}
	return data, "", nil
}
