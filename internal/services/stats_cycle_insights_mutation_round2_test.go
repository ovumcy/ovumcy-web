package services

// stats_cycle_insights_mutation_round2_test.go
// Round-2 mutation-survivor kill tests for
// internal/services/stats_cycle_insights.go.
//
// Both tests drive the periodLength fallback guard at line 55 of
// stats_cycle_insights.go:
//
//	periodLength := cycles[index].PeriodLength
//	if periodLength <= 0 {
//	    periodLength = models.DefaultPeriodLength
//	}
//
//   - CONDITIONALS_NEGATION (`<= 0` -> `> 0`): with an observed positive
//     periodLength the guard must NOT fire; asserting the exact observed value
//     (1) distinguishes it from the DefaultPeriodLength (5) the mutant forces.
//   - CONDITIONALS_BOUNDARY (`<= 0` -> `< 0`): driving periodLength to exactly
//     0 (via last-write-wins on a same calendar day) must still fall back to
//     DefaultPeriodLength under the original; the boundary mutant leaves a raw 0.
//
// The package-level helper statscycleinsightsCovDay already exists in
// stats_cycle_insights_coverage_test.go and is reused here (not redefined).

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestStatscycleinsightsCovBuildCompletedCycleSpansPeriodLengthMatchesObservedNotDefault(t *testing.T) {
	// Only the first day of each period is logged as IsPeriod, so buildCycles
	// computes an observed period length of exactly 1 for the first span.
	// The fallback-to-DefaultPeriodLength branch (line 55) must NOT fire, because
	// the observed length is positive. A negation of the guard would overwrite the
	// real length with models.DefaultPeriodLength (5); asserting the exact value 1
	// distinguishes the two.
	logs := []models.DailyLog{
		{Date: statscycleinsightsCovDay(t, "2026-01-01"), IsPeriod: true},
		{Date: statscycleinsightsCovDay(t, "2026-01-29"), IsPeriod: true},
	}
	spans := buildCompletedCycleSpans(logs, time.UTC)
	if len(spans) != 1 {
		t.Fatalf("expected exactly one completed span, got %d", len(spans))
	}
	if spans[0].PeriodLength != 1 {
		t.Fatalf("expected observed PeriodLength=1 (single logged period day), got %d", spans[0].PeriodLength)
	}
	if spans[0].PeriodLength == models.DefaultPeriodLength {
		t.Fatalf("PeriodLength must reflect the observed length, not the %d-day default", models.DefaultPeriodLength)
	}
}

func TestStatscycleinsightsBuildCompletedCycleSpansZeroPeriodLengthFallsBackToDefault(t *testing.T) {
	day := func(raw string) time.Time {
		p, err := time.ParseInLocation("2006-01-02T15:04:05", raw, time.UTC)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		return p
	}
	// Same calendar day (2026-01-01) carries a period log at 00:00 (which forms
	// the observed cycle start) AND a non-period log later that day at 23:59:59.
	// buildCycles builds isPeriodByDate with last-write-wins over the
	// timestamp-sorted slice, so the non-period entry (a strictly later instant)
	// overwrites the day to IsPeriod=false. The start day therefore counts zero
	// consecutive period days -> cycles[0].PeriodLength == 0, at a USED span
	// index (cycleLength = 28 > 0). Deterministic: the sort tie is broken by
	// time-of-day, not by unstable-sort ordering.
	logs := []models.DailyLog{
		{Date: day("2026-01-01T00:00:00"), IsPeriod: true},
		{Date: day("2026-01-01T23:59:59"), IsPeriod: false},
		{Date: day("2026-01-29T00:00:00"), IsPeriod: true},
	}

	spans := buildCompletedCycleSpans(logs, time.UTC)
	if len(spans) != 1 {
		t.Fatalf("expected exactly one completed span, got %d", len(spans))
	}
	// Original (periodLength <= 0): fallback fires -> DefaultPeriodLength (5).
	// CONDITIONALS_BOUNDARY mutant (periodLength < 0): for periodLength == 0 the
	// fallback does NOT fire, so the span carries a raw 0. Asserting the
	// documented fallback value kills the boundary mutant.
	if spans[0].PeriodLength != models.DefaultPeriodLength {
		t.Fatalf("expected PeriodLength to fall back to DefaultPeriodLength (%d), got %d",
			models.DefaultPeriodLength, spans[0].PeriodLength)
	}
}
