package services

import (
	"testing"
	"time"
)

// mr3statsPhaseContext builds a completed-cycle phase context with a fixed
// UTC anchor, the given period length and ovulation day, and a cycle long
// enough that all probed day numbers fall strictly before NextStart.
func mr3statsPhaseContext(periodLength, ovulationDay int) completedCyclePhaseContext {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return completedCyclePhaseContext{
		Start:        start,
		NextStart:    start.AddDate(0, 0, 60),
		CycleLength:  60,
		PeriodLength: periodLength,
		OvulationDay: ovulationDay,
	}
}

// mr3statsDayForNumber returns the calendar day whose one-based cycle-day
// number (relative to ctx.Start) equals dayNumber.
func mr3statsDayForNumber(ctx completedCyclePhaseContext, dayNumber int) time.Time {
	return ctx.Start.AddDate(0, 0, dayNumber-1)
}

// --- phaseForCompletedCycleDay ---------------------------------------------

// TestMR3Stats_PhaseLastMenstrualDay pins line 98 (dayNumber <= PeriodLength).
// The last menstrual day (dayNumber == PeriodLength) must classify as
// "menstrual". Mutant `<` would misclassify it as "follicular".
func TestMR3Stats_PhaseLastMenstrualDay(t *testing.T) {
	ctx := mr3statsPhaseContext(5, 14)
	day := mr3statsDayForNumber(ctx, ctx.PeriodLength) // dayNumber == 5

	phase := phaseForCompletedCycleDay(day, ctx, time.UTC)

	if phase != "menstrual" {
		t.Fatalf("expected menstrual at day %d (PeriodLength), got %q", ctx.PeriodLength, phase)
	}
}

// TestMR3Stats_PhaseOvulationDay pins line 100 (dayNumber == OvulationDay).
// Day == OvulationDay must classify as "ovulation"; a contrast day (6, which
// is after PeriodLength and before OvulationDay) must classify as
// "follicular". Mutant `!=` flips both classifications.
func TestMR3Stats_PhaseOvulationDay(t *testing.T) {
	ctx := mr3statsPhaseContext(5, 14)

	ovulationDay := mr3statsDayForNumber(ctx, ctx.OvulationDay) // dayNumber == 14
	if phase := phaseForCompletedCycleDay(ovulationDay, ctx, time.UTC); phase != "ovulation" {
		t.Fatalf("expected ovulation at day %d, got %q", ctx.OvulationDay, phase)
	}

	contrastDay := mr3statsDayForNumber(ctx, 6) // not ovulation, between menstrual and ovulation
	if phase := phaseForCompletedCycleDay(contrastDay, ctx, time.UTC); phase != "follicular" {
		t.Fatalf("expected follicular at day 6 (non-ovulation contrast), got %q", phase)
	}
}

// TestMR3Stats_PhaseFollicularLutealBoundary pins line 102
// (dayNumber < OvulationDay). The day immediately before ovulation must be
// "follicular"; the day immediately after must be "luteal". Mutant `<=` would
// make the post-ovulation day "follicular".
func TestMR3Stats_PhaseFollicularLutealBoundary(t *testing.T) {
	ctx := mr3statsPhaseContext(5, 14)

	beforeOv := mr3statsDayForNumber(ctx, ctx.OvulationDay-1) // dayNumber == 13
	if phase := phaseForCompletedCycleDay(beforeOv, ctx, time.UTC); phase != "follicular" {
		t.Fatalf("expected follicular at day %d (ovulation-1), got %q", ctx.OvulationDay-1, phase)
	}

	afterOv := mr3statsDayForNumber(ctx, ctx.OvulationDay+1) // dayNumber == 15
	if phase := phaseForCompletedCycleDay(afterOv, ctx, time.UTC); phase != "luteal" {
		t.Fatalf("expected luteal at day %d (ovulation+1), got %q", ctx.OvulationDay+1, phase)
	}
}
