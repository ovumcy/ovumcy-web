package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// These tests pin the current, independently-evolved behavior of the two
// cycle-phase detectors (detectCyclePhase in cycles.go, DetectCurrentPhase in
// cycle_baseline.go) before they are unified behind one implementation. They
// must keep passing, unchanged, after the refactor.

// TestPhaseDetectors_AgreeOnLoggedMenstrualDay: both detectors call a day
// "menstrual" when a period entry is actually logged for it, regardless of
// where it falls relative to the predicted cycle window.
func TestPhaseDetectors_AgreeOnLoggedMenstrualDay(t *testing.T) {
	today := mustParseDay(t, "2026-01-15")
	logs := []models.DailyLog{makeLog(t, "2026-01-15", true)}
	stats := CycleStats{
		LastPeriodStart:      mustParseDay(t, "2026-01-01"),
		AveragePeriodLength:  5,
		OvulationDate:        mustParseDay(t, "2026-01-20"),
		FertilityWindowStart: mustParseDay(t, "2026-01-15"),
		FertilityWindowEnd:   mustParseDay(t, "2026-01-20"),
	}

	if got := detectCyclePhase(stats, logs, today); got != "menstrual" {
		t.Fatalf("detectCyclePhase: expected menstrual for logged period day, got %s", got)
	}
	if got := DetectCurrentPhase(stats, logs, today, time.UTC); got != "menstrual" {
		t.Fatalf("DetectCurrentPhase: expected menstrual for logged period day, got %s", got)
	}
}

// TestPhaseDetectors_DivergeOnProjectedPeriodWithoutALoggedDay is the one true
// behavioral conflict between the two detectors: DetectCurrentPhase treats a
// day inside [LastPeriodStart, LastPeriodStart+AveragePeriodLength-1] as
// menstrual even with no logged period entry for it (a "projected period");
// detectCyclePhase requires an actual logged entry and falls through to the
// ovulation-window phases instead. Do not collapse this difference silently —
// see cycles.go/cycle_baseline.go for the callers that rely on each.
func TestPhaseDetectors_DivergeOnProjectedPeriodWithoutALoggedDay(t *testing.T) {
	today := mustParseDay(t, "2026-01-03") // inside Jan 1 - Jan 5 projected period, not logged
	var logs []models.DailyLog
	stats := CycleStats{
		LastPeriodStart:      mustParseDay(t, "2026-01-01"),
		AveragePeriodLength:  5,
		OvulationDate:        mustParseDay(t, "2026-01-20"),
		FertilityWindowStart: mustParseDay(t, "2026-01-15"),
		FertilityWindowEnd:   mustParseDay(t, "2026-01-20"),
	}

	if got := detectCyclePhase(stats, logs, today); got != "follicular" {
		t.Fatalf("detectCyclePhase: expected follicular (no logged-only menstrual match), got %s", got)
	}
	if got := DetectCurrentPhase(stats, logs, today, time.UTC); got != "menstrual" {
		t.Fatalf("DetectCurrentPhase: expected menstrual from projected period window, got %s", got)
	}
}

// TestPhaseDetectors_AgreeOnFertileOvulationFollicularLutealWindows pins that
// outside of the logged-vs-projected-menstrual difference, both detectors
// resolve the ovulation/fertility/follicular/luteal windows identically.
func TestPhaseDetectors_AgreeOnFertileOvulationFollicularLutealWindows(t *testing.T) {
	stats := CycleStats{
		LastPeriodStart:      mustParseDay(t, "2026-01-01"),
		AveragePeriodLength:  5,
		OvulationDate:        mustParseDay(t, "2026-01-20"),
		FertilityWindowStart: mustParseDay(t, "2026-01-15"),
		FertilityWindowEnd:   mustParseDay(t, "2026-01-20"),
	}
	var logs []models.DailyLog

	cases := []struct {
		name  string
		today string
		want  string
	}{
		{"follicular after projected period", "2026-01-10", "follicular"},
		{"fertile before ovulation", "2026-01-17", "fertile"},
		{"ovulation day exact", "2026-01-20", "ovulation"},
		{"luteal after ovulation", "2026-01-25", "luteal"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			today := mustParseDay(t, tc.today)
			if got := detectCyclePhase(stats, logs, today); got != tc.want {
				t.Fatalf("detectCyclePhase(%s): expected %s, got %s", tc.today, tc.want, got)
			}
			if got := DetectCurrentPhase(stats, logs, today, time.UTC); got != tc.want {
				t.Fatalf("DetectCurrentPhase(%s): expected %s, got %s", tc.today, tc.want, got)
			}
		})
	}
}

// TestPhaseDetectors_AgreeOnUnknownWhenOvulationImpossibleOrUnset pins the
// shared "unknown" fallback for both the OvulationImpossible flag and a
// zero-value OvulationDate, with no logged or projected period day involved.
func TestPhaseDetectors_AgreeOnUnknownWhenOvulationImpossibleOrUnset(t *testing.T) {
	today := mustParseDay(t, "2026-01-10")
	var logs []models.DailyLog

	impossible := CycleStats{
		LastPeriodStart:     mustParseDay(t, "2026-01-01"),
		AveragePeriodLength: 5,
		OvulationImpossible: true,
	}
	if got := detectCyclePhase(impossible, logs, today); got != "unknown" {
		t.Fatalf("detectCyclePhase: expected unknown when ovulation impossible, got %s", got)
	}
	if got := DetectCurrentPhase(impossible, logs, today, time.UTC); got != "unknown" {
		t.Fatalf("DetectCurrentPhase: expected unknown when ovulation impossible, got %s", got)
	}

	zeroOvulation := CycleStats{
		LastPeriodStart:     mustParseDay(t, "2026-01-01"),
		AveragePeriodLength: 5,
	}
	if got := detectCyclePhase(zeroOvulation, logs, today); got != "unknown" {
		t.Fatalf("detectCyclePhase: expected unknown for zero ovulation date, got %s", got)
	}
	if got := DetectCurrentPhase(zeroOvulation, logs, today, time.UTC); got != "unknown" {
		t.Fatalf("DetectCurrentPhase: expected unknown for zero ovulation date, got %s", got)
	}
}

// TestPhaseDetectors_DetectCurrentPhaseHonorsLocationForProjectedWindow pins
// that DetectCurrentPhase's projected-period window is computed via the
// supplied location (detectCyclePhase has no location parameter at all).
func TestPhaseDetectors_DetectCurrentPhaseHonorsLocationForProjectedWindow(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	stats := CycleStats{
		LastPeriodStart:      mustParseDay(t, "2026-01-01"),
		AveragePeriodLength:  5,
		OvulationDate:        mustParseDay(t, "2026-01-20"),
		FertilityWindowStart: mustParseDay(t, "2026-01-15"),
		FertilityWindowEnd:   mustParseDay(t, "2026-01-20"),
	}
	today := mustParseDay(t, "2026-01-05")
	var logs []models.DailyLog

	if got := DetectCurrentPhase(stats, logs, today, loc); got != "menstrual" {
		t.Fatalf("DetectCurrentPhase: expected menstrual on last projected-period day, got %s", got)
	}
}
