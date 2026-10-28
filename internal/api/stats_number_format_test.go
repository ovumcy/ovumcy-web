package api

import (
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

var statsLanguages = []string{"en", "de", "es", "fr", "it", "ru"}

func TestStatsCycleChartSummaryHasNoDoubleStop(t *testing.T) {
	for _, language := range statsLanguages {
		got := buildStatsCycleChartSummary(mustLocaleMessages(t, language), language, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{26, 35}, RangeMin: 26, RangeMax: 35},
			ChartBaseline: 31,
		})
		if strings.Contains(got, "..") {
			t.Fatalf("%s summary has a doubled full stop: %q", language, got)
		}
	}
}

func TestStatsCycleChartSummaryAverageUsesTheLanguageSeparator(t *testing.T) {
	for language, want := range map[string]string{"en": "30.5", "de": "30,5", "fr": "30,5", "ru": "30,5", "es": "30,5", "it": "30,5"} {
		got := buildStatsCycleChartSummary(mustLocaleMessages(t, language), language, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{26, 35}, RangeMin: 26, RangeMax: 35},
			ChartBaseline: 30.5,
		})
		if !strings.Contains(got, want+" ") {
			t.Fatalf("%s summary should read the average as %q, got %q", language, want, got)
		}
	}
}

func TestStatsBBTChartSummaryPrintsTheCoverlineWithLocaleSeparatorAndUnitSpace(t *testing.T) {
	reading := 36.6
	chart := services.StatsBBTChartViewData{
		Values:      []*float64{&reading},
		HasBaseline: true,
		Baseline:    36.6,
	}
	got := buildStatsBBTChartSummary(mustLocaleMessages(t, "fr"), "fr", chart)
	if !strings.Contains(got, "36,6\xc2\xa0°C") {
		t.Fatalf("expected a comma and a no-break space before the unit, got %q", got)
	}
}

func TestStatsCycleChartSummaryNamesAUniformSeriesOnce(t *testing.T) {
	for _, language := range statsLanguages {
		got := buildStatsCycleChartSummary(mustLocaleMessages(t, language), language, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{28, 28, 28}, RangeMin: 28, RangeMax: 28},
			ChartBaseline: 28,
		})
		if count := strings.Count(got, "28"); count != 1 {
			t.Fatalf("%s: uniform series should name its length once, got %d times: %q", language, count, got)
		}
	}
}
