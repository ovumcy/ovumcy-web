package services

import "testing"

// The truncation flag compares the UNCAPPED axis with the DOM bound: a cycle or
// start window that fits reports false, one that runs past the bound reports
// true even though AxisDays itself is clamped to the bound.
func TestDashboardCycleHeroUncappedAxisDaysExposesWhatTheCapCuts(t *testing.T) {
	fits := dashboardCycleHeroUncappedAxisDays(28, dashboardCycleHeroDaySpan{StartDay: 27, EndDay: 34, Present: true})
	if fits > dashboardCycleHeroMaxAxisDays {
		t.Fatalf("a 34-day axis fits the bound, got %d", fits)
	}

	cut := dashboardCycleHeroUncappedAxisDays(70, dashboardCycleHeroDaySpan{})
	if cut <= dashboardCycleHeroMaxAxisDays {
		t.Fatalf("a 70-day cycle outruns the bound, got %d", cut)
	}
	if got := dashboardCycleHeroAxisDays(70, dashboardCycleHeroDaySpan{}); got != dashboardCycleHeroMaxAxisDays {
		t.Fatalf("the rendered axis stays clamped to %d, got %d", dashboardCycleHeroMaxAxisDays, got)
	}

	windowCut := dashboardCycleHeroUncappedAxisDays(28, dashboardCycleHeroDaySpan{StartDay: 40, EndDay: 400, Present: true})
	if windowCut <= dashboardCycleHeroMaxAxisDays {
		t.Fatalf("a window reaching day 400 outruns the bound, got %d", windowCut)
	}
}
