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

	days := BuildCalendarDayStates(nil, monthStart, logs, CycleStats{}, now, time.UTC)

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

	days := BuildCalendarDayStates(nil, monthStart, nil, stats, now, time.UTC)

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

	days := BuildCalendarDayStates(nil, monthStart, nil, stats, now, time.UTC)

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

func TestBuildCalendarDayStatesDisablesPredictionsForUnpredictableCycle(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC)

	stats := CycleStats{
		AveragePeriodLength:  5,
		LastPeriodStart:      time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:      time.Date(2026, time.April, 5, 0, 0, 0, 0, time.UTC),
		FertilityWindowStart: time.Date(2026, time.March, 18, 0, 0, 0, 0, time.UTC),
		FertilityWindowEnd:   time.Date(2026, time.March, 23, 0, 0, 0, 0, time.UTC),
		OvulationDate:        time.Date(2026, time.March, 23, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(&models.User{UnpredictableCycle: true}, monthStart, nil, stats, now, time.UTC)

	for _, dateString := range []string{"2026-03-08", "2026-03-18", "2026-03-23", "2026-04-04"} {
		day := findCalendarDayStateByDateString(t, days, dateString)
		if day.IsPredicted || day.IsPreFertile || day.IsFertility || day.IsOvulation || day.IsTentativeOvulation {
			t.Fatalf("expected unpredictable cycle mode to suppress predictions on %s, got %#v", dateString, day)
		}
	}
}

func TestBuildCalendarDayStatesMarksTentativeOvulationWhenBBTHasNoShift(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{Date: time.Date(2026, time.March, 1, 7, 0, 0, 0, time.UTC), BBT: 36.40},
		{Date: time.Date(2026, time.March, 2, 7, 0, 0, 0, time.UTC), BBT: 36.42},
		{Date: time.Date(2026, time.March, 3, 7, 0, 0, 0, time.UTC), BBT: 36.41},
		{Date: time.Date(2026, time.March, 4, 7, 0, 0, 0, time.UTC), BBT: 36.39},
		{Date: time.Date(2026, time.March, 5, 7, 0, 0, 0, time.UTC), BBT: 36.43},
		{Date: time.Date(2026, time.March, 6, 7, 0, 0, 0, time.UTC), BBT: 36.44},
		{Date: time.Date(2026, time.March, 7, 7, 0, 0, 0, time.UTC), BBT: 36.45},
		{Date: time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC), BBT: 36.43},
	}

	stats := CycleStats{
		LastPeriodStart:      time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:      time.Date(2026, time.March, 29, 0, 0, 0, 0, time.UTC),
		OvulationDate:        time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
		FertilityWindowStart: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		FertilityWindowEnd:   time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(&models.User{TrackBBT: true}, monthStart, logs, stats, now, time.UTC)

	ovulationDay := findCalendarDayStateByDateString(t, days, "2026-03-15")
	if !ovulationDay.IsTentativeOvulation {
		t.Fatalf("expected tentative ovulation marker on 2026-03-15, got %#v", ovulationDay)
	}
	if ovulationDay.IsOvulation {
		t.Fatalf("expected confirmed ovulation marker to be removed when no BBT shift is present")
	}
}

func TestBuildCalendarDayStatesSeparatesFertilityEdgeAndPeak(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC)

	stats := CycleStats{
		LastPeriodStart:      time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:      time.Date(2026, time.March, 29, 0, 0, 0, 0, time.UTC),
		OvulationDate:        time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
		FertilityWindowStart: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		FertilityWindowEnd:   time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(nil, monthStart, nil, stats, now, time.UTC)

	edgeDay := findCalendarDayStateByDateString(t, days, "2026-03-10")
	if !edgeDay.IsFertilityEdge || edgeDay.IsFertilityPeak || !edgeDay.IsFertility {
		t.Fatalf("expected 2026-03-10 to render as fertile edge, got %#v", edgeDay)
	}

	peakDay := findCalendarDayStateByDateString(t, days, "2026-03-14")
	if peakDay.IsFertilityEdge || !peakDay.IsFertilityPeak || !peakDay.IsFertility {
		t.Fatalf("expected 2026-03-14 to render as fertile peak, got %#v", peakDay)
	}

	ovulationDay := findCalendarDayStateByDateString(t, days, "2026-03-15")
	if !ovulationDay.IsOvulation || !ovulationDay.IsFertilityPeak || ovulationDay.IsFertility {
		t.Fatalf("expected ovulation day to keep the peak marker without fertile fill, got %#v", ovulationDay)
	}
}

func TestBuildCalendarDayStatesKeepsConfirmedOvulationWhenBBTHasShift(t *testing.T) {
	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, time.March, 17, 0, 0, 0, 0, time.UTC)

	logs := []models.DailyLog{
		{Date: time.Date(2026, time.March, 10, 7, 0, 0, 0, time.UTC), BBT: 36.40},
		{Date: time.Date(2026, time.March, 11, 7, 0, 0, 0, time.UTC), BBT: 36.42},
		{Date: time.Date(2026, time.March, 12, 7, 0, 0, 0, time.UTC), BBT: 36.41},
		{Date: time.Date(2026, time.March, 13, 7, 0, 0, 0, time.UTC), BBT: 36.39},
		{Date: time.Date(2026, time.March, 14, 7, 0, 0, 0, time.UTC), BBT: 36.43},
		{Date: time.Date(2026, time.March, 15, 7, 0, 0, 0, time.UTC), BBT: 36.66},
		{Date: time.Date(2026, time.March, 16, 7, 0, 0, 0, time.UTC), BBT: 36.67},
		{Date: time.Date(2026, time.March, 17, 7, 0, 0, 0, time.UTC), BBT: 36.69},
	}

	stats := CycleStats{
		LastPeriodStart:      time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		NextPeriodStart:      time.Date(2026, time.April, 7, 0, 0, 0, 0, time.UTC),
		OvulationDate:        time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
		FertilityWindowStart: time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		FertilityWindowEnd:   time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC),
	}

	days := BuildCalendarDayStates(&models.User{TrackBBT: true}, monthStart, logs, stats, now, time.UTC)

	ovulationDay := findCalendarDayStateByDateString(t, days, "2026-03-15")
	if !ovulationDay.IsOvulation {
		t.Fatalf("expected confirmed ovulation marker to remain when BBT shift exists, got %#v", ovulationDay)
	}
	if ovulationDay.IsTentativeOvulation {
		t.Fatalf("expected tentative ovulation marker to stay off when BBT shift exists")
	}
}
