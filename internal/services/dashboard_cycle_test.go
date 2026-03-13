package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestDashboardCycleReferenceLengthPrefersObservedAverage(t *testing.T) {
	user := &models.User{CycleLength: 29}
	stats := CycleStats{MedianCycleLength: 28, AverageCycleLength: 27}
	if got := DashboardCycleReferenceLength(user, stats); got != 27 {
		t.Fatalf("expected 27, got %d", got)
	}
}

func TestDashboardCycleStaleAnchorPrefersStatsBaseline(t *testing.T) {
	userBaseline := time.Date(2026, time.January, 1, 15, 30, 0, 0, time.UTC)
	statsBaseline := time.Date(2026, time.February, 20, 7, 0, 0, 0, time.UTC)
	user := &models.User{LastPeriodStart: &userBaseline}
	stats := CycleStats{LastPeriodStart: statsBaseline}

	anchor := DashboardCycleStaleAnchor(user, stats, time.UTC)
	if anchor.Format("2006-01-02") != "2026-02-20" {
		t.Fatalf("expected stats baseline date, got %s", anchor.Format("2006-01-02"))
	}
}

func TestCompletedCycleTrendLengths(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseDashboardDay(t, "2026-01-01"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-01-29"), IsPeriod: true},
		{Date: mustParseDashboardDay(t, "2026-02-26"), IsPeriod: true},
	}
	now := mustParseDashboardDay(t, "2026-03-10")

	got := CompletedCycleTrendLengths(logs, now, time.UTC)
	if len(got) != 2 || got[0] != 28 || got[1] != 28 {
		t.Fatalf("expected [28 28], got %#v", got)
	}
}

func TestBuildDashboardCycleContext(t *testing.T) {
	userStart := mustParseDashboardDay(t, "2026-02-10")
	user := &models.User{
		CycleLength:     28,
		PeriodLength:    5,
		LastPeriodStart: &userStart,
	}
	stats := CycleStats{
		CurrentCycleDay:     36,
		LastPeriodStart:     mustParseDashboardDay(t, "2026-02-10"),
		MedianCycleLength:   28,
		AveragePeriodLength: 5,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-03-10"),
		OvulationDate:       mustParseDashboardDay(t, "2026-02-24"),
	}
	today := mustParseDashboardDay(t, "2026-03-14")

	context := BuildDashboardCycleContext(user, stats, today, time.UTC)
	if context.CycleDayReference != 28 {
		t.Fatalf("expected cycle day reference 28, got %d", context.CycleDayReference)
	}
	if !context.CycleDayWarning {
		t.Fatalf("expected cycle day warning for long cycle day")
	}
	if !context.CycleDataStale {
		t.Fatalf("expected stale cycle data flag")
	}
	if !context.DisplayNextPeriodStart.IsZero() {
		t.Fatalf("expected exact next period date to be hidden for delayed cycle")
	}
	if !context.DisplayNextPeriodDelayed {
		t.Fatalf("expected delayed next period marker")
	}
}

func TestBuildDashboardCycleContextUsesRangeForIrregularMode(t *testing.T) {
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:    mustParseDashboardDay(t, "2026-03-01"),
		AverageCycleLength: 32,
		MinCycleLength:     24,
		MaxCycleLength:     45,
		CurrentCycleDay:    20,
	}
	today := mustParseDashboardDay(t, "2026-03-20")

	context := BuildDashboardCycleContext(user, stats, today, time.UTC)
	if !context.DisplayNextPeriodUseRange {
		t.Fatalf("expected irregular mode to use prediction range")
	}
	if got := context.DisplayNextPeriodRangeStart.Format("2006-01-02"); got != "2026-03-25" {
		t.Fatalf("expected range start 2026-03-25, got %s", got)
	}
	if got := context.DisplayNextPeriodRangeEnd.Format("2006-01-02"); got != "2026-04-15" {
		t.Fatalf("expected range end 2026-04-15, got %s", got)
	}
	if context.DisplayNextPeriodDelayed {
		t.Fatalf("expected irregular range to avoid delayed placeholder")
	}
}

func mustParseDashboardDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
