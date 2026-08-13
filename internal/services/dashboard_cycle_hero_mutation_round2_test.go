package services

import (
	"testing"
	"time"
)

// Round-2 mutation-survivor kill-tests for dashboard_cycle_hero.go.
//
// These pin the ribbon's day arithmetic: the conversion from a calendar date to
// a 1-based cycle day, the closed bounds of a day span, and the axis clamp. The
// behaviour tests in the coverage file exercise these through whole heroes,
// where an off-by-one inside one span can be masked by another; here each is
// called directly with a non-degenerate input so the ARITHMETIC and CONDITIONAL
// mutants are distinguished.

// TestDashboardCycleHeroSpanFromDatesIsOneBased kills the mutants that drop the
// +1 in the date→cycle-day conversion. The cycle start is day 1, not day 0, so a
// window opening on the start date must report StartDay 1; without the +1 every
// span slides one day early and the ribbon shades the wrong column.
func TestDashboardCycleHeroSpanFromDatesIsOneBased(t *testing.T) {
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

	span := dashboardCycleHeroSpanFromDates(
		cycleStart,
		cycleStart,
		cycleStart.AddDate(0, 0, 4),
		cycleStart.AddDate(0, 0, 2),
	)
	if !span.Present {
		t.Fatal("expected a present span for a valid date range")
	}
	if span.StartDay != 1 {
		t.Fatalf("the cycle start is day 1, got StartDay %d", span.StartDay)
	}
	if span.EndDay != 5 {
		t.Fatalf("four days after the start is day 5, got EndDay %d", span.EndDay)
	}
	if span.PeakDay != 3 {
		t.Fatalf("two days after the start is day 3, got PeakDay %d", span.PeakDay)
	}
}

// TestDashboardCycleHeroSpanFromDatesRejectsAnInvertedRange pins the guard that
// an end before its start is no span at all — a mutant weakening it would emit a
// span whose covers() never matches and whose EndDay would still stretch the axis.
func TestDashboardCycleHeroSpanFromDatesRejectsAnInvertedRange(t *testing.T) {
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)

	inverted := dashboardCycleHeroSpanFromDates(
		cycleStart,
		cycleStart.AddDate(0, 0, 6),
		cycleStart.AddDate(0, 0, 2),
		time.Time{},
	)
	if inverted.Present {
		t.Fatal("expected no span when the end precedes the start")
	}

	// A window that closed before this cycle began is not this cycle's window.
	ended := dashboardCycleHeroSpanFromDates(
		cycleStart,
		cycleStart.AddDate(0, 0, -6),
		cycleStart.AddDate(0, 0, -2),
		time.Time{},
	)
	if ended.Present {
		t.Fatal("expected no span when the whole range predates the cycle start")
	}

	// One that straddles the start is clamped to day 1 rather than dropped.
	straddling := dashboardCycleHeroSpanFromDates(
		cycleStart,
		cycleStart.AddDate(0, 0, -2),
		cycleStart.AddDate(0, 0, 1),
		time.Time{},
	)
	if !straddling.Present || straddling.StartDay != 1 || straddling.EndDay != 2 {
		t.Fatalf("expected a span clamped to days 1-2, got %+v", straddling)
	}
}

// TestDashboardCycleHeroDaySpanCoversItsBounds pins covers() as a CLOSED range:
// both the first and the last day belong to it. A mutant turning either <= into
// < drops one end day, which on a fertile window silently un-shades the peak's
// neighbour.
func TestDashboardCycleHeroDaySpanCoversItsBounds(t *testing.T) {
	span := dashboardCycleHeroDaySpan{StartDay: 9, EndDay: 15, Present: true}

	for _, day := range []int{9, 12, 15} {
		if !span.covers(day) {
			t.Fatalf("expected day %d inside the span 9-15", day)
		}
	}
	for _, day := range []int{8, 16} {
		if span.covers(day) {
			t.Fatalf("expected day %d outside the span 9-15", day)
		}
	}

	absent := dashboardCycleHeroDaySpan{StartDay: 9, EndDay: 15}
	if absent.covers(12) {
		t.Fatal("an absent span covers nothing, whatever its bounds hold")
	}
}

// TestDashboardCycleHeroAxisDaysGrowsOnlyForALaterWindow pins the axis: it is
// the cycle length unless the start window reaches past it, and it never grows
// beyond the DOM bound. A mutant flipping the comparison would shrink the axis
// to a window that ends early, cutting the luteal phase off the ribbon.
func TestDashboardCycleHeroAxisDaysGrowsOnlyForALaterWindow(t *testing.T) {
	if got := dashboardCycleHeroAxisDays(28, dashboardCycleHeroDaySpan{}); got != 28 {
		t.Fatalf("with no window the axis is the cycle length, got %d", got)
	}
	if got := dashboardCycleHeroAxisDays(28, dashboardCycleHeroDaySpan{StartDay: 24, EndDay: 26, Present: true}); got != 28 {
		t.Fatalf("a window ending inside the cycle leaves the axis at 28, got %d", got)
	}
	if got := dashboardCycleHeroAxisDays(28, dashboardCycleHeroDaySpan{StartDay: 27, EndDay: 34, Present: true}); got != 34 {
		t.Fatalf("a window ending past the cycle stretches the axis to 34, got %d", got)
	}
	if got := dashboardCycleHeroAxisDays(28, dashboardCycleHeroDaySpan{StartDay: 40, EndDay: 400, Present: true}); got != dashboardCycleHeroMaxAxisDays {
		t.Fatalf("expected the axis clamped to %d, got %d", dashboardCycleHeroMaxAxisDays, got)
	}
}
