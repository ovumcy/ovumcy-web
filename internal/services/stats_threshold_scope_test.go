package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The stats page carries three sample-size thresholds that are all 3 or 2
// today, and reading them as one number is the mistake this test exists to
// stop:
//
//   - statsMinimumInsightsCycles (2) — the basic-insights tier, counted in
//     completed cycles;
//   - minimumPhaseInsightCycles (3) — the pattern minimum, counted in completed
//     cycles, gating every surface that claims a pattern rather than a single
//     observation, the cycle-length trend sentence included;
//   - statsReliableTrendCycles (3) — counted in trend POINTS, gating the
//     chart's reliability flag alone.
//
// The two 3s answer different questions about different quantities, so they are
// kept as two names rather than collapsed into one. That is only safe while a
// change to either is visible: each surface below is driven at its own boundary
// through its own entry point, never by reading a constant back, so moving one
// threshold reddens exactly the surfaces it governs and names them.
func statsThresholdCycleLogs(t *testing.T, starts []string) []models.DailyLog {
	t.Helper()

	logs := []models.DailyLog{}
	for _, start := range starts {
		cycle := statscycleribbonCycle(t, start, 5)
		for index := range cycle {
			cycle[index].Mood = MinDayMood + 1
		}
		logs = append(logs, cycle...)
	}
	return logs
}

func TestStatsThresholdsAreNamedPerSurface(t *testing.T) {
	// Four cycle starts are three completed cycles; three starts are two.
	atPattern := statsThresholdCycleLogs(t, []string{"2026-01-01", "2026-01-29", "2026-02-26", "2026-03-26"})
	belowPattern := statsThresholdCycleLogs(t, []string{"2026-01-01", "2026-01-29", "2026-02-26"})
	owner := statscycleribbonOwner(false)
	service := &StatsService{}
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

	t.Run("phase mood insights unlock at the pattern minimum", func(t *testing.T) {
		if _, ok := service.BuildPhaseMoodInsights(owner, belowPattern, time.UTC); ok {
			t.Error("phase mood insights rendered on two completed cycles: the pattern minimum is three, and a surface claiming a phase pattern from two observations is the gate this threshold exists for")
		}
		if _, ok := service.BuildPhaseMoodInsights(owner, atPattern, time.UTC); !ok {
			t.Error("phase mood insights withheld at three completed cycles: the pattern minimum moved, and every surface named on minimumPhaseInsightCycles moved with it")
		}
	})

	t.Run("the cycle-length trend sentence unlocks at the pattern minimum", func(t *testing.T) {
		// Driven on lengths directly: the sentence's window is completed cycle
		// lengths, so its boundary is expressible without building logs — and
		// this is the surface whose gate reads a constant named for phases.
		if _, ok := buildCycleLengthTrendStatement([]int{28, 30}); ok {
			t.Error("the cycle-length trend sentence rendered on a two-cycle window: it rides the pattern minimum, not the trend-point threshold")
		}
		if _, ok := buildCycleLengthTrendStatement([]int{28, 30, 29}); !ok {
			t.Error("the cycle-length trend sentence was withheld on a three-cycle window: it rides the pattern minimum, and that gate moved")
		}
	})

	t.Run("the trend reliability flag counts trend points, not cycles", func(t *testing.T) {
		below := service.BuildFlags(owner, atPattern, CycleStats{}, now, time.UTC, 2)
		at := service.BuildFlags(owner, atPattern, CycleStats{}, now, time.UTC, 3)

		if below.HasReliableTrend {
			t.Error("two trend points were called a reliable trend: the reliability flag counts trend points and its threshold moved")
		}
		if !at.HasReliableTrend {
			t.Error("three trend points were not called a reliable trend: the reliability flag counts trend points and its threshold moved")
		}
		// The same call answers the basic-insights tier, which is the third
		// threshold and the one that is NOT 3 — proof that the surfaces here are
		// read independently rather than through one shared number.
		if !at.HasInsights {
			t.Error("three completed cycles did not reach the basic-insights tier, which unlocks at two")
		}
	})

	t.Run("the basic-insights tier unlocks below the pattern minimum", func(t *testing.T) {
		below := service.BuildFlags(owner, statsThresholdCycleLogs(t, []string{"2026-01-01", "2026-01-29"}), CycleStats{}, now, time.UTC, 0)
		at := service.BuildFlags(owner, belowPattern, CycleStats{}, now, time.UTC, 0)

		if below.HasInsights {
			t.Error("one completed cycle reached the basic-insights tier, which unlocks at two")
		}
		if !at.HasInsights {
			t.Error("two completed cycles did not reach the basic-insights tier: the tier moved, or it is now expressed through the pattern minimum")
		}
	})
}
