package services

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestBuildCalendarLegendNamesOnlyTheStatesTheGridCarries(t *testing.T) {
	cases := []struct {
		name string
		day  CalendarDayState
		want CalendarLegend
	}{
		{name: "no day carries anything", day: CalendarDayState{}, want: CalendarLegend{}},
		{name: "recorded period", day: CalendarDayState{IsPeriod: true}, want: CalendarLegend{PeriodRecorded: true}},
		{name: "projected period", day: CalendarDayState{IsPredicted: true}, want: CalendarLegend{PredictedPeriod: true}},
		{name: "start window", day: CalendarDayState{IsPredictedStartWindow: true}, want: CalendarLegend{StartWindow: true}},
		{name: "fertile edge", day: CalendarDayState{IsFertilityEdge: true}, want: CalendarLegend{Fertility: true}},
		{name: "fertile peak", day: CalendarDayState{IsFertilityPeak: true}, want: CalendarLegend{Fertility: true}},
		{name: "projected period inside the fertile window", day: CalendarDayState{IsPredicted: true, IsFertilityEdge: true, IsPredictedFertileOverlap: true}, want: CalendarLegend{PredictedPeriod: true, Fertility: true, PredictedPeriodInFertility: true}},
		{name: "start window inside the fertile window", day: CalendarDayState{IsPredictedStartWindow: true, IsFertilityPeak: true}, want: CalendarLegend{StartWindow: true, Fertility: true, PredictedPeriodInFertility: true}},
		{name: "solid ovulation", day: CalendarDayState{IsOvulation: true}, want: CalendarLegend{OvulationEstimate: true}},
		{name: "tentative ovulation", day: CalendarDayState{IsTentativeOvulation: true}, want: CalendarLegend{OvulationTentative: true}},
		{name: "today", day: CalendarDayState{IsToday: true}, want: CalendarLegend{Today: true}},
		{name: "logged entry", day: CalendarDayState{HasData: true}, want: CalendarLegend{LoggedEntry: true}},
		{name: "logged sex", day: CalendarDayState{HasSex: true}, want: CalendarLegend{LoggedEntry: true}},
		{name: "pre-fertile day", day: CalendarDayState{IsPreFertile: true}, want: CalendarLegend{PreFertile: true}},
		{name: "pre-fertile day repainted by the fertile window", day: CalendarDayState{IsPreFertile: true, IsFertilityEdge: true}, want: CalendarLegend{Fertility: true}},
		{name: "pre-fertile day repainted by a projected period", day: CalendarDayState{IsPreFertile: true, IsPredicted: true}, want: CalendarLegend{PredictedPeriod: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildCalendarLegend([]CalendarDayState{{}, tc.day, {}})
			if got != tc.want {
				t.Fatalf("legend = %+v, want %+v", got, tc.want)
			}
			if got.Any() != (tc.want != CalendarLegend{}) {
				t.Fatalf("Any() = %t for %+v", got.Any(), got)
			}
		})
	}
}

// TestOpenAPIDescribesAConfirmedOvulationAsAnInferenceNotAMeasurement pins the
// wording of the two stats properties that carry a BBT-confirmed day: the
// published API says the day is inferred from the owner's recorded signal, so
// no client reads it as a measured ovulation.
func TestOpenAPIDescribesAConfirmedOvulationAsAnInferenceNotAMeasurement(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test file path: runtime.Caller failed")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "..", "..", "docs", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read docs/openapi.yaml: %v", err)
	}
	spec := strings.Join(strings.Fields(string(raw)), " ")

	cases := []struct {
		name   string
		anchor string
		phrase string
	}{
		{"ovulation_date", "ovulation_date: type: [string, \"null\"] format: date description: >-", "an inference from a recorded signal, never a measurement of the ovulation itself"},
		{"ovulation_confirmed", "ovulation_confirmed: type: boolean description: >-", "`ovulation_date` is inferred from the owner's own temperature shift"},
	}
	for _, tc := range cases {
		start := strings.Index(spec, tc.anchor)
		if start < 0 {
			t.Fatalf("%s: property block not found in docs/openapi.yaml", tc.name)
		}
		block := spec[start+len(tc.anchor):]
		if next := strings.Index(block, " type:"); next >= 0 {
			// The block runs to the next property's type line, so a phrase
			// elsewhere in the document cannot satisfy this property.
			block = block[:next]
		}
		if !strings.Contains(block, tc.phrase) {
			t.Errorf("%s description lost the inference wording %q", tc.name, tc.phrase)
		}
	}
}

func TestCalendarLegendFollowsTheGridForAnOwnerWhosePredictionsAreWithheld(t *testing.T) {
	monthStart := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	// One completed cycle in irregular-cycle mode: the grid draws no projected
	// mark, so the legend may not list one.
	logs := statsThresholdCycleLogs(t, []string{"2026-01-01", "2026-01-29"})
	owner := &models.User{ID: 21, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, IrregularCycle: true}

	stats := BuildCycleStatsFromLogs(owner, logs, now, time.UTC)
	days := BuildCalendarDayStates(owner, monthStart, logs, stats, now, time.UTC)
	got := BuildCalendarLegend(days)

	if got.PredictedPeriod || got.StartWindow || got.Fertility || got.PredictedPeriodInFertility || got.OvulationEstimate || got.OvulationTentative {
		t.Fatalf("legend lists a projected state the grid withholds for one completed cycle: %+v", got)
	}
	if !got.PeriodRecorded || !got.Today || !got.LoggedEntry {
		t.Fatalf("legend dropped a state the grid does draw: %+v", got)
	}
}

func TestCalendarLegendListsTheProjectedStatesOnceTheGridDrawsThem(t *testing.T) {
	monthStart := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
	logs := statsThresholdCycleLogs(t, []string{"2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26"})
	owner := &models.User{ID: 22, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5}

	stats := BuildCycleStatsFromLogs(owner, logs, now, time.UTC)
	days := BuildCalendarDayStates(owner, monthStart, logs, stats, now, time.UTC)
	got := BuildCalendarLegend(days)

	if !got.PredictedPeriod || !got.Fertility || !got.OvulationEstimate {
		t.Fatalf("legend omits a projected state the grid draws after three completed cycles: %+v", got)
	}
	if got.OvulationTentative {
		t.Fatalf("legend promises the temperature dash to an owner who does not track temperature: %+v", got)
	}
}
