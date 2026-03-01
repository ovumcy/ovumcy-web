package api

import (
	"github.com/terraincognita07/ovumcy/internal/services"
)

func buildCycleTrendLabels(messages map[string]string, pointCount int) []string {
	cycleLabelPattern := translateMessage(messages, "stats.cycle_label")
	if cycleLabelPattern == "stats.cycle_label" {
		cycleLabelPattern = ""
	}
	return services.BuildCycleTrendLabels(cycleLabelPattern, pointCount)
}

func localizeSymptomFrequencySummaries(language string, counts []SymptomCount) {
	for index := range counts {
		counts[index].FrequencySummary = services.LocalizedSymptomFrequencySummary(language, counts[index].Count, counts[index].TotalDays)
	}
}
