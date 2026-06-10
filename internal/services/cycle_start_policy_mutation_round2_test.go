package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestResolveManualCycleStartPolicy_AnchorSearchStopsBeforeTargetDay kills the
// mutants at cycle_start_policy.go:61, where the previous-start anchor search
// uses an exclusive cutoff of targetDay.AddDate(0, 0, -1). It covers both the
// ARITHMETIC_BASE mutant (the -1 offset flipped to +1) and the INVERT_NEGATIVES
// mutant (the negative literal inverted to +1) — both shift the cutoff forward
// so the on-day anchor leaks into the search.
//
// Two cycle-start anchors exist: one 3 days before the target (2026-03-12) and
// one ON the target day (2026-03-15). With the correct exclusive cutoff
// (2026-03-14) the on-day anchor is rejected and the 2026-03-12 anchor wins,
// producing a 3-day short gap. If the cutoff offset becomes +1 (2026-03-16) the
// more-recent on-day anchor is selected instead, gapDays collapses to 0, and the
// `gapDays > 0` guard suppresses the short-gap fields.
func TestResolveManualCycleStartPolicy_AnchorSearchStopsBeforeTargetDay(t *testing.T) {
	// The anchor search must stop strictly before targetDay (cutoff = targetDay-1).
	// Two cycle-start anchors exist: one 3 days before the target and one ON the
	// target day. The earlier anchor must win, producing a 3-day short gap. If the
	// cutoff offset flips from -1 to +1 (or the negative literal is inverted), the
	// on-day anchor is selected instead, gapDays collapses to 0, and the short-gap
	// flag is wrongly suppressed.
	targetDay := mustParseCycleStartPolicyDay(t, "2026-03-15")
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-03-12"), IsPeriod: true, CycleStart: true},
		{Date: targetDay, IsPeriod: true, CycleStart: true},
	}
	now := targetDay

	p := ResolveManualCycleStartPolicy(user, logs, targetDay, now, time.UTC)

	if p.ShortGapDays != 3 {
		t.Fatalf("expected ShortGapDays=3 from the 2026-03-12 anchor, got %d", p.ShortGapDays)
	}
	if got := p.PreviousStart.Format("2006-01-02"); got != "2026-03-12" {
		t.Fatalf("expected PreviousStart 2026-03-12 (on-day anchor must be excluded), got %s", got)
	}
}
