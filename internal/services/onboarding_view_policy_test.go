package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestBuildOnboardingViewStateUsesUserValues(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	now := time.Date(2026, time.April, 15, 14, 0, 0, 0, location)
	// Canonical date-only storage: UTC midnight of the calendar day.
	// CalendarDay must surface the same calendar day in any viewer locale,
	// without the cross-timezone shift that previously caused issue #48.
	rawLastPeriod := time.Date(2026, time.March, 11, 0, 0, 0, 0, time.UTC)
	user := &models.User{
		CycleLength:     31,
		PeriodLength:    6,
		AutoPeriodFill:  true,
		IrregularCycle:  true,
		LastPeriodStart: &rawLastPeriod,
	}

	state := BuildOnboardingViewState(user, " 2 ", now, location)
	minDate, maxDate := OnboardingDateBounds(now, location)

	if state.Step != 2 {
		t.Fatalf("expected step 2, got %d", state.Step)
	}
	if !state.MinDate.Equal(minDate) {
		t.Fatalf("expected min date %s, got %s", minDate.Format("2006-01-02"), state.MinDate.Format("2006-01-02"))
	}
	if !state.MaxDate.Equal(maxDate) {
		t.Fatalf("expected max date %s, got %s", maxDate.Format("2006-01-02"), state.MaxDate.Format("2006-01-02"))
	}
	if state.LastPeriodStart == nil {
		t.Fatal("expected last period start to be set")
	}
	if got := state.LastPeriodStart.Format("2006-01-02"); got != "2026-03-11" {
		t.Fatalf("expected last period start 2026-03-11, got %s", got)
	}
	if state.CycleLength != 31 {
		t.Fatalf("expected cycle length 31, got %d", state.CycleLength)
	}
	if state.PeriodLength != 6 {
		t.Fatalf("expected period length 6, got %d", state.PeriodLength)
	}
	if !state.AutoPeriodFill {
		t.Fatal("expected auto period fill true")
	}
	if !state.IrregularCycle {
		t.Fatal("expected irregular cycle true")
	}
}

func TestBuildOnboardingViewStateUsesDefaultsForNilUser(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, time.April, 15, 9, 0, 0, 0, location)

	state := BuildOnboardingViewState(nil, "abc", now, location)
	minDate, maxDate := OnboardingDateBounds(now, location)

	if state.Step != 1 {
		t.Fatalf("expected step 1, got %d", state.Step)
	}
	if !state.MinDate.Equal(minDate) || !state.MaxDate.Equal(maxDate) {
		t.Fatalf("expected bounds %s..%s, got %s..%s",
			minDate.Format("2006-01-02"),
			maxDate.Format("2006-01-02"),
			state.MinDate.Format("2006-01-02"),
			state.MaxDate.Format("2006-01-02"),
		)
	}
	if state.LastPeriodStart != nil {
		t.Fatalf("expected nil last period start, got %s", state.LastPeriodStart.Format(time.RFC3339))
	}
	if state.CycleLength != models.DefaultCycleLength {
		t.Fatalf("expected default cycle length %d, got %d", models.DefaultCycleLength, state.CycleLength)
	}
	if state.PeriodLength != models.DefaultPeriodLength {
		t.Fatalf("expected default period length %d, got %d", models.DefaultPeriodLength, state.PeriodLength)
	}
	if state.AutoPeriodFill {
		t.Fatal("expected auto period fill false by default")
	}
	if state.IrregularCycle {
		t.Fatal("expected irregular cycle false by default")
	}
}
