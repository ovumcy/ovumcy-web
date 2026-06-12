package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Boundary tests for the manual-cycle-start policy. The cycle baseline left the
// exact day thresholds (future-date allowance, short-gap window, new-cycle
// suggestion gap) unpinned, so mutating those comparisons survived. These tests
// assert each threshold on both sides so the boundary cannot drift silently.

func TestIsAllowedManualCycleStartDate_FutureBoundary(t *testing.T) {
	now := mustParseCycleStartPolicyDay(t, "2026-03-10")
	cases := []struct {
		name string
		day  string
		want bool
	}{
		{"today is allowed", "2026-03-10", true},
		{"max future (+2 days) is allowed", "2026-03-12", true},
		{"one day past the +2 limit is rejected", "2026-03-13", false},
		{"zero date is rejected", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var day time.Time
			if tc.day != "" {
				day = mustParseCycleStartPolicyDay(t, tc.day)
			}
			if got := IsAllowedManualCycleStartDate(day, now, time.UTC); got != tc.want {
				t.Fatalf("IsAllowedManualCycleStartDate(%q) = %t, want %t", tc.day, got, tc.want)
			}
		})
	}
}

func TestShouldSuggestManualCycleStart_GapThreshold(t *testing.T) {
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
	}
	// Anchor is 2026-01-01; the suggestion only fires once the gap reaches 15 days.
	cases := []struct {
		name string
		day  string
		want bool
	}{
		{"gap of 14 days does not suggest", "2026-01-15", false},
		{"gap of exactly 15 days suggests", "2026-01-16", true},
		{"larger gap still suggests", "2026-02-01", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			day := mustParseCycleStartPolicyDay(t, tc.day)
			logEntry := models.DailyLog{Date: day, IsPeriod: true}
			// now == day keeps the date within the allowed future window.
			got := ShouldSuggestManualCycleStart(user, logs, logEntry, day, day, time.UTC)
			if got != tc.want {
				t.Fatalf("ShouldSuggestManualCycleStart(gap to %s) = %t, want %t", tc.day, got, tc.want)
			}
		})
	}
}

func TestShouldSuggestManualCycleStart_RequiresPeriodNonStart(t *testing.T) {
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
	}
	day := mustParseCycleStartPolicyDay(t, "2026-02-01") // 31-day gap, well past threshold

	// A non-period entry must never trigger the suggestion.
	if ShouldSuggestManualCycleStart(user, logs, models.DailyLog{Date: day, IsPeriod: false}, day, day, time.UTC) {
		t.Fatal("non-period entry must not suggest a manual cycle start")
	}
	// An entry already marked as a cycle start must never trigger it either.
	if ShouldSuggestManualCycleStart(user, logs, models.DailyLog{Date: day, IsPeriod: true, CycleStart: true}, day, day, time.UTC) {
		t.Fatal("entry already marked CycleStart must not suggest another")
	}
}

func TestPotentialImplantationGapDays_WindowBoundary(t *testing.T) {
	// With no prior logs the cycle length resolves to the user's configured 28
	// days and the luteal phase to the 14-day default, so ovulation for a cycle
	// starting 2026-02-26 lands on 2026-03-11. The implantation warning fires
	// only for a gap of 6..12 days after that ovulation date.
	user := &models.User{CycleLength: 28}
	previousStart := mustParseCycleStartPolicyDay(t, "2026-02-26")
	cases := []struct {
		name      string
		targetDay string
		wantGap   int
		wantOK    bool
	}{
		{"5 days after ovulation is too early", "2026-03-16", 0, false},
		{"6 days after ovulation is the lower edge", "2026-03-17", 6, true},
		{"12 days after ovulation is the upper edge", "2026-03-23", 12, true},
		{"13 days after ovulation is too late", "2026-03-24", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			targetDay := mustParseCycleStartPolicyDay(t, tc.targetDay)
			gap, ok := potentialImplantationGapDays(user, nil, targetDay, previousStart)
			if gap != tc.wantGap || ok != tc.wantOK {
				t.Fatalf("potentialImplantationGapDays(target %s) = (%d,%t), want (%d,%t)",
					tc.targetDay, gap, ok, tc.wantGap, tc.wantOK)
			}
		})
	}
}

func TestResolveManualCycleStartPolicy_ShortGapBoundary(t *testing.T) {
	user := &models.User{}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
	}
	now := mustParseCycleStartPolicyDay(t, "2026-02-01")

	// A gap inside (0, 15) flags a short gap and records the previous start.
	within := mustParseCycleStartPolicyDay(t, "2026-01-10") // gap 9
	p := ResolveManualCycleStartPolicy(user, logs, within, now, time.UTC)
	if p.ShortGapDays != 9 {
		t.Fatalf("expected ShortGapDays 9, got %d", p.ShortGapDays)
	}
	if p.PreviousStart.IsZero() {
		t.Fatal("expected PreviousStart to be recorded for a short gap")
	}

	// At exactly 15 days the gap is no longer "short" — nothing is flagged.
	atThreshold := mustParseCycleStartPolicyDay(t, "2026-01-16") // gap 15
	p2 := ResolveManualCycleStartPolicy(user, logs, atThreshold, now, time.UTC)
	if p2.ShortGapDays != 0 {
		t.Fatalf("expected no short-gap flag at the 15-day boundary, got %d", p2.ShortGapDays)
	}
}

// TestPotentialImplantationGapDays_CrossTimezone pins the issue-#48-class
// fix on the implantation gap: `targetDay` is a location-midnight working
// value while PredictCycleWindow returns a UTC-midnight ovulation date.
// Before the fix, DateAtLocation on the UTC-midnight ovulation date shifted
// it one calendar day backward in UTC-minus locales, inflating the gap by
// one: a 5-day gap (too early) was reported as a 6-day implantation
// candidate and a true 12-day gap fell outside the 6..12 window.
func TestPotentialImplantationGapDays_CrossTimezone(t *testing.T) {
	// Same geometry as the window-boundary test: ovulation for a 28-day cycle
	// starting 2026-02-26 lands on 2026-03-11.
	user := &models.User{CycleLength: 28}
	previousStart := mustParseCycleStartPolicyDay(t, "2026-02-26")
	tokyo := time.FixedZone("UTC+9", 9*60*60)
	lima := time.FixedZone("UTC-5", -5*60*60)

	cases := []struct {
		name      string
		targetDay time.Time
		wantGap   int
		wantOK    bool
	}{
		{"UTC-5 gap of 5 days stays too early", time.Date(2026, 3, 16, 0, 0, 0, 0, lima), 0, false},
		{"UTC-5 gap of 6 days is the lower edge", time.Date(2026, 3, 17, 0, 0, 0, 0, lima), 6, true},
		{"UTC-5 gap of 12 days is the upper edge", time.Date(2026, 3, 23, 0, 0, 0, 0, lima), 12, true},
		{"UTC+9 gap of 6 days is the lower edge", time.Date(2026, 3, 17, 0, 0, 0, 0, tokyo), 6, true},
		{"UTC+9 gap of 13 days stays too late", time.Date(2026, 3, 24, 0, 0, 0, 0, tokyo), 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gap, ok := potentialImplantationGapDays(user, nil, tc.targetDay, previousStart)
			if gap != tc.wantGap || ok != tc.wantOK {
				t.Fatalf("potentialImplantationGapDays(target %s) = (%d,%t), want (%d,%t)",
					tc.targetDay.Format(time.RFC3339), gap, ok, tc.wantGap, tc.wantOK)
			}
		})
	}
}
