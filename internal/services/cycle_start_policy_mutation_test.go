package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestManualCycleStartMaxDate_NonUTCLocationDecidesAllowance kills the line-28
// CONDITIONALS_NEGATION mutant (`location == nil` -> `location != nil`). With a
// non-nil UTC+10 location, the max-allowed day must be computed in the caller's
// location.
func TestManualCycleStartMaxDate_NonUTCLocationDecidesAllowance(t *testing.T) {
	// now is an instant that is still 2026-03-10 in UTC but already 2026-03-11
	// in UTC+10. The max allowed day (today+2) must be computed in the caller's
	// location, giving 2026-03-13. If manualCycleStartMaxDate forces UTC instead,
	// the max collapses to 2026-03-12 and a 2026-03-13 query is wrongly rejected.
	loc := time.FixedZone("east10", 10*3600)
	now := time.Date(2026, 3, 10, 20, 0, 0, 0, time.UTC)
	day := mustParseCycleStartPolicyDay(t, "2026-03-13")

	if !IsAllowedManualCycleStartDate(day, now, loc) {
		t.Fatal("expected 2026-03-13 to be allowed: in UTC+10 'today' is 2026-03-11 so the +2 window reaches 2026-03-13")
	}
}

// TestIsAllowedManualCycleStartDate_NonUTCLocationNotForcedToUTC kills the
// line-39 CONDITIONALS_NEGATION mutant (`location == nil` -> `location != nil`).
func TestIsAllowedManualCycleStartDate_NonUTCLocationNotForcedToUTC(t *testing.T) {
	// A non-nil UTC+10 location must be honoured, not silently replaced by UTC.
	// now=2026-03-10T20:00Z is 2026-03-11 in UTC+10, so today+2 = 2026-03-13 is
	// allowed. If the location were forced to UTC, today would be 2026-03-10,
	// the window would end at 2026-03-12, and 2026-03-13 would be rejected.
	loc := time.FixedZone("east10", 10*3600)
	now := time.Date(2026, 3, 10, 20, 0, 0, 0, time.UTC)

	if !IsAllowedManualCycleStartDate(mustParseCycleStartPolicyDay(t, "2026-03-13"), now, loc) {
		t.Fatal("expected 2026-03-13 allowed in UTC+10 (caller location must not be forced to UTC)")
	}
	// Sanity: one day past the local window is still rejected.
	if IsAllowedManualCycleStartDate(mustParseCycleStartPolicyDay(t, "2026-03-14"), now, loc) {
		t.Fatal("expected 2026-03-14 rejected in UTC+10")
	}
}

// TestPotentialImplantationGapDays_NoStatsUsesPredictedDefaultNotUserLength
// kills the line-83 CONDITIONALS_NEGATION mutant (`cycleLength <= 0` ->
// `cycleLength > 0`) by pinning the predicted-default fallback path.
func TestPotentialImplantationGapDays_NoStatsUsesPredictedDefaultNotUserLength(t *testing.T) {
	// With no logs, BuildCycleStats yields empty stats, so cycleLength must come
	// from predictedCycleLength's default of 28 (not from the user's configured
	// 35). For previousStart 2026-02-26 that puts ovulation on 2026-03-11, and a
	// targetDay of 2026-03-17 is a 6-day gap -> inside the implantation window.
	// If the code instead fell back to the user's 35-day length, ovulation would
	// move to 2026-03-18 and the gap would be negative, returning (0,false).
	user := &models.User{CycleLength: 35}
	previousStart := mustParseCycleStartPolicyDay(t, "2026-02-26")
	targetDay := mustParseCycleStartPolicyDay(t, "2026-03-17")

	gap, ok := potentialImplantationGapDays(user, nil, targetDay, previousStart, time.UTC)
	if !ok || gap != 6 {
		t.Fatalf("expected (6,true) using the predicted 28-day default, got (%d,%t)", gap, ok)
	}
}

// TestShouldSuggestManualCycleStart_AnchorSearchExcludesTargetDay kills the
// line-117 INVERT_NEGATIVES mutant (`day.AddDate(0, 0, -1)` -> `+1`) by ensuring
// the anchor search stops strictly before `day`.
func TestShouldSuggestManualCycleStart_AnchorSearchExcludesTargetDay(t *testing.T) {
	// The anchor search must stop strictly before `day`. Here the only recent
	// anchor is the user's LastPeriodStart on `day` itself (2026-03-20); the only
	// other anchor is far in the past (2026-01-01). Because the on-day anchor is
	// excluded, the effective gap is ~78 days and the long-gap suggestion fires.
	// If the search included `day`, the anchor would be 2026-03-20, the gap 0, and
	// the suggestion would be wrongly suppressed.
	lastPeriod := mustParseCycleStartPolicyDay(t, "2026-03-20")
	user := &models.User{LastPeriodStart: &lastPeriod}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
	}
	day := mustParseCycleStartPolicyDay(t, "2026-03-20")
	logEntry := models.DailyLog{Date: day, IsPeriod: true}

	if !ShouldSuggestManualCycleStart(user, logs, logEntry, day, day, time.UTC) {
		t.Fatal("expected a long-gap suggestion: the on-day anchor must be excluded, leaving the 2026-01-01 anchor (~78d gap)")
	}
}
