package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestApplyUserCycleBaselineUsesSettingsFallbackWhenNoExplicitCycleStartExists(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2026-02-07")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     29,
		PeriodLength:    6,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-02-07"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2026-02-16"), IsPeriod: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2026-02-17")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if stats.AverageCycleLength != 9 {
		t.Fatalf("expected average cycle length 9, got %.2f", stats.AverageCycleLength)
	}
	if stats.MedianCycleLength != 9 {
		t.Fatalf("expected median cycle length 9, got %d", stats.MedianCycleLength)
	}
	if stats.AveragePeriodLength != 1 {
		t.Fatalf("expected average period length 1, got %.2f", stats.AveragePeriodLength)
	}
	if stats.LastPeriodStart.Format("2006-01-02") != "2026-02-07" {
		t.Fatalf("expected settings fallback last period start 2026-02-07, got %s", stats.LastPeriodStart.Format("2006-01-02"))
	}
	if stats.NextPeriodStart.Format("2006-01-02") != "2026-02-16" {
		t.Fatalf("expected next period start 2026-02-16, got %s", stats.NextPeriodStart.Format("2006-01-02"))
	}
	if stats.CurrentCycleDay != 11 {
		t.Fatalf("expected current cycle day 11, got %d", stats.CurrentCycleDay)
	}
	if stats.CurrentPhase != "unknown" {
		t.Fatalf("expected unknown phase for incompatible projected cycle, got %s", stats.CurrentPhase)
	}
}

func TestApplyUserCycleBaselinePrefersExplicitCycleStartOverSettingsFallback(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2025-03-27")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     29,
		PeriodLength:    6,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2025-01-01"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-02"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-03"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-29"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-30"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-01-31"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-02-26"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-02-27"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2025-02-28"), IsPeriod: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2025-03-05")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if stats.AverageCycleLength != 28 {
		t.Fatalf("expected reliable average cycle length 28, got %.2f", stats.AverageCycleLength)
	}
	if stats.MedianCycleLength != 28 {
		t.Fatalf("expected reliable median cycle length 28, got %d", stats.MedianCycleLength)
	}
	if stats.LastPeriodStart.Format("2006-01-02") != "2025-02-26" {
		t.Fatalf("expected explicit last period start 2025-02-26, got %s", stats.LastPeriodStart.Format("2006-01-02"))
	}
}

func TestApplyUserCycleBaselineMarksIncompatibleCycleAsUncalculable(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2026-02-10")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     15,
		PeriodLength:    10,
		LutealPhase:     15,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-02-10"), IsPeriod: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2026-02-12")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if !stats.OvulationDate.IsZero() {
		t.Fatalf("expected empty ovulation date for incompatible values, got %s", stats.OvulationDate.Format("2006-01-02"))
	}
	if !stats.FertilityWindowStart.IsZero() || !stats.FertilityWindowEnd.IsZero() {
		t.Fatalf("expected empty fertility window for incompatible values")
	}
	if stats.OvulationExact {
		t.Fatalf("expected exact=false for incompatible values")
	}
	if !stats.OvulationImpossible {
		t.Fatalf("expected impossible=true for incompatible values")
	}
}

func mustParseBaselineDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
