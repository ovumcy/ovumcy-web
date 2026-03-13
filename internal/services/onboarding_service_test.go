package services

import (
	"errors"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type stubOnboardingRepo struct {
	user              models.User
	findErr           error
	completeErr       error
	completeCalled    bool
	completeStartDay  time.Time
	completePeriodLen int
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

func (stub *stubOnboardingRepo) SaveOnboardingStep2(uint, int, int, bool, bool) error {
	return nil
}

func (stub *stubOnboardingRepo) CompleteOnboarding(userID uint, startDay time.Time, periodLength int) error {
	stub.completeCalled = true
	stub.completeStartDay = startDay
	stub.completePeriodLen = periodLength
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
	if startDay.Hour() != 0 || startDay.Minute() != 0 {
		t.Fatalf("expected normalized start day, got %s", startDay.Format(time.RFC3339))
	}
}
