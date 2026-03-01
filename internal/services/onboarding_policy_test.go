package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestOnboardingDateBounds_UsesYearStartWhenWithinFirstSixtyDays(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	now := time.Date(2026, time.February, 15, 18, 45, 0, 0, location)
	minDate, maxDate := OnboardingDateBounds(now, location)

	expectedMin := time.Date(2026, time.January, 1, 0, 0, 0, 0, location)
	expectedMax := time.Date(2026, time.February, 15, 0, 0, 0, 0, location)
	if !minDate.Equal(expectedMin) {
		t.Fatalf("expected min date %s, got %s", expectedMin.Format(time.RFC3339), minDate.Format(time.RFC3339))
	}
	if !maxDate.Equal(expectedMax) {
		t.Fatalf("expected max date %s, got %s", expectedMax.Format(time.RFC3339), maxDate.Format(time.RFC3339))
	}
}

func TestOnboardingDateBounds_UsesRollingSixtyDaysAfterWindow(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	now := time.Date(2026, time.April, 15, 9, 10, 0, 0, location)
	minDate, maxDate := OnboardingDateBounds(now, location)

	expectedMin := time.Date(2026, time.February, 14, 0, 0, 0, 0, location)
	expectedMax := time.Date(2026, time.April, 15, 0, 0, 0, 0, location)
	if !minDate.Equal(expectedMin) {
		t.Fatalf("expected min date %s, got %s", expectedMin.Format(time.RFC3339), minDate.Format(time.RFC3339))
	}
	if !maxDate.Equal(expectedMax) {
		t.Fatalf("expected max date %s, got %s", expectedMax.Format(time.RFC3339), maxDate.Format(time.RFC3339))
	}
}

func TestValidateStep1StartDate_RejectsRequiredAndOutOfRange(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	service := NewOnboardingService(nil)
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, location)

	if err := service.ValidateStep1StartDate(time.Time{}, now, location); !errors.Is(err, ErrOnboardingStartDateRequired) {
		t.Fatalf("expected ErrOnboardingStartDateRequired, got %v", err)
	}

	tooOld := time.Date(2026, time.February, 13, 0, 0, 0, 0, location)
	if err := service.ValidateStep1StartDate(tooOld, now, location); !errors.Is(err, ErrOnboardingStartDateOutOfRange) {
		t.Fatalf("expected ErrOnboardingStartDateOutOfRange for old date, got %v", err)
	}

	future := now.AddDate(0, 0, 1)
	if err := service.ValidateStep1StartDate(future, now, location); !errors.Is(err, ErrOnboardingStartDateOutOfRange) {
		t.Fatalf("expected ErrOnboardingStartDateOutOfRange for future date, got %v", err)
	}
}

func TestValidateStep1StartDate_AcceptsBoundaries(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	service := NewOnboardingService(nil)
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, location)
	minDate, maxDate := OnboardingDateBounds(now, location)

	if err := service.ValidateStep1StartDate(minDate, now, location); err != nil {
		t.Fatalf("expected nil error for min boundary, got %v", err)
	}
	if err := service.ValidateStep1StartDate(maxDate, now, location); err != nil {
		t.Fatalf("expected nil error for max boundary, got %v", err)
	}
}

func TestValidateAndParseStep1StartDate(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	service := NewOnboardingService(nil)
	now := time.Date(2026, time.April, 15, 10, 0, 0, 0, location)

	if _, err := service.ValidateAndParseStep1StartDate("", now, location); !errors.Is(err, ErrOnboardingStartDateRequired) {
		t.Fatalf("expected ErrOnboardingStartDateRequired, got %v", err)
	}
	if _, err := service.ValidateAndParseStep1StartDate("invalid", now, location); !errors.Is(err, ErrOnboardingStartDateInvalid) {
		t.Fatalf("expected ErrOnboardingStartDateInvalid, got %v", err)
	}
	if _, err := service.ValidateAndParseStep1StartDate("2026-02-13", now, location); !errors.Is(err, ErrOnboardingStartDateOutOfRange) {
		t.Fatalf("expected ErrOnboardingStartDateOutOfRange, got %v", err)
	}

	parsed, err := service.ValidateAndParseStep1StartDate("2026-04-10", now, location)
	if err != nil {
		t.Fatalf("expected valid step1 date, got %v", err)
	}
	if parsed.Format("2006-01-02") != "2026-04-10" {
		t.Fatalf("expected parsed date 2026-04-10, got %s", parsed.Format("2006-01-02"))
	}
}

func TestResolveCycleAndPeriodDefaults(t *testing.T) {
	t.Run("keeps valid values", func(t *testing.T) {
		cycleLength, periodLength := ResolveCycleAndPeriodDefaults(29, 6)
		if cycleLength != 29 || periodLength != 6 {
			t.Fatalf("expected 29/6, got %d/%d", cycleLength, periodLength)
		}
	})

	t.Run("falls back for invalid values", func(t *testing.T) {
		cycleLength, periodLength := ResolveCycleAndPeriodDefaults(120, 0)
		if cycleLength != models.DefaultCycleLength || periodLength != models.DefaultPeriodLength {
			t.Fatalf("expected defaults %d/%d, got %d/%d", models.DefaultCycleLength, models.DefaultPeriodLength, cycleLength, periodLength)
		}
	})
}

func TestParseAndNormalizeStep2Input(t *testing.T) {
	service := NewOnboardingService(nil)

	if _, _, _, err := service.ParseAndNormalizeStep2Input("invalid", "5", true); !errors.Is(err, ErrOnboardingStep2InputInvalid) {
		t.Fatalf("expected ErrOnboardingStep2InputInvalid for invalid cycle, got %v", err)
	}
	if _, _, _, err := service.ParseAndNormalizeStep2Input("28", "invalid", true); !errors.Is(err, ErrOnboardingStep2InputInvalid) {
		t.Fatalf("expected ErrOnboardingStep2InputInvalid for invalid period, got %v", err)
	}

	cycleLength, periodLength, autoPeriodFill, err := service.ParseAndNormalizeStep2Input("14", "20", true)
	if err != nil {
		t.Fatalf("expected valid step2 input after normalize, got %v", err)
	}
	if cycleLength != 15 || periodLength != 7 {
		t.Fatalf("expected normalized values 15/7, got %d/%d", cycleLength, periodLength)
	}
	if !autoPeriodFill {
		t.Fatalf("expected autoPeriodFill=true")
	}
}

func TestOnboardingRedirectPolicy(t *testing.T) {
	ownerPending := &models.User{Role: models.RoleOwner, OnboardingCompleted: false}
	if !RequiresOnboarding(ownerPending) {
		t.Fatalf("expected owner without onboarding to require onboarding")
	}
	if path := PostLoginRedirectPath(ownerPending); path != "/onboarding" {
		t.Fatalf("expected onboarding redirect, got %q", path)
	}

	ownerCompleted := &models.User{Role: models.RoleOwner, OnboardingCompleted: true}
	if RequiresOnboarding(ownerCompleted) {
		t.Fatalf("did not expect completed owner to require onboarding")
	}
	if path := PostLoginRedirectPath(ownerCompleted); path != "/dashboard" {
		t.Fatalf("expected dashboard redirect, got %q", path)
	}

	partner := &models.User{Role: models.RolePartner, OnboardingCompleted: false}
	if RequiresOnboarding(partner) {
		t.Fatalf("did not expect partner to require onboarding")
	}
	if path := PostLoginRedirectPath(partner); path != "/dashboard" {
		t.Fatalf("expected dashboard redirect for partner, got %q", path)
	}

	if RequiresOnboarding(nil) {
		t.Fatalf("did not expect nil user to require onboarding")
	}
	if path := PostLoginRedirectPath(nil); path != "/dashboard" {
		t.Fatalf("expected dashboard redirect for nil user, got %q", path)
	}
}

func TestResolveOnboardingStep(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "valid middle step", raw: "2", want: 2},
		{name: "negative clamped to zero", raw: "-1", want: 0},
		{name: "too large clamped to three", raw: "99", want: 3},
		{name: "invalid defaults to zero", raw: "abc", want: 0},
		{name: "trimmed value accepted", raw: " 3 ", want: 3},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ResolveOnboardingStep(testCase.raw); got != testCase.want {
				t.Fatalf("ResolveOnboardingStep(%q) = %d, want %d", testCase.raw, got, testCase.want)
			}
		})
	}
}
