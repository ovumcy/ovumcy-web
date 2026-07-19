package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// mr3statsOwner returns a minimal owner user; callers tweak cycle flags.
func mr3statsOwner() *models.User {
	return &models.User{Role: models.RoleOwner}
}

// mr3statsRegularStats returns CycleStats whose length spread is regular
// (spread <= irregularCycleSpreadDays = 7), so IsIrregularCycleSpread == false.
func mr3statsRegularStats() CycleStats {
	return CycleStats{MinCycleLength: 28, MaxCycleLength: 30}
}

// --- buildStatsPredictionReliability ---------------------------------------

// TestMR3Stats_ReliabilityVariableAtThreeCycles pins line 337
// (variablePattern && sampleCount >= minimumPhaseInsightCycles). A variable
// owner with exactly minimumPhaseInsightCycles (3) completed cycles must yield
// "stats.reliability.variable". Mutant `> 3` would drop to "building".
func TestMR3Stats_ReliabilityVariableAtThreeCycles(t *testing.T) {
	user := mr3statsOwner()
	user.IrregularCycle = true
	flags := StatsFlags{CompletedCycleCount: minimumPhaseInsightCycles}

	_, _, labelKey, _, ok := buildStatsPredictionReliability(user, flags, mr3statsRegularStats())

	if !ok {
		t.Fatalf("expected reliability to be available")
	}
	if labelKey != "stats.reliability.variable" {
		t.Fatalf("expected stats.reliability.variable at %d cycles, got %q", minimumPhaseInsightCycles, labelKey)
	}
}

// TestMR3Stats_ReliabilityStableAtWindow pins line 339
// (sampleCount >= cyclePredictionWindow). A non-variable owner with exactly
// cyclePredictionWindow (6) completed cycles must yield
// "stats.reliability.stable". Mutant `> 6` would drop to "building".
func TestMR3Stats_ReliabilityStableAtWindow(t *testing.T) {
	user := mr3statsOwner()
	flags := StatsFlags{CompletedCycleCount: cyclePredictionWindow}

	sampleCount, _, labelKey, _, ok := buildStatsPredictionReliability(user, flags, mr3statsRegularStats())

	if !ok {
		t.Fatalf("expected reliability to be available")
	}
	if sampleCount != cyclePredictionWindow {
		t.Fatalf("expected sampleCount %d, got %d", cyclePredictionWindow, sampleCount)
	}
	if labelKey != "stats.reliability.stable" {
		t.Fatalf("expected stats.reliability.stable at %d cycles, got %q", cyclePredictionWindow, labelKey)
	}
}

// TestMR3Stats_ReliabilityBuildingAtThreeCycles pins line 341
// (sampleCount >= minimumPhaseInsightCycles). A non-variable owner with exactly
// minimumPhaseInsightCycles (3) completed cycles must yield
// "stats.reliability.building". Mutant `> 3` would stay "early".
func TestMR3Stats_ReliabilityBuildingAtThreeCycles(t *testing.T) {
	user := mr3statsOwner()
	flags := StatsFlags{CompletedCycleCount: minimumPhaseInsightCycles}

	_, _, labelKey, _, ok := buildStatsPredictionReliability(user, flags, mr3statsRegularStats())

	if !ok {
		t.Fatalf("expected reliability to be available")
	}
	if labelKey != "stats.reliability.building" {
		t.Fatalf("expected stats.reliability.building at %d cycles, got %q", minimumPhaseInsightCycles, labelKey)
	}
}
