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

func TestApplyUserCycleBaselinePrefersMoreRecentSettingsBaselineOverOlderExplicitHistory(t *testing.T) {
	userLastPeriod := mustParseBaselineDay(t, "2026-03-13")
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     28,
		PeriodLength:    5,
		LastPeriodStart: &userLastPeriod,
	}

	logs := []models.DailyLog{
		{Date: mustParseBaselineDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2026-01-25"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseBaselineDay(t, "2026-03-10"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
	}

	now := mustParseBaselineDay(t, "2026-03-16")
	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, time.UTC)

	if got := stats.LastPeriodStart.Format("2006-01-02"); got != "2026-03-13" {
		t.Fatalf("expected more recent user baseline 2026-03-13, got %s", got)
	}
	if stats.CurrentCycleDay != 4 {
		t.Fatalf("expected current cycle day 4 from recent baseline, got %d", stats.CurrentCycleDay)
	}
}

func TestApplyUserCycleBaselineClampsShortCyclePredictionsAwayFromDayOne(t *testing.T) {
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

	if got := stats.OvulationDate.Format("2006-01-02"); got != "2026-02-14" {
		t.Fatalf("expected clamped ovulation date 2026-02-14, got %s", got)
	}
	if got := stats.FertilityWindowStart.Format("2006-01-02"); got != "2026-02-10" {
		t.Fatalf("expected fertility window to start on 2026-02-10, got %s", got)
	}
	if got := stats.FertilityWindowEnd.Format("2006-01-02"); got != "2026-02-14" {
		t.Fatalf("expected fertility window to end on 2026-02-14, got %s", got)
	}
	if stats.OvulationExact {
		t.Fatalf("expected exact=false for clamped short-cycle values")
	}
	if stats.OvulationImpossible {
		t.Fatalf("expected impossible=false for clamped short-cycle values")
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
