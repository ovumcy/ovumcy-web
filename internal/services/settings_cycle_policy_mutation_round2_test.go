package services

import (
	"testing"
	"time"
)

func TestValidateCycleSettingsUsesRequestLocationForBoundsNearLocalMidnight(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}

	service := NewSettingsService(nil)
	// 00:30 on 2026-02-15 in UTC+9 is still 2026-02-14 15:30 in UTC.
	// The local calendar day (Tokyo) is 2026-02-15; the UTC day is 2026-02-14.
	now := time.Date(2026, time.February, 15, 0, 30, 0, 0, tokyo)

	update, err := service.ValidateCycleSettings(CycleSettingsValidationInput{
		CycleLength:        28,
		PeriodLength:       6,
		LastPeriodStartSet: true,
		LastPeriodStartRaw: "2026-02-15",
	}, now, tokyo)
	if err != nil {
		t.Fatalf("expected local-today 2026-02-15 to pass bounds in UTC+9, got %v", err)
	}
	if update.LastPeriodStart == nil {
		t.Fatal("expected non-nil last_period_start for accepted local-today date")
	}
	if got := update.LastPeriodStart.Format("2006-01-02"); got != "2026-02-15" {
		t.Fatalf("expected canonical last_period_start 2026-02-15, got %s", got)
	}
}

func TestSettingsCycleStartDateBoundsHonorsLocationNearLocalMidnight(t *testing.T) {
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("load Asia/Tokyo: %v", err)
	}

	// 00:30 on 2026-02-15 in UTC+9 is 2026-02-14 15:30 in UTC: local day and UTC day differ.
	now := time.Date(2026, time.February, 15, 0, 30, 0, 0, tokyo)

	minDate, today := SettingsCycleStartDateBounds(now, tokyo)
	if got := today.Format("2006-01-02"); got != "2026-02-15" {
		t.Fatalf("expected today computed in request location (UTC+9) = 2026-02-15, got %s", got)
	}
	if got := minDate.Format("2006-01-02"); got != "2026-01-01" {
		t.Fatalf("expected minDate = 2026-01-01, got %s", got)
	}

	// nil location must default to UTC (no panic) and yield UTC-based bounds.
	nowUTC := time.Date(2026, time.February, 15, 0, 30, 0, 0, time.UTC)
	minDateNil, todayNil := SettingsCycleStartDateBounds(nowUTC, nil)
	if got := todayNil.Format("2006-01-02"); got != "2026-02-15" {
		t.Fatalf("expected nil-location today defaulted to UTC = 2026-02-15, got %s", got)
	}
	if got := minDateNil.Format("2006-01-02"); got != "2026-01-01" {
		t.Fatalf("expected nil-location minDate = 2026-01-01, got %s", got)
	}
}
