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

// TestDashboardCycleHeroConfirmedDayNarrowsThePeriodBandInsteadOfHiding is F2:
// the minimum confirmed ovulation day the shared "3-over-6" detector can ever
// name is cycle day 6, and a projected periodLength of 5 makes the OLD guard
// (`ovulationDay <= periodLength+1`) read `6 <= 6` and hide the whole hero —
// ribbon, phase cards, "today" marker — for the very cohort whose confirmed
// day is the most accurate signal available. The confirmed day is an
// OBSERVATION and periodLength is a PROJECTION of the average; the
// observation must narrow the projected band, not be hidden by it.
func TestDashboardCycleHeroConfirmedDayNarrowsThePeriodBandInsteadOfHiding(t *testing.T) {
	lastPeriodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	stats := CycleStats{
		CurrentCycleDay:     6,
		CurrentPhase:        "ovulation",
		AveragePeriodLength: 5,
		AverageCycleLength:  28,
		LutealPhase:         14,
		LastPeriodStart:     lastPeriodStart,
		// Confirmed cycle day 6: LastPeriodStart + 5 days.
		OvulationDate: AddCalendarDays(lastPeriodStart, 5, time.UTC),
	}
	cycleContext := DashboardCycleContext{DisplayOvulationConfirmed: true}

	hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("the hero must render for a confirmed day at the detector's own earliest floor (cycle day 6)")
	}

	var menstrualCard, follicularCard, ovulationCard DashboardCycleHeroPhaseCard
	for _, card := range hero.PhaseCards {
		switch card.Phase {
		case "menstrual":
			menstrualCard = card
		case "follicular":
			follicularCard = card
		case "ovulation":
			ovulationCard = card
		}
	}
	if menstrualCard.StartDay != 1 || menstrualCard.EndDay != 5 {
		t.Fatalf(`the "menstrual" card must stay 1-5 (the projected periodLength), got %d-%d`, menstrualCard.StartDay, menstrualCard.EndDay)
	}
	if follicularCard.EndDay >= follicularCard.StartDay {
		t.Fatalf(`the "follicular" card must be empty when periodLength borders the confirmed day, got %d-%d`, follicularCard.StartDay, follicularCard.EndDay)
	}
	if ovulationCard.StartDay != 6 || ovulationCard.EndDay != 6 {
		t.Fatalf(`the "ovulation" card must sit on the confirmed cycle day 6, got %d-%d`, ovulationCard.StartDay, ovulationCard.EndDay)
	}
}

// TestDashboardCycleHeroConfirmedDayClampsALongerProjectedPeriodBand is F2's
// second case: a confirmed day of 6 sits BEFORE the projected period band ends
// (AveragePeriodLength 7), so the observation must clamp the published
// menstrual band down to 5 days everywhere that band is read — the phase
// cards and dashboardCycleHeroCurrentPhase's own fallback — or the ribbon and
// the cards disagree with each other about how long the period was.
func TestDashboardCycleHeroConfirmedDayClampsALongerProjectedPeriodBand(t *testing.T) {
	lastPeriodStart := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	stats := CycleStats{
		CurrentCycleDay:     6,
		CurrentPhase:        "ovulation",
		AveragePeriodLength: 7,
		AverageCycleLength:  28,
		LutealPhase:         14,
		LastPeriodStart:     lastPeriodStart,
		OvulationDate:       AddCalendarDays(lastPeriodStart, 5, time.UTC),
	}
	cycleContext := DashboardCycleContext{DisplayOvulationConfirmed: true}

	hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("the hero must render when the confirmed day precedes the projected period band")
	}

	var menstrualCard DashboardCycleHeroPhaseCard
	for _, card := range hero.PhaseCards {
		if card.Phase == "menstrual" {
			menstrualCard = card
		}
	}
	if menstrualCard.StartDay != 1 || menstrualCard.EndDay != 5 {
		t.Fatalf(`the "menstrual" card must be clamped to 1-5 (confirmed day - 1), not the projected 7 days, got %d-%d`, menstrualCard.StartDay, menstrualCard.EndDay)
	}

	menstrualDays := 0
	predictedFlowDays := 0
	for _, day := range hero.Days {
		if day.Phase == "menstrual" {
			menstrualDays++
		}
		if day.IsPredictedFlow {
			predictedFlowDays++
		}
	}
	if menstrualDays != 5 {
		t.Fatalf("the ribbon must show 5 menstrual days, not the projected 7, got %d", menstrualDays)
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
