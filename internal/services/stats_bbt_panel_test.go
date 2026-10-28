package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestStatsBBTChartBelowTheMinimumKeepsTheReadingCount pins the data behind the
// "need N readings" hint: a chart that is not drawn still says how many
// readings the cycle has and how many it takes.
func TestStatsBBTChartBelowTheMinimumKeepsTheReadingCount(t *testing.T) {
	stats := CycleStats{LastPeriodStart: statscycleinsightsCovDay(t, "2026-01-01")}
	now := statscycleinsightsCovDay(t, "2026-01-14")
	logs := []models.DailyLog{
		statscycleinsightsCovBBTLog(t, "2026-01-01", 36.20),
		statscycleinsightsCovBBTLog(t, "2026-01-02", 36.25),
		statscycleinsightsCovBBTLog(t, "2026-01-03", 36.30),
	}

	chart := buildCurrentCycleBBTChart("en", stats, logs, now, time.UTC)
	if len(chart.Labels) != 0 {
		t.Fatalf("expected no drawn series below the minimum, got %d labels", len(chart.Labels))
	}
	if chart.ReadingCount != 3 || chart.ReadingsRequired != 5 {
		t.Fatalf("expected 3 of 5 readings, got %d of %d", chart.ReadingCount, chart.ReadingsRequired)
	}

	tracking := buildOwnerCurrentCycleBBTChart(&models.User{Role: models.RoleOwner, TrackBBT: true}, "en", stats, logs, now, time.UTC)
	if tracking.ReadingsRequired != 5 {
		t.Fatalf("expected a BBT-tracking owner to keep the hint data, got %d", tracking.ReadingsRequired)
	}
	notTracking := buildOwnerCurrentCycleBBTChart(&models.User{Role: models.RoleOwner}, "en", stats, logs, now, time.UTC)
	if notTracking.ReadingsRequired != 0 {
		t.Fatalf("expected no hint for an owner who does not track BBT, got %d", notTracking.ReadingsRequired)
	}
}

// TestStatsBBTChartMarkerCarriesItsDate pins that the marker names its day: the
// date rides on the chart and the matching table point is flagged.
func TestStatsBBTChartMarkerCarriesItsDate(t *testing.T) {
	stats := CycleStats{LastPeriodStart: statscycleinsightsCovDay(t, "2026-01-01")}
	now := statscycleinsightsCovDay(t, "2026-01-14")
	logs := []models.DailyLog{
		statscycleinsightsCovBBTLog(t, "2026-01-01", 36.20),
		statscycleinsightsCovBBTLog(t, "2026-01-02", 36.25),
		statscycleinsightsCovBBTLog(t, "2026-01-03", 36.30),
		statscycleinsightsCovBBTLog(t, "2026-01-04", 36.22),
		statscycleinsightsCovBBTLog(t, "2026-01-05", 36.24),
		statscycleinsightsCovBBTLog(t, "2026-01-06", 36.21),
		statscycleinsightsCovBBTLog(t, "2026-01-10", 36.45),
		statscycleinsightsCovBBTLog(t, "2026-01-11", 36.50),
		statscycleinsightsCovBBTLog(t, "2026-01-12", 36.55),
	}

	chart := buildCurrentCycleBBTChart("en", stats, logs, now, time.UTC)
	if !chart.HasMarker {
		t.Fatal("expected a marker for a detected shift")
	}
	want := LocalizedDateShort("en", statscycleinsightsCovDay(t, "2026-01-09"))
	if chart.MarkerDate != want {
		t.Fatalf("expected the marker dated %q, got %q", want, chart.MarkerDate)
	}
	for index, point := range chart.Points {
		if point.IsMarker != (index == chart.MarkerIndex) {
			t.Fatalf("point %d: IsMarker=%v, marker index %d", index, point.IsMarker, chart.MarkerIndex)
		}
	}
	if got := chart.BaselineText(); got != "36.3" {
		t.Fatalf("expected the coverline to print at the chart's precision, got %q", got)
	}
}
