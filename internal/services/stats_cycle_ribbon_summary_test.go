package services

import (
	"testing"
	"time"
)

// TestBuildStatsCycleRibbonCountsWhatItLeavesOut pins the numbers behind the
// "showing N of M" caption: the stack draws the latest few cycles, and M is
// every completed cycle, so a reader can tell the drawing is a window.
func TestBuildStatsCycleRibbonCountsWhatItLeavesOut(t *testing.T) {
	logs := statscycleribbonHistory(t)
	logs = append(logs, statscycleribbonCycle(t, "2026-05-26", 5)...)

	ribbon := buildStatsCycleRibbon(
		statscycleribbonOwner(false),
		CycleStats{LutealPhase: 14},
		logs,
		buildCompletedCycleSpans(logs, time.UTC, BoundaryContext{}),
	)
	if ribbon.TotalCycles != 5 || len(ribbon.Rows) != 4 {
		t.Fatalf("expected 4 of 5 cycles, got %d of %d", len(ribbon.Rows), ribbon.TotalCycles)
	}
}

// TestBuildStatsCycleRibbonRowsCarryTheirTextAlternative pins what a reader who
// cannot see the bands is told per row: how many days carry an entry, and — only
// when the row draws inferred phases and located an ovulation — the estimated
// fertile window as day numbers.
func TestBuildStatsCycleRibbonRowsCarryTheirTextAlternative(t *testing.T) {
	logs := statscycleribbonHistory(t)
	logs = append(logs, statscycleribbonCycle(t, "2026-05-26", 5)...)
	spans := buildCompletedCycleSpans(logs, time.UTC, BoundaryContext{})

	plain := buildStatsCycleRibbon(statscycleribbonOwner(false), CycleStats{LutealPhase: 14}, logs, spans)
	for _, row := range plain.Rows {
		if row.FertileStartDay != 0 || row.FertileEndDay != 0 {
			t.Fatalf("expected no fertile window text with inferred phases off, got %d-%d", row.FertileStartDay, row.FertileEndDay)
		}
	}
	// Cycle 2026-04-28 is five period days, all logged.
	if got := plain.Rows[3].LoggedDays; got != 5 {
		t.Fatalf("expected 5 logged days on the newest row, got %d", got)
	}

	phased := buildStatsCycleRibbon(statscycleribbonOwner(true), CycleStats{LutealPhase: 14}, logs, spans)
	row := phased.Rows[3]
	if row.FertileStartDay <= 0 || row.FertileEndDay < row.FertileStartDay || row.FertileEndDay > row.CycleLength {
		t.Fatalf("expected a fertile window inside the cycle, got day %d-%d of %d", row.FertileStartDay, row.FertileEndDay, row.CycleLength)
	}
}
