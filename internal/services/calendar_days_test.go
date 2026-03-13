package services

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestBuildCalendarDayStatesUsesLatestLogPerDateDeterministically(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 20, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{
			ID:       20,
			Date:     time.Date(2026, time.February, 17, 20, 0, 0, 0, time.UTC),
			IsPeriod: false,
			Flow:     models.FlowNone,
		},
		{
			ID:       10,
			Date:     time.Date(2026, time.February, 17, 8, 0, 0, 0, time.UTC),
			IsPeriod: true,
			Flow:     models.FlowMedium,
		},
		{
			ID:       30,
			Date:     time.Date(2026, time.February, 18, 9, 0, 0, 0, time.UTC),
			IsPeriod: true,
			Flow:     models.FlowMedium,
		},
		{
			ID:       31,
			Date:     time.Date(2026, time.February, 18, 9, 0, 0, 0, time.UTC),
			IsPeriod: false,
			Flow:     models.FlowNone,
		},
	}

	days := BuildCalendarDayStates(monthStart, logs, CycleStats{}, now, time.UTC)

	day17 := findCalendarDayStateByDateString(t, days, "2026-02-17")
	if day17.IsPeriod {
		t.Fatalf("expected 2026-02-17 period=false from latest log, got true")
	}

	day18 := findCalendarDayStateByDateString(t, days, "2026-02-18")
	if day18.IsPeriod {
		t.Fatalf("expected 2026-02-18 period=false from highest id tie-breaker, got true")
	}
}

func TestCalendarLogRange(t *testing.T) {
	monthStart := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	from, to := CalendarLogRange(monthStart)

	if from.Format("2006-01-02") != "2025-11-23" {
		t.Fatalf("expected range start 2025-11-23, got %s", from.Format("2006-01-02"))
	}
	if to.Format("2006-01-02") != "2026-05-09" {
		t.Fatalf("expected range end 2026-05-09, got %s", to.Format("2006-01-02"))
	}
}

func TestBuildCalendarDayStatesProjectsOvulationIntoFutureCycles(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.February, 23, 0, 0, 0, 0, time.UTC)

	stats := CycleStats{
		MedianCycleLength:    28,
		AveragePeriodLength:  5,
		LastPeriodStart:      time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:      time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		OvulationDate:        time.Date(2026, time.February, 23, 0, 0, 0, 0, time.UTC),
		FertilityWindowStart: time.Date(2026, time.February, 18, 0, 0, 0, 0, time.UTC),
		FertilityWindowEnd:   time.Date(2026, time.February, 24, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(monthStart, nil, stats, now, time.UTC)

	ovulationDay := findCalendarDayStateByDateString(t, days, "2026-03-23")
	if !ovulationDay.IsOvulation {
		t.Fatalf("expected projected ovulation marker on 2026-03-23")
	}
	if ovulationDay.IsFertility {
		t.Fatalf("expected ovulation day to not be marked as fertile state")
	}
	if ovulationDay.IsPredicted {
		t.Fatalf("did not expect ovulation day to be marked as predicted period")
	}

	preFertileDay := findCalendarDayStateByDateString(t, days, "2026-03-16")
	if !preFertileDay.IsPreFertile {
		t.Fatalf("expected projected pre-fertile marker on 2026-03-16")
	}
	if preFertileDay.IsPredicted || preFertileDay.IsFertility || preFertileDay.IsOvulation {
		t.Fatalf("expected pre-fertile day to be distinct from predicted period, fertile, and ovulation states")
	}
}

func TestBuildCalendarDayStatesIncludesCurrentBaselinePeriodWindow(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC)

	stats := CycleStats{
		AveragePeriodLength: 5,
		LastPeriodStart:     time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(monthStart, nil, stats, now, time.UTC)

	for _, dateString := range []string{"2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11", "2026-03-12"} {
		day := findCalendarDayStateByDateString(t, days, dateString)
		if !day.IsPredicted {
			t.Fatalf("expected baseline period day %s to be marked as predicted period", dateString)
		}
	}

	for _, dateString := range []string{"2026-03-13", "2026-03-14", "2026-03-15"} {
		day := findCalendarDayStateByDateString(t, days, dateString)
		if !day.IsPreFertile {
			t.Fatalf("expected baseline pre-fertile day %s to be marked as low-risk gap", dateString)
		}
		if day.IsPredicted || day.IsFertility || day.IsOvulation {
			t.Fatalf("expected baseline pre-fertile day %s to stay distinct from other prediction states", dateString)
		}
	}
}

func findCalendarDayStateByDateString(t *testing.T, days []CalendarDayState, date string) CalendarDayState {
	t.Helper()
	for _, day := range days {
		if day.DateString == date {
			return day
		}
	}
	t.Fatalf("calendar day %s not found", date)
	return CalendarDayState{}
}
