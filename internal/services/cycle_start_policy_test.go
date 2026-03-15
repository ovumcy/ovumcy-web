package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestLatestCycleStartAnchorBeforeOrOnPrefersMoreRecentSettingsBaseline(t *testing.T) {
	userLastPeriod := mustParseCycleStartPolicyDay(t, "2026-03-13")
	user := &models.User{LastPeriodStart: &userLastPeriod}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseCycleStartPolicyDay(t, "2026-02-16"), IsPeriod: true, CycleStart: true},
	}

	anchor := LatestCycleStartAnchorBeforeOrOn(user, logs, mustParseCycleStartPolicyDay(t, "2026-03-14"), time.UTC)
	if got := anchor.Format("2006-01-02"); got != "2026-03-13" {
		t.Fatalf("expected latest anchor to use user baseline 2026-03-13, got %s", got)
	}
}

func TestShouldSuggestManualCycleStartUsesMostRecentKnownAnchor(t *testing.T) {
	userLastPeriod := mustParseCycleStartPolicyDay(t, "2026-03-13")
	user := &models.User{LastPeriodStart: &userLastPeriod}
	logs := []models.DailyLog{
		{Date: mustParseCycleStartPolicyDay(t, "2026-01-01"), IsPeriod: true, CycleStart: true},
		{Date: mustParseCycleStartPolicyDay(t, "2026-02-16"), IsPeriod: true, CycleStart: true},
	}
	logEntry := models.DailyLog{
		Date:     mustParseCycleStartPolicyDay(t, "2026-03-14"),
		IsPeriod: true,
	}
	now := mustParseCycleStartPolicyDay(t, "2026-03-14")

	if ShouldSuggestManualCycleStart(user, logs, logEntry, logEntry.Date, now, time.UTC) {
		t.Fatalf("expected no long-gap suggestion when a newer baseline already exists")
	}
}

func mustParseCycleStartPolicyDay(t *testing.T, raw string) time.Time {
	t.Helper()
	parsed, err := time.ParseInLocation("2006-01-02", raw, time.UTC)
	if err != nil {
		t.Fatalf("parse day %q: %v", raw, err)
	}
	return parsed
}
