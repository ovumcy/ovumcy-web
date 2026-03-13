package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestDetectCycleStarts(t *testing.T) {
	logs := []models.DailyLog{
		makeLog(t, "2025-01-01", true),
		makeLog(t, "2025-01-02", true),
		makeLog(t, "2025-01-03", true),
		makeLog(t, "2025-01-29", true),
		makeLog(t, "2025-01-30", true),
		makeLog(t, "2025-02-26", true),
	}

	starts := DetectCycleStarts(logs)
	if len(starts) != 3 {
		t.Fatalf("expected 3 cycle starts, got %d", len(starts))
	}

	expected := []string{"2025-01-01", "2025-01-29", "2025-02-26"}
	for i, day := range starts {
		if day.Format("2006-01-02") != expected[i] {
			t.Fatalf("expected cycle start %s, got %s", expected[i], day.Format("2006-01-02"))
		}
	}
}

func TestBuildCycleStats(t *testing.T) {
	logs := []models.DailyLog{}
	periodDays := []string{
		"2025-01-01", "2025-01-02", "2025-01-03", "2025-01-04",
		"2025-01-29", "2025-01-30", "2025-01-31", "2025-02-01",
		"2025-02-26", "2025-02-27", "2025-02-28", "2025-03-01",
	}
	for _, day := range periodDays {
		logs = append(logs, makeLog(t, day, true))
	}

	now := mustParseDay(t, "2025-03-05")
	stats := BuildCycleStats(logs, now)

	if stats.MedianCycleLength != 28 {
		t.Fatalf("expected median cycle length 28, got %d", stats.MedianCycleLength)
	}
	if stats.AverageCycleLength != 28 {
		t.Fatalf("expected average cycle length 28, got %.2f", stats.AverageCycleLength)
	}
	if stats.AveragePeriodLength != 4 {
		t.Fatalf("expected average period length 4, got %.2f", stats.AveragePeriodLength)
	}
	if stats.LastPeriodStart.Format("2006-01-02") != "2025-02-26" {
		t.Fatalf("unexpected last period start: %s", stats.LastPeriodStart.Format("2006-01-02"))
	}
	if stats.NextPeriodStart.Format("2006-01-02") != "2025-03-26" {
		t.Fatalf("unexpected next period start: %s", stats.NextPeriodStart.Format("2006-01-02"))
	}
	if stats.OvulationDate.Format("2006-01-02") != "2025-03-11" {
		t.Fatalf("unexpected ovulation date: %s", stats.OvulationDate.Format("2006-01-02"))
	}
	if !stats.OvulationExact {
		t.Fatalf("expected ovulation to be exact for regular cycle")
	}
	if stats.OvulationImpossible {
		t.Fatalf("expected ovulation impossible flag=false for regular cycle")
	}
	if stats.CurrentCycleDay != 8 {
		t.Fatalf("expected current cycle day 8, got %d", stats.CurrentCycleDay)
	}
	if stats.CurrentPhase != "follicular" {
		t.Fatalf("expected current phase follicular, got %s", stats.CurrentPhase)
	}
}

func TestBuildCycleStats_ShortCycleLongPeriodAllowsOverlapPredictions(t *testing.T) {
	logs := []models.DailyLog{}
	periodDays := []string{
		"2026-01-26", "2026-01-27", "2026-01-28", "2026-01-29", "2026-01-30",
		"2026-01-31", "2026-02-01", "2026-02-02", "2026-02-03", "2026-02-04",
		"2026-02-10", "2026-02-11", "2026-02-12", "2026-02-13", "2026-02-14",
		"2026-02-15", "2026-02-16", "2026-02-17", "2026-02-18", "2026-02-19",
	}
	for _, day := range periodDays {
		logs = append(logs, makeLog(t, day, true))
	}

	now := mustParseDay(t, "2026-02-12")
	stats := BuildCycleStats(logs, now)

	if got := stats.OvulationDate.Format("2006-01-02"); got != "2026-02-10" {
		t.Fatalf("expected ovulation date 2026-02-10, got %s", got)
	}
	if got := stats.FertilityWindowStart.Format("2006-01-02"); got != "2026-02-10" {
		t.Fatalf("expected fertility start 2026-02-10, got %s", got)
	}
	if got := stats.FertilityWindowEnd.Format("2006-01-02"); got != "2026-02-10" {
		t.Fatalf("expected fertility end 2026-02-10, got %s", got)
	}
	if !stats.OvulationExact {
		t.Fatalf("expected ovulation exact flag=true for short cycle")
	}
	if stats.OvulationImpossible {
		t.Fatalf("expected ovulation impossible flag=false for short cycle")
	}
}

func makeLog(t *testing.T, date string, isPeriod bool) models.DailyLog {
	day := mustParseDay(t, date)
	return models.DailyLog{
		Date:     day,
		IsPeriod: isPeriod,
		Flow:     models.FlowNone,
	}
}

func mustParseDay(t *testing.T, raw string) time.Time {
	t.Helper()

	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
