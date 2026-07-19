package api

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

var statsChartDataPattern = regexp.MustCompile(`data-chart='([^']+)'`)

type statsChartPayload struct {
	Labels []string `json:"labels"`
	Values []int    `json:"values"`
}

func extractStatsChartPayload(rendered string) (statsChartPayload, error) {
	matches := statsChartDataPattern.FindStringSubmatch(rendered)
	if len(matches) != 2 {
		return statsChartPayload{}, fmt.Errorf("data-chart attribute not found")
	}

	rawJSON := html.UnescapeString(matches[1])
	payload := statsChartPayload{}
	if err := json.Unmarshal([]byte(rawJSON), &payload); err != nil {
		return statsChartPayload{}, err
	}
	return payload, nil
}

// TestMapStatsChartData pins which optional keys the cycle-length chart payload
// carries. Kind and baseline are added only when populated (mutation survivors:
// the `Kind != ""` and `HasBaseline` guards), so each row asserts key presence
// AND value, and that an empty Kind / cleared HasBaseline omits the key. The
// payload feeds the client chart, so an accidental always-on baseline would
// draw a phantom average line.
func TestMapStatsChartData(t *testing.T) {
	t.Parallel()

	t.Run("all optional fields populated", func(t *testing.T) {
		t.Parallel()
		payload := mapStatsChartData(services.StatsChartViewData{
			Labels:      []string{"C1", "C2"},
			Values:      []int{28, 30},
			Baseline:    29,
			HasBaseline: true,
			Kind:        "cycle",
		})
		if kind, ok := payload["kind"]; !ok || kind != "cycle" {
			t.Fatalf("expected kind=cycle, got %v (present=%v)", kind, ok)
		}
		if baseline, ok := payload["baseline"]; !ok || baseline != 29 {
			t.Fatalf("expected baseline=29, got %v (present=%v)", baseline, ok)
		}
	})

	t.Run("empty kind and no baseline omit keys", func(t *testing.T) {
		t.Parallel()
		payload := mapStatsChartData(services.StatsChartViewData{
			Labels:      []string{"C1"},
			Values:      []int{28},
			Baseline:    29, // present but not flagged -> must be omitted
			HasBaseline: false,
			Kind:        "",
		})
		if _, ok := payload["kind"]; ok {
			t.Fatalf("expected kind key omitted when Kind empty, got %v", payload["kind"])
		}
		if _, ok := payload["baseline"]; ok {
			t.Fatalf("expected baseline key omitted when HasBaseline false, got %v", payload["baseline"])
		}
	})
}

// TestMapStatsBBTChartData pins the BBT chart payload's optional keys: Kind,
// baseline, and the marker pair (markerIndex + markerLabel). The marker label
// is only emitted when both HasMarker and a non-empty MarkerLabelKey hold and
// is resolved through the message map, matching the surviving guards on the
// `Kind`, `HasBaseline`, `HasMarker`, and `MarkerLabelKey != ""` branches.
func TestMapStatsBBTChartData(t *testing.T) {
	t.Parallel()

	messages := map[string]string{"stats.ovulation_marker": "Ovulation"}

	t.Run("marker with label emits index and translated label", func(t *testing.T) {
		t.Parallel()
		payload := mapStatsBBTChartData(services.StatsBBTChartViewData{
			Labels:         []string{"D1", "D2"},
			Baseline:       36.5,
			HasBaseline:    true,
			Kind:           "bbt",
			MarkerIndex:    1,
			MarkerLabelKey: "stats.ovulation_marker",
			HasMarker:      true,
		}, messages)
		if payload["kind"] != "bbt" {
			t.Fatalf("expected kind=bbt, got %v", payload["kind"])
		}
		if payload["baseline"] != 36.5 {
			t.Fatalf("expected baseline=36.5, got %v", payload["baseline"])
		}
		if payload["markerIndex"] != 1 {
			t.Fatalf("expected markerIndex=1, got %v", payload["markerIndex"])
		}
		if payload["markerLabel"] != "Ovulation" {
			t.Fatalf("expected translated markerLabel=Ovulation, got %v", payload["markerLabel"])
		}
	})

	t.Run("marker without label key omits marker label", func(t *testing.T) {
		t.Parallel()
		payload := mapStatsBBTChartData(services.StatsBBTChartViewData{
			Labels:         []string{"D1"},
			MarkerIndex:    2,
			MarkerLabelKey: "",
			HasMarker:      true,
		}, messages)
		if payload["markerIndex"] != 2 {
			t.Fatalf("expected markerIndex=2, got %v", payload["markerIndex"])
		}
		if _, ok := payload["markerLabel"]; ok {
			t.Fatalf("expected markerLabel omitted without a label key, got %v", payload["markerLabel"])
		}
	})

	t.Run("no marker omits both marker keys", func(t *testing.T) {
		t.Parallel()
		payload := mapStatsBBTChartData(services.StatsBBTChartViewData{
			Labels:    []string{"D1"},
			HasMarker: false,
		}, messages)
		if _, ok := payload["markerIndex"]; ok {
			t.Fatalf("expected markerIndex omitted without a marker, got %v", payload["markerIndex"])
		}
		if _, ok := payload["markerLabel"]; ok {
			t.Fatalf("expected markerLabel omitted without a marker, got %v", payload["markerLabel"])
		}
	})
}

// TestBuildStatsCycleChartSummary pins the accessible text summary for the
// cycle-length chart. It exercises the empty-data guard, the min/max <= 0
// fallback to the latest value, and both the baseline and no-baseline sentence
// shapes (distinct fmt argument lists). Each surviving conditional in
// buildStatsCycleChartSummary changes the numbers the summary announces, so the
// assertions check the rendered figures, not just non-emptiness.
func TestBuildStatsCycleChartSummary(t *testing.T) {
	t.Parallel()

	noData := map[string]string{"stats.no_cycle_data": "No cycle data yet"}

	t.Run("no trend data returns no-data message", func(t *testing.T) {
		t.Parallel()
		got := buildStatsCycleChartSummary(noData, services.StatsPageViewData{
			Flags:     services.StatsFlags{HasTrendData: false},
			ChartData: services.StatsChartViewData{Values: []int{28}},
		})
		if got != "No cycle data yet" {
			t.Fatalf("expected no-data message, got %q", got)
		}
	})

	t.Run("empty values returns no-data message", func(t *testing.T) {
		t.Parallel()
		got := buildStatsCycleChartSummary(noData, services.StatsPageViewData{
			Flags:     services.StatsFlags{HasTrendData: true},
			ChartData: services.StatsChartViewData{Values: []int{}},
		})
		if got != "No cycle data yet" {
			t.Fatalf("expected no-data message for empty values, got %q", got)
		}
	})

	t.Run("with baseline announces average and range", func(t *testing.T) {
		t.Parallel()
		got := buildStatsCycleChartSummary(map[string]string{}, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{27, 31}},
			ChartBaseline: 29,
			Stats:         services.CycleStats{MinCycleLength: 27, MaxCycleLength: 31},
		})
		// Default pattern: "%d completed cycles shown. Latest cycle %d %s.
		// Average %d %s. Range %d to %d %s." -> 2 cycles, latest 31, avg 29,
		// range 27 to 31.
		for _, want := range []string{"2 completed", "Latest cycle 31", "Average 29", "Range 27 to 31"} {
			if !strings.Contains(got, want) {
				t.Fatalf("baseline summary %q missing %q", got, want)
			}
		}
	})

	t.Run("zero min max falls back to latest value", func(t *testing.T) {
		t.Parallel()
		got := buildStatsCycleChartSummary(map[string]string{}, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{27, 33}},
			ChartBaseline: 0, // no baseline -> shorter sentence
			Stats:         services.CycleStats{MinCycleLength: 0, MaxCycleLength: 0},
		})
		// No-baseline pattern: "%d completed cycles shown. Latest cycle %d %s.
		// Range %d to %d %s." with min=max=latest=33.
		for _, want := range []string{"2 completed", "Latest cycle 33", "Range 33 to 33"} {
			if !strings.Contains(got, want) {
				t.Fatalf("no-baseline fallback summary %q missing %q", got, want)
			}
		}
		if strings.Contains(got, "Average") {
			t.Fatalf("no-baseline summary must not announce an average, got %q", got)
		}
	})

	t.Run("single non-positive bound still triggers latest fallback", func(t *testing.T) {
		t.Parallel()
		// Only the min bound is non-positive: the OR guard must still fall back
		// to the latest value for BOTH bounds (an AND guard would leave the
		// stray 0 in the announced range).
		got := buildStatsCycleChartSummary(map[string]string{}, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{26, 34}},
			ChartBaseline: 0,
			Stats:         services.CycleStats{MinCycleLength: 0, MaxCycleLength: 31},
		})
		if !strings.Contains(got, "Range 34 to 34") {
			t.Fatalf("expected latest-value fallback range 34 to 34 when min bound is 0, got %q", got)
		}
	})
}

// TestBuildStatsBBTChartSummary pins the BBT chart's text summary: the
// non-nil reading count, the zero-readings guard, and the marker vs no-marker
// sentence shapes. The surviving conditionals (`value != nil`, the
// readingsCount++ increment, `readingsCount == 0`, and the marker guard) each
// change the announced count or whether the marker clause appears.
func TestBuildStatsBBTChartSummary(t *testing.T) {
	t.Parallel()

	messages := map[string]string{"stats.ovulation_marker": "Ovulation"}
	f := func(v float64) *float64 { return &v }

	t.Run("no readings returns no-data message", func(t *testing.T) {
		t.Parallel()
		got := buildStatsBBTChartSummary(map[string]string{"stats.no_cycle_data": "No cycle data yet"}, services.StatsBBTChartViewData{
			Values: []*float64{nil, nil},
		})
		if got != "No cycle data yet" {
			t.Fatalf("expected no-data message, got %q", got)
		}
	})

	t.Run("counts only non-nil readings without marker", func(t *testing.T) {
		t.Parallel()
		got := buildStatsBBTChartSummary(messages, services.StatsBBTChartViewData{
			Values:      []*float64{f(36.4), nil, f(36.6), f(36.8)},
			Baseline:    36.5,
			HasBaseline: true,
		})
		// Default: "%d readings this cycle. Coverline %.2f %s." -> 3 readings.
		if !strings.Contains(got, "3 readings") {
			t.Fatalf("expected 3 non-nil readings counted, got %q", got)
		}
		if strings.Contains(got, "Marker") {
			t.Fatalf("did not expect a marker clause without a marker, got %q", got)
		}
	})

	t.Run("marker clause appended when marker present", func(t *testing.T) {
		t.Parallel()
		got := buildStatsBBTChartSummary(messages, services.StatsBBTChartViewData{
			Values:         []*float64{f(36.4), f(36.6)},
			Baseline:       36.5,
			HasBaseline:    true,
			MarkerLabelKey: "stats.ovulation_marker",
			HasMarker:      true,
		})
		if !strings.Contains(got, "2 readings") {
			t.Fatalf("expected 2 readings counted, got %q", got)
		}
		if !strings.Contains(got, "Ovulation") {
			t.Fatalf("expected translated marker label in summary, got %q", got)
		}
	})

	t.Run("no detected shift announces readings without a coverline value", func(t *testing.T) {
		t.Parallel()
		got := buildStatsBBTChartSummary(messages, services.StatsBBTChartViewData{
			Values: []*float64{f(36.4), f(36.6)},
		})
		// Default: "%d readings this cycle. No temperature shift detected yet."
		if !strings.Contains(got, "2 readings") {
			t.Fatalf("expected 2 readings counted, got %q", got)
		}
		if strings.Contains(got, "0.00") {
			t.Fatalf("a zero coverline must never be announced, got %q", got)
		}
	})
}
