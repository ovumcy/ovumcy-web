package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// statsBBTUnitFixture is one cycle with a gap on day 4 and a detected shift on
// day 8. The control reading on day 3 is also the coverline: 36.5 °C, which is
// 97.7 °F, so the reading, its text and the coverline name one number per unit.
func statsBBTUnitFixture(t *testing.T) StatsBBTChartViewData {
	t.Helper()

	stats := CycleStats{LastPeriodStart: statscycleinsightsCovDay(t, "2026-01-01")}
	now := statscycleinsightsCovDay(t, "2026-01-12")
	logs := []models.DailyLog{
		statscycleinsightsCovBBTLog(t, "2026-01-01", 36.30),
		statscycleinsightsCovBBTLog(t, "2026-01-02", 36.40),
		statscycleinsightsCovBBTLog(t, "2026-01-03", 36.50),
		statscycleinsightsCovBBTLog(t, "2026-01-05", 36.35),
		statscycleinsightsCovBBTLog(t, "2026-01-06", 36.45),
		statscycleinsightsCovBBTLog(t, "2026-01-07", 36.40),
		statscycleinsightsCovBBTLog(t, "2026-01-08", 36.70),
		statscycleinsightsCovBBTLog(t, "2026-01-09", 36.75),
		statscycleinsightsCovBBTLog(t, "2026-01-10", 36.80),
	}

	chart := buildCurrentCycleBBTChart("en", stats, logs, now, time.UTC)
	if !chart.HasBaseline || chart.Baseline != 36.5 || !chart.HasMarker || len(chart.Values) != 10 {
		t.Fatalf("fixture premise: expected a 10-day series with a 36.5 coverline and a marker, got baseline=%v/%v marker=%v days=%d",
			chart.HasBaseline, chart.Baseline, chart.HasMarker, len(chart.Values))
	}
	return chart
}

// TestStatsBBTChartSpeaksTheOwnersTemperatureUnit pins that every number the
// BBT chart hands the page — the plotted value, its text, the coverline — and
// the unit label naming them are in the owner's unit, never stored Celsius
// under a Fahrenheit label or the reverse. Detection ran on stored units before
// the conversion, so the marker must not move with the unit, and the
// stored-unit chart the conversion started from is left as it was.
func TestStatsBBTChartSpeaksTheOwnersTemperatureUnit(t *testing.T) {
	stored := statsBBTUnitFixture(t)

	cases := []struct {
		unit     string
		labelKey string
		control  float64
		text     string
	}{
		{unit: TemperatureUnitCelsius, labelKey: "stats.bbt_unit", control: 36.5, text: "36.5"},
		{unit: TemperatureUnitFahrenheit, labelKey: "stats.bbt_unit_fahrenheit", control: 97.7, text: "97.7"},
	}
	for _, tc := range cases {
		t.Run(tc.unit, func(t *testing.T) {
			chart := stored.inTemperatureUnit(tc.unit)

			if got := chart.UnitLabelKey(); got != tc.labelKey {
				t.Fatalf("unit label key = %q, want %q", got, tc.labelKey)
			}
			if chart.Values[2] == nil || *chart.Values[2] != tc.control {
				t.Fatalf("control reading on day 3 = %v, want %v", chart.Values[2], tc.control)
			}
			if got := chart.Points[2].ValueText; got != tc.text {
				t.Fatalf("control reading text on day 3 = %q, want %q", got, tc.text)
			}
			if !chart.HasBaseline || chart.Baseline != tc.control {
				t.Fatalf("coverline = %v (has=%v), want %v", chart.Baseline, chart.HasBaseline, tc.control)
			}
			if chart.MarkerIndex != stored.MarkerIndex || chart.HasMarker != stored.HasMarker {
				t.Fatalf("marker moved with the unit: %d/%v, stored %d/%v", chart.MarkerIndex, chart.HasMarker, stored.MarkerIndex, stored.HasMarker)
			}
		})
	}

	if *stored.Values[2] != 36.5 || stored.Points[2].ValueText != "36.5" || stored.Baseline != 36.5 || stored.UnitLabelKey() != "stats.bbt_unit" {
		t.Fatalf("the display conversion rewrote the stored-unit chart: value=%v text=%q coverline=%v key=%q",
			*stored.Values[2], stored.Points[2].ValueText, stored.Baseline, stored.UnitLabelKey())
	}
}
