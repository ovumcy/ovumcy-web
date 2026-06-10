package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Round-2 mutation-survivor kill tests for stats_phase_insights.go.
//
// All package test helpers referenced here already exist elsewhere in package
// services and are intentionally NOT redefined:
//   - statsphaseinsightsCovParseDay / statsphaseinsightsCovLocation /
//     statsphaseinsightsCovOwner / statsphaseinsightsCovDay
//     (stats_phase_insights_coverage_test.go)
//   - NewStatsService / stubStatsDayReader / stubStatsSymptomReader
//     (stats_service.go, stats_service_test.go)
//   - mustParseStatsServiceDay (stats_service_test.go)

// TestStatsphaseinsightsCovBuildContextsExcludesShortOvulationlessCycles kills
// the CONDITIONALS_BOUNDARY mutant at line 74 (ovulationDay <= 0 -> < 0).
func TestStatsphaseinsightsCovBuildContextsExcludesShortOvulationlessCycles(t *testing.T) {
	// Period days chosen so DetectCycleStarts (new start when a period day is
	// >= 6 calendar days after the previous one) yields five starts and thus four
	// completed cycles:
	//   Jan 1 -> Jan 7  : length 6  -> CalcOvulationDay(6,14)=0  -> must be skipped
	//   Jan 7 -> Jan 21 : length 14 -> CalcOvulationDay(14,14)=0 -> must be skipped
	//   Jan 21-> Feb 18 : length 28 -> ovulationDay 14           -> kept
	//   Feb 18-> Mar 18 : length 28 -> ovulationDay 14           -> kept
	logs := []models.DailyLog{
		{Date: statsphaseinsightsCovParseDay("2026-01-01"), IsPeriod: true},
		{Date: statsphaseinsightsCovParseDay("2026-01-07"), IsPeriod: true},
		{Date: statsphaseinsightsCovParseDay("2026-01-21"), IsPeriod: true},
		{Date: statsphaseinsightsCovParseDay("2026-02-18"), IsPeriod: true},
		{Date: statsphaseinsightsCovParseDay("2026-03-18"), IsPeriod: true},
	}

	got := buildCompletedCyclePhaseContexts(logs, statsphaseinsightsCovLocation)

	// Original skips both ovulation-less short cycles, leaving exactly the two
	// valid 28-day cycles. The mutation (ovulationDay < 0) keeps the short cycles.
	if len(got) != 2 {
		t.Fatalf("expected 2 contexts (short ovulation-less cycles skipped), got %d: %+v", len(got), got)
	}
	for _, ctx := range got {
		if ctx.OvulationDay <= 0 {
			t.Fatalf("context retained with invalid ovulation day %d: %+v", ctx.OvulationDay, ctx)
		}
		if ctx.CycleLength < 15 {
			t.Fatalf("context retained for too-short cycle (length %d): %+v", ctx.CycleLength, ctx)
		}
	}
}

// TestStatsphaseinsightsCovMoodInsightEmptyPhaseHasNoData kills the
// CONDITIONALS_BOUNDARY mutant at line 164 (current.count > 0 -> >= 0).
func TestStatsphaseinsightsCovMoodInsightEmptyPhaseHasNoData(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	owner := statsphaseinsightsCovOwner(123)

	// Three completed 28-day cycles. Mood is logged ONLY on the menstrual start
	// day of each cycle, so follicular, ovulation and luteal phases have zero
	// qualifying mood entries (count == 0).
	logs := []models.DailyLog{
		{Date: statsphaseinsightsCovDay(t, "2026-01-01"), IsPeriod: true, Mood: 3},
		{Date: statsphaseinsightsCovDay(t, "2026-01-29"), IsPeriod: true, Mood: 3},
		{Date: statsphaseinsightsCovDay(t, "2026-02-26"), IsPeriod: true, Mood: 3},
		{Date: statsphaseinsightsCovDay(t, "2026-03-26"), IsPeriod: true}, // opens the 4th start
	}

	insights, ok := service.BuildPhaseMoodInsights(owner, logs, statsphaseinsightsCovLocation)
	if !ok {
		t.Fatal("expected phase mood insights to be available")
	}
	if len(insights) != 4 {
		t.Fatalf("expected four phase insights, got %d", len(insights))
	}

	// phaseInsightOrder = menstrual, follicular, ovulation, luteal.
	// Phases without any mood entry must report no data and a zero average,
	// not HasData=true with a NaN average.
	for _, idx := range []int{1, 2, 3} {
		insight := insights[idx]
		if insight.EntryCount != 0 {
			t.Fatalf("phase %q expected EntryCount=0, got %d", insight.Phase, insight.EntryCount)
		}
		if insight.HasData {
			t.Fatalf("phase %q with no mood entries must have HasData=false", insight.Phase)
		}
		if insight.AverageMood != 0 {
			t.Fatalf("phase %q with no mood entries must have AverageMood=0, got %v", insight.Phase, insight.AverageMood)
		}
	}
}

// TestStatsphaseinsightsCovHasPhaseSymptomInsightDataRequiresPositiveTotalDays
// kills the CONDITIONALS_BOUNDARY mutant at line 305 (totalDays > 0 -> >= 0).
func TestStatsphaseinsightsCovHasPhaseSymptomInsightDataRequiresPositiveTotalDays(t *testing.T) {
	counters := newPhaseSymptomCounters()
	// A phase that has symptom counts but zero observed days must NOT be treated
	// as having data: totalDays>0 is a required part of the guard alongside
	// len(counts)>0. (Mirror of the existing days>0/no-counts case.)
	counters["luteal"].counts[1] = 2 // counts present
	// counters["luteal"].totalDays is left at 0

	if hasPhaseSymptomInsightData(counters) {
		t.Fatal("expected hasPhaseSymptomInsightData=false when totalDays==0 even though counts are present")
	}
}

// TestBuildPhaseMoodInsightsDefaultsZeroPeriodLengthToMenstrual kills
// stats_phase_insights.go:69 CONDITIONALS_BOUNDARY (<=0 -> <0).
// Deterministic: a duplicate IsPeriod=false log with a LATER time-of-day on the
// cycle-1 start day forces buildCycles' periodLength=0 for that cycle (last-write
// to isPeriodByDate by calendar key, ordered by full timestamp). Original defaults
// periodLength to 5 (day1 -> menstrual); mutant leaves 0 (day1 -> follicular).
func TestBuildPhaseMoodInsightsDefaultsZeroPeriodLengthToMenstrual(t *testing.T) {
	service := NewStatsService(&stubStatsDayReader{}, &stubStatsSymptomReader{})
	owner := &models.User{ID: 7, Role: models.RoleOwner}

	// Three completed 28-day cycles (4 period starts). On the cycle-1 start day
	// there are TWO logs: the IsPeriod=true start at 00:00 carries the only
	// in-range mood (3); a duplicate IsPeriod=false log at 12:00 (later wall time,
	// Mood out of range) deterministically sorts last and zeroes periodLength.
	dupFalseLater := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-01-01"), IsPeriod: true, Mood: 3}, // cycle1 day1, 00:00
		{Date: dupFalseLater, IsPeriod: false, Mood: 0},                            // dup same day, 12:00 -> periodLength 0
		{Date: mustParseStatsServiceDay(t, "2026-01-29"), IsPeriod: true},          // cycle2 start
		{Date: mustParseStatsServiceDay(t, "2026-02-26"), IsPeriod: true},          // cycle3 start
		{Date: mustParseStatsServiceDay(t, "2026-03-26"), IsPeriod: true},          // closes cycle3
	}

	insights, ok := service.BuildPhaseMoodInsights(owner, logs, time.UTC)
	if !ok {
		t.Fatal("expected phase mood insights to be available (3 completed cycles)")
	}
	menstrual := insights[0]
	if menstrual.Phase != "menstrual" {
		t.Fatalf("expected first insight menstrual, got %q", menstrual.Phase)
	}
	// ORIGINAL: day-1 mood lands in menstrual (periodLength defaulted to 5).
	// MUTANT (<0): periodLength stays 0, day1 classifies follicular -> these fail.
	if !menstrual.HasData || menstrual.EntryCount != 1 || menstrual.AverageMood != 3 {
		t.Fatalf("expected menstrual{count=1,avg=3,hasData=true}, got %#v", menstrual)
	}
	follicular := insights[1]
	if follicular.Phase != "follicular" {
		t.Fatalf("expected second insight follicular, got %q", follicular.Phase)
	}
	if follicular.EntryCount != 0 || follicular.HasData {
		t.Fatalf("expected follicular empty under original, got %#v", follicular)
	}
}
