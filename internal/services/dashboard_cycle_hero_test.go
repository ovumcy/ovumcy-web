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

	hero := BuildDashboardCycleHero(user, stats, cycleContext)
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
	if len(hero.Segments) != 4 {
		t.Fatalf("expected 4 hero ring segments, got %d", len(hero.Segments))
	}
	if !hero.ShowCurrentDayMarker {
		t.Fatal("expected current-day marker")
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

	hero := BuildDashboardCycleHero(user, stats, cycleContext)
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
	})
	if sparse.Visible {
		t.Fatal("did not expect sparse irregular state to render segmented cycle hero")
	}

	disabled := BuildDashboardCycleHero(user, stats, DashboardCycleContext{
		PredictionDisabled: true,
	})
	if disabled.Visible {
		t.Fatal("did not expect unpredictable mode to render segmented cycle hero")
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
