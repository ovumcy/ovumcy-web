package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Mutation-kill tests for stats_page_helpers.go. These target survivors left by
// the existing blocking_p0_stats_helpers_test.go where an assertion gap let a
// mutant live (e.g. "3 readings" is a substring of "-3 readings", so the
// readingsCount-- mutant survived a Contains check; the min/max fallback OR only
// pinned one operand). Each case here uses an injected pattern or a
// bracket-delimited count so the mutated value cannot masquerade as the expected
// output.

// TestBuildStatsCycleChartSummaryMutKill pins the cycle-summary conditionals.
func TestBuildStatsCycleChartSummaryMutKill(t *testing.T) {
	// L58 (CONDITIONALS_BOUNDARY, `<= 0` on BOTH bounds): a single non-positive
	// bound must still pull BOTH announced range bounds to the latest value.
	// Changing either `<=` to `<` leaves the stray 0 in the range.
	t.Run("max bound zero still falls back to latest", func(t *testing.T) {
		got := buildStatsCycleChartSummary(map[string]string{}, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{26, 34}},
			ChartBaseline: 0,
			Stats:         services.CycleStats{MinCycleLength: 29, MaxCycleLength: 0},
		})
		if !strings.Contains(got, "Range 34 to 34") {
			t.Fatalf("max=0 must trigger latest-value fallback for both bounds, got %q", got)
		}
		if strings.Contains(got, "to 0") {
			t.Fatalf("a stray 0 bound must not survive into the range, got %q", got)
		}
	})

	t.Run("min bound zero still falls back to latest", func(t *testing.T) {
		got := buildStatsCycleChartSummary(map[string]string{}, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{26, 34}},
			ChartBaseline: 0,
			Stats:         services.CycleStats{MinCycleLength: 0, MaxCycleLength: 31},
		})
		if !strings.Contains(got, "Range 34 to 34") {
			t.Fatalf("min=0 must trigger latest-value fallback for both bounds, got %q", got)
		}
		if strings.Contains(got, "Range 0") {
			t.Fatalf("a stray 0 bound must not survive into the range, got %q", got)
		}
	})

	// L65 (CONDITIONALS_NEGATION, two operators): in the baseline branch, a
	// present translation must be used verbatim. Negating either operator forces
	// the hardcoded default pattern instead, dropping the injected marker.
	t.Run("baseline branch uses present translation", func(t *testing.T) {
		got := buildStatsCycleChartSummary(map[string]string{
			"stats.cycle_chart_summary": "CY[%d] latest=%d %s avg=%d %s min=%d max=%d %s",
			"common.days_short":         "d",
		}, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{27, 31}},
			ChartBaseline: 29,
			Stats:         services.CycleStats{MinCycleLength: 27, MaxCycleLength: 31},
		})
		if !strings.Contains(got, "CY[2]") {
			t.Fatalf("baseline branch must use the injected translation, got %q", got)
		}
	})

	// L82 (CONDITIONALS_NEGATION, two operators): same for the no-baseline branch.
	t.Run("no-baseline branch uses present translation", func(t *testing.T) {
		got := buildStatsCycleChartSummary(map[string]string{
			"stats.cycle_chart_summary_no_baseline": "NOBASE[%d] latest=%d %s min=%d max=%d %s",
			"common.days_short":                     "d",
		}, services.StatsPageViewData{
			Flags:         services.StatsFlags{HasTrendData: true},
			ChartData:     services.StatsChartViewData{Values: []int{27, 33}},
			ChartBaseline: 0,
			Stats:         services.CycleStats{MinCycleLength: 27, MaxCycleLength: 33},
		})
		if !strings.Contains(got, "NOBASE[2]") {
			t.Fatalf("no-baseline branch must use the injected translation, got %q", got)
		}
	})
}

// TestBuildStatsBBTChartSummaryMutKill pins the BBT-summary conditionals.
func TestBuildStatsBBTChartSummaryMutKill(t *testing.T) {
	f := func(v float64) *float64 { return &v }

	// L100 (INCREMENT_DECREMENT) + L117 (CONDITIONALS_NEGATION, two operators):
	// the reading count must be the number of non-nil values (3), and the present
	// translation must be used. A bracket-delimited count is asserted so the
	// decrement mutant's "-3" cannot masquerade as "3".
	t.Run("no-marker branch counts non-nil readings with present translation", func(t *testing.T) {
		got := buildStatsBBTChartSummary(map[string]string{
			"stats.bbt_chart_summary": "CNT[%d] base=%.2f %s",
			"stats.bbt_unit":          "u",
		}, services.StatsBBTChartViewData{
			Values:      []*float64{f(36.4), nil, f(36.6), f(36.8)},
			Baseline:    36.5,
			HasBaseline: true,
		})
		if !strings.Contains(got, "CNT[3]") {
			t.Fatalf("expected 3 non-nil readings via the injected translation, got %q", got)
		}
		if strings.Contains(got, "CNT[-3]") {
			t.Fatalf("decremented count must not survive, got %q", got)
		}
	})

	// L110 (CONDITIONALS_NEGATION, two operators): the marker branch must use its
	// present translation. Negating either operator forces the default pattern.
	t.Run("marker branch uses present translation", func(t *testing.T) {
		got := buildStatsBBTChartSummary(map[string]string{
			"stats.bbt_chart_summary_with_marker": "MARK[%d] base=%.2f %s label=%s",
			"stats.bbt_unit":                      "u",
			"stats.ovulation_marker":              "Ov",
		}, services.StatsBBTChartViewData{
			Values:         []*float64{f(36.4), f(36.6)},
			Baseline:       36.5,
			HasBaseline:    true,
			MarkerLabelKey: "stats.ovulation_marker",
			HasMarker:      true,
		})
		if !strings.Contains(got, "MARK[2]") {
			t.Fatalf("marker branch must use the injected translation, got %q", got)
		}
	})
}

type mutkillStatsDayReader struct {
	logsForRange []models.DailyLog
	logsForAll   []models.DailyLog
}

func (r *mutkillStatsDayReader) FetchLogsForUser(_ context.Context, _ uint, _ time.Time, _ time.Time, _ *time.Location) ([]models.DailyLog, error) {
	return r.logsForRange, nil
}

func (r *mutkillStatsDayReader) FetchAllLogsForUser(_ context.Context, _ uint) ([]models.DailyLog, error) {
	return r.logsForAll, nil
}

type mutkillStatsSymptomReader struct{}

func (r *mutkillStatsSymptomReader) CalculateFrequencies(_ context.Context, _ uint, _ []models.DailyLog) ([]services.SymptomFrequency, error) {
	return nil, nil
}

func (r *mutkillStatsSymptomReader) FetchSymptoms(_ context.Context, _ uint) ([]models.SymptomType, error) {
	return nil, nil
}

// TestBuildStatsPageDataCycleLabelPatternMutKill pins buildStatsPageData L125
// (`cycleLabelPattern == "stats.cycle_label"`). The handler normalizes a MISSING
// translation (key echoed) to "" so the service uses its clean "Cycle %d"
// default; a PRESENT translation must be passed through so the chart labels are
// localized. Negating to `!=` discards a real translation (falling back to the
// English default) and leaks the raw i18n key when the translation is missing.
// English is invisible here because en "stats.cycle_label" == "Cycle %d" (the
// default), so a distinct pattern ("Zyklus %d") is injected.
func TestBuildStatsPageDataCycleLabelPatternMutKill(t *testing.T) {
	day := func(iso string) time.Time {
		parsed, err := time.ParseInLocation("2006-01-02", iso, time.UTC)
		if err != nil {
			t.Fatalf("parse day %q: %v", iso, err)
		}
		return parsed
	}
	periodLogs := []models.DailyLog{
		{Date: day("2026-01-01"), IsPeriod: true},
		{Date: day("2026-01-29"), IsPeriod: true},
		{Date: day("2026-02-26"), IsPeriod: true},
		{Date: day("2026-03-26"), IsPeriod: true},
	}
	handler := &Handler{statsService: services.NewStatsService(
		&mutkillStatsDayReader{logsForRange: periodLogs, logsForAll: []models.DailyLog{{ID: 1}}},
		&mutkillStatsSymptomReader{},
	)}
	user := &models.User{ID: 7, Role: models.RoleOwner, CycleLength: 28}
	now := day("2026-04-10")

	labelsFor := func(t *testing.T, messages map[string]string) []string {
		t.Helper()
		data, err := handler.buildStatsPageData(context.Background(), user, "en", messages, now, time.UTC)
		if err != nil {
			t.Fatalf("buildStatsPageData: %v", err)
		}
		chartData, ok := data["ChartData"].(fiber.Map)
		if !ok {
			t.Fatalf("ChartData is not a fiber.Map: %T", data["ChartData"])
		}
		labels, ok := chartData["labels"].([]string)
		if !ok || len(labels) == 0 {
			t.Fatalf("expected non-empty chart labels, got %#v", chartData["labels"])
		}
		return labels
	}

	// Present translation that differs from the default -> must localize the
	// labels. The mutant discards it and falls back to "Cycle 1".
	t.Run("present translation localizes labels", func(t *testing.T) {
		labels := labelsFor(t, map[string]string{"stats.cycle_label": "Zyklus %d"})
		if labels[0] != "Zyklus 1" {
			t.Fatalf("present translation must localize the first label, got %q", labels[0])
		}
	})

	// Missing translation -> normalized to the clean "Cycle %d" default. The
	// mutant keeps the raw key, leaking "stats.cycle_label%!(EXTRA...)".
	t.Run("missing translation normalizes to clean default", func(t *testing.T) {
		labels := labelsFor(t, map[string]string{})
		if labels[0] != "Cycle 1" {
			t.Fatalf("missing translation must yield the clean default label, got %q", labels[0])
		}
	})
}
