package services

import (
	"reflect"
	"testing"
)

func TestDashboardCycleHeroScaleLabelsDayOneTodayAndTheEndWithoutCrowding(t *testing.T) {
	for name, testCase := range map[string]struct {
		axisDays   int
		currentDay int
		want       []int
	}{
		"today mid-cycle":           {axisDays: 30, currentDay: 16, want: []int{1, 16, 30}},
		"today on day one":          {axisDays: 30, currentDay: 1, want: []int{1, 30}},
		"today crowds day one":      {axisDays: 30, currentDay: 3, want: []int{3, 30}},
		"today just clears day one": {axisDays: 30, currentDay: 4, want: []int{1, 4, 30}},
		"today crowds the end":      {axisDays: 30, currentDay: 28, want: []int{1, 28}},
		"today on the last day":     {axisDays: 30, currentDay: 30, want: []int{1, 30}},
		"today off a capped axis":   {axisDays: 60, currentDay: 61, want: []int{1, 60}},
	} {
		t.Run(name, func(t *testing.T) {
			days := make([]DashboardCycleHeroDay, testCase.axisDays)
			for index := range days {
				days[index].Day = index + 1
			}
			dashboardCycleHeroMarkScale(days, testCase.currentDay)

			var labelled []int
			for _, day := range days {
				if day.ScaleLabel {
					labelled = append(labelled, day.Day)
				}
			}
			if !reflect.DeepEqual(labelled, testCase.want) {
				t.Fatalf("scale labels = %v, want %v", labelled, testCase.want)
			}
		})
	}
}

func TestDashboardCycleHeroEmptyAxisDrawsNoScaleAndNoRing(t *testing.T) {
	dashboardCycleHeroMarkScale(nil, 5)
	if ring := dashboardCycleHeroRingFor(nil, 28, 5); ring.Visible || len(ring.Arcs) != 0 || ring.TodayOnRing {
		t.Fatalf("ring over an empty axis = %+v, want the zero ring", ring)
	}
	if ring := dashboardCycleHeroRingFor([]DashboardCycleHeroDay{{Day: 1, Phase: "menstrual"}}, 0, 1); ring.Visible {
		t.Fatalf("ring over a zero cycle length = %+v, want the zero ring", ring)
	}
}

func TestDashboardCycleHeroTodayMarkerStaysInsideTheRibbon(t *testing.T) {
	for currentDay, want := range map[int]string{1: "start", 3: "start", 4: "center", 27: "center", 28: "end", 30: "end"} {
		if got := dashboardCycleHeroTodayMarkerAlign(currentDay, 30); got != want {
			t.Errorf("day %d of 30: align = %q, want %q", currentDay, got, want)
		}
	}
}

// The legend is the ribbon in words, so it lists exactly what the cells draw:
// a phase appears once, in axis order, and a withheld tier lists no phase the
// cells withhold.
func TestDashboardCycleHeroLegendNamesOnlyWhatTheCellsDraw(t *testing.T) {
	days := []DashboardCycleHeroDay{
		{Day: 1, Phase: "menstrual", IsLogged: true},
		{Day: 2, Phase: "menstrual", IsPredictedFlow: true},
		{Day: 3, Phase: "follicular", IsFertile: true},
		{Day: 4, Phase: "ovulation", IsFertile: true},
		{Day: 5, Phase: "luteal"},
		{Day: 6, Phase: "luteal", IsStartWindow: true},
		{Day: 7, Phase: phaseBeyondProjectedCycle, IsStartWindow: true},
	}
	want := DashboardCycleHeroLegend{
		Phases:            []string{"menstrual", "follicular", "ovulation", "luteal"},
		Fertile:           DashboardCycleHeroDayRange{StartDay: 3, EndDay: 4},
		PredictedFlow:     DashboardCycleHeroDayRange{StartDay: 2, EndDay: 2},
		StartWindow:       DashboardCycleHeroDayRange{StartDay: 6, EndDay: 7},
		StartWindowPhases: []string{"luteal", phaseBeyondProjectedCycle},
		Logged:            true,
	}
	if got := dashboardCycleHeroLegendFor(days); !reflect.DeepEqual(got, want) {
		t.Fatalf("legend = %+v, want %+v", got, want)
	}
	if !want.PredictedFlow.SingleDay() || want.Fertile.SingleDay() {
		t.Fatal("SingleDay must hold for a one-day range only")
	}

	withheld := []DashboardCycleHeroDay{
		{Day: 1, Phase: "menstrual"},
		{Day: 2, Phase: phaseFertilityWithheld},
		{Day: 3, Phase: phaseFertilityWithheld},
	}
	got := dashboardCycleHeroLegendFor(withheld)
	if !reflect.DeepEqual(got.Phases, []string{"menstrual", phaseFertilityWithheld}) || got.Fertile.Present() {
		t.Fatalf("withheld legend = %+v, want menstrual + withheld and no fertile window", got)
	}
}
