package services

import "testing"

// Round-2 mutation-survivor kill-tests for dashboard_cycle_hero.go.
//
// These pin the arithmetic at dashboardCycleHeroMarkerAtDay (lines 168-169),
// where the marker X/Y coordinates are computed. The existing coverage tests
// only probe degenerate angles (sin/cos == 0 or ±1), at which the + and *
// operators coincide with their - and / mutants; these tests use a
// non-degenerate angle so the ARITHMETIC_BASE mutants are distinguished.

// TestDashboardcycleheroCovMarkerXUsesPlusRadiusCosine kills the ARITHMETIC_BASE
// mutant at line 168 (centerX + radius*cos -> centerX - radius*cos).
func TestDashboardcycleheroCovMarkerXUsesPlusRadiusCosine(t *testing.T) {
	// dayIndex=7, cycleLength=28 -> ratio=0.25 -> angle=0 -> cos(angle)=1.
	// Original X = centerX + radius*cos = 110 + 78*1 = 188.0.
	// The +->- mutation would yield 110 - 78 = 32.0.
	marker := dashboardCycleHeroMarkerAtDay(7, 28)
	if marker.X != "188.0" {
		t.Fatalf("expected marker X 188.0 (centerX + radius*cos at angle 0), got %q", marker.X)
	}
}

// TestDashboardcycleheroCovMarkerYUsesRadiusTimesSine kills the ARITHMETIC_BASE
// mutant at line 169 (centerY + radius*sin -> centerY + radius/sin).
func TestDashboardcycleheroCovMarkerYUsesRadiusTimesSine(t *testing.T) {
	// dayIndex=7, cycleLength=21 -> ratio=1/3 -> angle=pi/6 -> sin(angle)=0.5.
	// Original Y = centerY + radius*sin = 110 + 78*0.5 = 149.0.
	// The *->/ mutation would yield 110 + 78/0.5 = 266.0.
	// Existing tests only check Y where sin=+/-1, at which * and / coincide,
	// so a non-degenerate sine is required to distinguish the mutation.
	marker := dashboardCycleHeroMarkerAtDay(7, 21)
	if marker.Y != "149.0" {
		t.Fatalf("expected marker Y 149.0 (centerY + radius*sin at angle pi/6), got %q", marker.Y)
	}
}
