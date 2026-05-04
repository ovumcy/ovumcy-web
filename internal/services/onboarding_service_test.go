package services

import (
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type stubOnboardingRepo struct {
	user              models.User
	findErr           error
	completeErr       error
	completeCalled    bool
	completeStartDay  time.Time
	completePeriodLen int
	completeAutoFill  bool
}

func (stub *stubOnboardingRepo) FindByID(uint) (models.User, error) {
	if stub.findErr != nil {
		return models.User{}, stub.findErr
	}
	return stub.user, nil
}

func (stub *stubOnboardingRepo) SaveOnboardingStep1(uint, time.Time) error {
	return nil
}

func (stub *stubOnboardingRepo) SaveOnboardingStep2(uint, int, int, bool, bool, string, string) error {
	return nil
}

func (stub *stubOnboardingRepo) CompleteOnboarding(userID uint, startDay time.Time, periodLength int, autoPeriodFill bool) error {
	stub.completeCalled = true
	stub.completeStartDay = startDay
	stub.completePeriodLen = periodLength
	stub.completeAutoFill = autoPeriodFill
	return stub.completeErr
}

func TestSanitizeOnboardingCycleAndPeriod(t *testing.T) {
	cycle, period := SanitizeOnboardingCycleAndPeriod(20, 19)
	if cycle != 20 || period != 10 {
		t.Fatalf("SanitizeOnboardingCycleAndPeriod() = (%d, %d), want (20, 10)", cycle, period)
	}
}

func TestCompleteOnboardingForUserRequiresStep1Date(t *testing.T) {
	service := NewOnboardingService(&stubOnboardingRepo{
		user: models.User{},
	})

	_, err := service.CompleteOnboardingForUser(1, time.UTC)
	if !errors.Is(err, ErrOnboardingStepsRequired) {
		t.Fatalf("expected ErrOnboardingStepsRequired, got %v", err)
	}
}

func TestCompleteOnboardingForUserNormalizesDateAndPeriod(t *testing.T) {
	location, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	original := time.Date(2026, 2, 10, 22, 45, 0, 0, time.UTC)
	repo := &stubOnboardingRepo{
		user: models.User{
			CycleLength:     22,
			PeriodLength:    20,
			AutoPeriodFill:  true,
			LastPeriodStart: &original,
		},
	}
	service := NewOnboardingService(repo)

	startDay, err := service.CompleteOnboardingForUser(1, location)
	if err != nil {
		t.Fatalf("CompleteOnboardingForUser() unexpected error: %v", err)
	}
	if !repo.completeCalled {
		t.Fatal("expected CompleteOnboarding() to be called")
	}
	if repo.completePeriodLen != 12 {
		t.Fatalf("expected sanitized period length 12, got %d", repo.completePeriodLen)
	}
	if !repo.completeAutoFill {
		t.Fatal("expected auto_period_fill to be forwarded to onboarding completion")
	}
	if startDay.Hour() != 0 || startDay.Minute() != 0 {
		t.Fatalf("expected normalized start day, got %s", startDay.Format(time.RFC3339))
	}
	if startDay.Location() != time.UTC {
		t.Fatalf("expected start day at UTC so user_repository.CompleteOnboarding iterates UTC-midnight bounds, got %s", startDay.Location())
	}
	if !repo.completeStartDay.Equal(startDay) {
		t.Fatalf("expected repo to receive same canonical start day, got repo=%s service=%s", repo.completeStartDay.Format(time.RFC3339), startDay.Format(time.RFC3339))
	}
	if repo.completeStartDay.Format("2006-01-02") != "2026-02-10" {
		t.Fatalf("expected calendar day 2026-02-10 preserved, got %s", repo.completeStartDay.Format("2006-01-02"))
	}
}
