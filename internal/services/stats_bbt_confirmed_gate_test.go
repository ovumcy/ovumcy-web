package services

// stats_bbt_confirmed_gate_test.go — the stats page's BBT chart draws a
// probable-ovulation marker and a coverline, which name the day the owner's
// temperatures confirmed. The dashboard, the calendar and the JSON API withhold
// that day under unpredictable-cycle mode, a pregnancy pause and the
// first-cycle floor; the chart drew it under all three. The readings are a
// different matter: they are recorded facts and stay on the chart whatever the
// verdict.

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// statsPageBBTChart is the chart the stats page hands its template, read from
// the owner-insights builder the page itself calls.
func statsPageBBTChart(t *testing.T, user *models.User, logs []models.DailyLog, stats CycleStats, today time.Time) StatsBBTChartViewData {
	t.Helper()

	insights, err := (&StatsService{}).buildOwnerStatsInsights(context.Background(), user, "en", stats, logs, nil, today, time.UTC)
	if err != nil {
		t.Fatalf("buildOwnerStatsInsights: %v", err)
	}
	return insights.currentCycleBBTChart
}

// statsBBTChartMarkerDayKey names the calendar day the chart's marker sits on:
// chart index 0 is the cycle start, as the labels number it.
func statsBBTChartMarkerDayKey(stats CycleStats, chart StatsBBTChartViewData) string {
	if !chart.HasMarker {
		return ""
	}
	return CalendarDayKey(AddCalendarDays(CalendarDay(stats.LastPeriodStart, time.UTC), chart.MarkerIndex, time.UTC))
}

func statsBBTChartReadingCount(chart StatsBBTChartViewData) int {
	count := 0
	for _, value := range chart.Values {
		if value != nil {
			count++
		}
	}
	return count
}

func TestStatsBBTChartInterpretsTheShiftOnlyWhereTheConfirmedDayIsNamed(t *testing.T) {
	controlUser, controlLogs, controlStats, controlToday := projectedWindowFixture(t)
	control := statsPageBBTChart(t, controlUser, controlLogs, controlStats, controlToday)
	if !control.HasMarker || !control.HasBaseline || statsBBTChartMarkerDayKey(controlStats, control) != confirmedVerdictDayKey {
		t.Fatalf("control: marker=%t on %q coverline=%t, want both drawn on %s",
			control.HasMarker, statsBBTChartMarkerDayKey(controlStats, control), control.HasBaseline, confirmedVerdictDayKey)
	}
	readings := statsBBTChartReadingCount(control)
	if readings == 0 {
		t.Fatal("control: the chart carries no readings")
	}

	for _, testCase := range confirmedVerdictRows() {
		t.Run(confirmedVerdictRowName(testCase.signal), func(t *testing.T) {
			user, logs, stats, today := projectedWindowFixture(t)
			testCase.suppress(user, &stats, &today)

			chart := statsPageBBTChart(t, user, logs, stats, today)
			if chart.HasMarker != testCase.wantKept || chart.HasBaseline != testCase.wantKept {
				t.Fatalf("marker=%t coverline=%t, want both %t", chart.HasMarker, chart.HasBaseline, testCase.wantKept)
			}
			if testCase.wantKept && statsBBTChartMarkerDayKey(stats, chart) != confirmedVerdictDayKey {
				t.Fatalf("marker on %q, want %s", statsBBTChartMarkerDayKey(stats, chart), confirmedVerdictDayKey)
			}
			if !testCase.wantKept && (chart.MarkerIndex != 0 || chart.Baseline != 0) {
				t.Fatalf("withheld chart still carries markerIndex=%d coverline=%v", chart.MarkerIndex, chart.Baseline)
			}
			if got := statsBBTChartReadingCount(chart); got != readings {
				t.Fatalf("chart carries %d readings, want the %d recorded", got, readings)
			}
		})
	}
}
