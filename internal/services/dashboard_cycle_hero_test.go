package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestBuildDashboardCycleHeroBuildsSegmentedOverview(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	stats := CycleStats{
		CurrentCycleDay:     3,
		CurrentPhase:        "menstrual",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	cycleContext := DashboardCycleContext{
		DisplayOvulationExact: true,
	}

	hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("expected cycle hero to be visible")
	}
	if hero.Approximate {
		t.Fatal("did not expect exact hero to be approximate")
	}
	if hero.CurrentPhase != "menstrual" {
		t.Fatalf("expected menstrual current phase, got %q", hero.CurrentPhase)
	}
	if hero.CycleLength != 28 {
		t.Fatalf("expected cycle length 28, got %d", hero.CycleLength)
	}
	if len(hero.PhaseCards) != 4 {
		t.Fatalf("expected 4 phase cards, got %d", len(hero.PhaseCards))
	}
	expectCardRange(t, hero.PhaseCards[0], "menstrual", 1, 5, true)
	expectCardRange(t, hero.PhaseCards[1], "follicular", 6, 13, false)
	expectCardRange(t, hero.PhaseCards[2], "ovulation", 14, 14, false)
	expectCardRange(t, hero.PhaseCards[3], "luteal", 15, 28, false)
	if hero.AxisDays != 28 || len(hero.Days) != 28 {
		t.Fatalf("expected a 28-day ribbon, got axis %d over %d cells", hero.AxisDays, len(hero.Days))
	}
	if !hero.Days[2].IsToday {
		t.Fatal("expected cycle day 3 to be marked as today")
	}
}

func TestBuildDashboardCycleHeroMarksRangeBasedViewsApproximate(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 29, IrregularCycle: true}
	stats := CycleStats{
		CurrentCycleDay:     11,
		CurrentPhase:        "follicular",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	cycleContext := DashboardCycleContext{
		DisplayNextPeriodUseRange: true,
		DisplayOvulationUseRange:  true,
	}

	hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("expected cycle hero to stay visible for range-based predictions")
	}
	if !hero.Approximate {
		t.Fatal("expected range-based cycle hero to be approximate")
	}
	if hero.CurrentPhase != "follicular" {
		t.Fatalf("expected follicular hero phase, got %q", hero.CurrentPhase)
	}
}

func TestBuildDashboardCycleHeroSkipsSparseOrDisabledPredictionStates(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	stats := CycleStats{
		CurrentCycleDay:     9,
		CurrentPhase:        "follicular",
		AveragePeriodLength: 5,
		LutealPhase:         14,
		LastPeriodStart:     time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
	}

	sparse := BuildDashboardCycleHero(user, stats, DashboardCycleContext{
		DisplayNextPeriodNeedsData: true,
		DisplayOvulationNeedsData:  true,
	}, dashboardCycleHeroInput{})
	if sparse.Visible {
		t.Fatal("did not expect sparse irregular state to render segmented cycle hero")
	}

	disabled := BuildDashboardCycleHero(user, stats, DashboardCycleContext{
		PredictionDisabled: true,
	}, dashboardCycleHeroInput{})
	if disabled.Visible {
		t.Fatal("did not expect unpredictable mode to render segmented cycle hero")
	}
}

// TestDashboardCycleHeroIgnoresUnconfirmedOvulationDateOutsideTheAverageWindow
// is the unconfirmed counterpart of TestDashboardHeroOvulationCardSitsOnTheConfirmedPeak:
// stats.OvulationDate is a MEDIAN-driven projection (here cycle day 32, from a
// 45-day median), while the ribbon's own geometry is keyed to
// DashboardCycleReferenceLength — the AVERAGE (28 here). Without a confirmed
// shift, dashboardCycleHeroOvulationDay must not anchor on that median-driven
// date: doing so pushes the "ovulation" card past the average cycle length and
// the whole hero disappears for an account that never recorded a thermal
// shift. The card must instead sit where CalcOvulationDay(cycleLength,
// LutealPhase) always put it before the confirmed-shift anchoring existed.
func TestDashboardCycleHeroIgnoresUnconfirmedOvulationDateOutsideTheAverageWindow(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	lastPeriodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	stats := CycleStats{
		CurrentCycleDay:     20,
		CurrentPhase:        "luteal",
		AveragePeriodLength: 5,
		LutealPhase:         14,
		AverageCycleLength:  28,
		MedianCycleLength:   45,
		LastPeriodStart:     lastPeriodStart,
		// A median-first projection for a 45-day cycle: cycle day 32, well past
		// the 28-day average the ribbon renders against.
		OvulationDate: AddCalendarDays(lastPeriodStart, 31, time.UTC),
	}

	hero := BuildDashboardCycleHero(user, stats, DashboardCycleContext{}, dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("the hero must render for an unconfirmed account even when stats.OvulationDate falls outside the average cycle length")
	}

	var ovulationCard DashboardCycleHeroPhaseCard
	for _, card := range hero.PhaseCards {
		if card.Phase == "ovulation" {
			ovulationCard = card
		}
	}
	wantDay, ok := CalcOvulationDay(28, stats.LutealPhase)
	if !ok {
		t.Fatal("fixture: CalcOvulationDay must succeed for a 28-day cycle")
	}
	if ovulationCard.StartDay != wantDay || ovulationCard.EndDay != wantDay {
		t.Fatalf(`the "ovulation" card must sit on CalcOvulationDay's day %d, got %d-%d`, wantDay, ovulationCard.StartDay, ovulationCard.EndDay)
	}
}

func expectCardRange(t *testing.T, card DashboardCycleHeroPhaseCard, phase string, startDay int, endDay int, isCurrent bool) {
	t.Helper()
	if card.Phase != phase {
		t.Fatalf("expected phase %q, got %q", phase, card.Phase)
	}
	if card.StartDay != startDay || card.EndDay != endDay {
		t.Fatalf("expected %s range %d-%d, got %d-%d", phase, startDay, endDay, card.StartDay, card.EndDay)
	}
	if card.IsCurrent != isCurrent {
		t.Fatalf("expected current=%v for %s, got %v", isCurrent, phase, card.IsCurrent)
	}
}
