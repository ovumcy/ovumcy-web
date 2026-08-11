package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Cycle phase and fertility status are two orthogonal facts sharing the same
// day: the phase taxonomy is strictly menstrual/follicular/ovulation/luteal/
// unknown, and "fertile" exists only as the window-membership status. These
// tests pin both axes together so no surface can rebuild the merged "Fertile
// phase" the dashboard, insights and calendar used to disagree on.

func TestResolveFertilityStatusFollowsWindowMembership(t *testing.T) {
	stats := CycleStats{
		LastPeriodStart:      mustParseDay(t, "2026-01-01"),
		AveragePeriodLength:  5,
		OvulationDate:        mustParseDay(t, "2026-01-20"),
		FertilityWindowStart: mustParseDay(t, "2026-01-15"),
		FertilityWindowEnd:   mustParseDay(t, "2026-01-20"),
	}

	cases := []struct {
		name  string
		today string
		want  string
	}{
		{"day before the window", "2026-01-14", FertilityStatusNotFertile},
		{"window start", "2026-01-15", FertilityStatusFertile},
		{"mid-window before ovulation", "2026-01-17", FertilityStatusFertile},
		{"ovulation day", "2026-01-20", FertilityStatusFertile},
		{"day after the window", "2026-01-21", FertilityStatusNotFertile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ResolveFertilityStatus(stats, mustParseDay(t, tc.today)); got != tc.want {
				t.Fatalf("ResolveFertilityStatus(%s): expected %s, got %s", tc.today, tc.want, got)
			}
		})
	}
}

func TestResolveFertilityStatusUnknownWithoutAPrediction(t *testing.T) {
	today := mustParseDay(t, "2026-01-10")

	impossible := CycleStats{
		LastPeriodStart:     mustParseDay(t, "2026-01-01"),
		AveragePeriodLength: 5,
		OvulationImpossible: true,
	}
	if got := ResolveFertilityStatus(impossible, today); got != FertilityStatusUnknown {
		t.Fatalf("expected unknown when ovulation impossible, got %s", got)
	}

	zeroOvulation := CycleStats{
		LastPeriodStart:     mustParseDay(t, "2026-01-01"),
		AveragePeriodLength: 5,
	}
	if got := ResolveFertilityStatus(zeroOvulation, today); got != FertilityStatusUnknown {
		t.Fatalf("expected unknown for zero ovulation date, got %s", got)
	}
}

// TestPhaseAndFertilityAreOrthogonalOnAWindowDay is the item-24 regression:
// the same day answers both questions, and neither answer leaks into the
// other's taxonomy.
func TestPhaseAndFertilityAreOrthogonalOnAWindowDay(t *testing.T) {
	stats := CycleStats{
		LastPeriodStart:      mustParseDay(t, "2026-01-01"),
		AveragePeriodLength:  5,
		OvulationDate:        mustParseDay(t, "2026-01-20"),
		FertilityWindowStart: mustParseDay(t, "2026-01-15"),
		FertilityWindowEnd:   mustParseDay(t, "2026-01-21"),
	}
	var logs []models.DailyLog

	preOvulation := mustParseDay(t, "2026-01-17")
	if got := detectCyclePhase(stats, logs, preOvulation); got != "follicular" {
		t.Fatalf("detectCyclePhase: expected follicular on a pre-ovulation window day, got %s", got)
	}
	if got := DetectCurrentPhase(stats, logs, preOvulation, time.UTC); got != "follicular" {
		t.Fatalf("DetectCurrentPhase: expected follicular on a pre-ovulation window day, got %s", got)
	}
	if got := ResolveFertilityStatus(stats, preOvulation); got != FertilityStatusFertile {
		t.Fatalf("expected fertile status on a pre-ovulation window day, got %s", got)
	}

	// A window that outlives ovulation keeps the status while the phase moves on.
	postOvulation := mustParseDay(t, "2026-01-21")
	if got := detectCyclePhase(stats, logs, postOvulation); got != "luteal" {
		t.Fatalf("detectCyclePhase: expected luteal on a post-ovulation window day, got %s", got)
	}
	if got := ResolveFertilityStatus(stats, postOvulation); got != FertilityStatusFertile {
		t.Fatalf("expected fertile status on a post-ovulation window day, got %s", got)
	}
}

func TestBuildCycleStatsPopulatesCurrentFertility(t *testing.T) {
	empty := BuildCycleStats(nil, mustParseDay(t, "2026-01-10"))
	if empty.CurrentFertility != FertilityStatusUnknown {
		t.Fatalf("expected unknown fertility without logs, got %s", empty.CurrentFertility)
	}

	logs := []models.DailyLog{
		makeLog(t, "2026-01-01", true),
		makeLog(t, "2026-01-02", true),
	}
	stats := BuildCycleStats(logs, mustParseDay(t, "2026-01-10"))
	if stats.CurrentFertility != ResolveFertilityStatus(stats, mustParseDay(t, "2026-01-10")) {
		t.Fatalf("BuildCycleStats fertility %s disagrees with ResolveFertilityStatus", stats.CurrentFertility)
	}
	if stats.CurrentFertility == "" {
		t.Fatal("BuildCycleStats left CurrentFertility empty")
	}
}

// TestApplyUserCycleBaselineExposesBothAxesOnAFertileDay drives the full
// owner-facing path: settings-derived prediction (cycle 28, luteal 14 →
// ovulation Jan 14, window Jan 9–14) with today inside the window.
func TestApplyUserCycleBaselineExposesBothAxesOnAFertileDay(t *testing.T) {
	userLastPeriod := mustParseDay(t, "2026-01-01")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     28,
		PeriodLength:    5,
		LastPeriodStart: &userLastPeriod,
	}
	logs := []models.DailyLog{makeLog(t, "2026-01-01", true)}

	now := mustParseDay(t, "2026-01-12")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if stats.CurrentPhase != "follicular" {
		t.Fatalf("expected follicular phase on a fertile-window day, got %s", stats.CurrentPhase)
	}
	if stats.CurrentFertility != FertilityStatusFertile {
		t.Fatalf("expected fertile status on a fertile-window day, got %s", stats.CurrentFertility)
	}

	later := mustParseDay(t, "2026-01-20")
	statsLater := ApplyUserCycleBaseline(user, logs, BuildCycleStats(logs, later), later, time.UTC)
	if statsLater.CurrentPhase != "luteal" {
		t.Fatalf("expected luteal phase after the window, got %s", statsLater.CurrentPhase)
	}
	if statsLater.CurrentFertility != FertilityStatusNotFertile {
		t.Fatalf("expected not_fertile status after the window, got %s", statsLater.CurrentFertility)
	}
}
