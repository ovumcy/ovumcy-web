package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// onboardingFixtureUserID is deliberately not 1. Every onboarding write is
// scoped by the owner id the service forwards, and an id of 1 is
// indistinguishable from a hard-coded owner in a fresh fixture — an assertion
// written against 1 would be satisfied by the very defect it exists to catch.
const onboardingFixtureUserID = uint(42)

type stubOnboardingRepo struct {
	user              models.User
	findErr           error
	completeErr       error
	completeCalled    bool
	completeStartDay  time.Time
	completePeriodLen int
	completeAutoFill  bool
	// The four ids below record which owner each call was scoped to. The stub
	// serves a single embedded user, so no other assertion in this file can
	// observe the service resolving or writing the wrong account. Every method
	// records — including the ones no test drives yet, so a case written
	// against this stub later is instrumented by default rather than blind
	// while looking instrumented.
	findUserID     uint
	step1UserID    uint
	step1Start     time.Time
	step2UserID    uint
	completeUserID uint
}

func (stub *stubOnboardingRepo) FindByID(_ context.Context, userID uint) (models.User, error) {
	stub.findUserID = userID
	if stub.findErr != nil {
		return models.User{}, stub.findErr
	}
	return stub.user, nil
}

func (stub *stubOnboardingRepo) SaveOnboardingStep1(_ context.Context, userID uint, start time.Time) error {
	stub.step1UserID = userID
	stub.step1Start = start
	return nil
}

func (stub *stubOnboardingRepo) SaveOnboardingStep2(_ context.Context, userID uint, _ int, _ int, _ bool, _ bool, _ string) error {
	stub.step2UserID = userID
	return nil
}

func (stub *stubOnboardingRepo) CompleteOnboarding(ctx context.Context, userID uint, startDay time.Time, periodLength int, autoPeriodFill bool) error {
	stub.completeCalled = true
	stub.completeUserID = userID
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
	repo := &stubOnboardingRepo{user: models.User{}}
	service := NewOnboardingService(repo)

	_, err := service.CompleteOnboardingForUser(context.Background(), onboardingFixtureUserID, time.UTC)
	if !errors.Is(err, ErrOnboardingStepsRequired) {
		t.Fatalf("expected ErrOnboardingStepsRequired, got %v", err)
	}
	if repo.findUserID != onboardingFixtureUserID {
		t.Fatalf("expected the eligibility read to be scoped to owner %d, got %d", onboardingFixtureUserID, repo.findUserID)
	}
}

// TestOnboardingServiceSaveStep1ForwardsTheCallersOwnerID pins the owner id on
// the step-1 write. The repository stub accepts any id, so the forwarded value
// is the only observable that distinguishes a correctly scoped write from one
// that lands on a constant account.
func TestOnboardingServiceSaveStep1ForwardsTheCallersOwnerID(t *testing.T) {
	repo := &stubOnboardingRepo{}
	service := NewOnboardingService(repo)

	start := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	if err := service.SaveStep1(context.Background(), onboardingFixtureUserID, start); err != nil {
		t.Fatalf("SaveStep1() unexpected error: %v", err)
	}
	if repo.step1UserID != onboardingFixtureUserID {
		t.Fatalf("expected step 1 to be written for owner %d, got %d", onboardingFixtureUserID, repo.step1UserID)
	}
	if !repo.step1Start.Equal(start) {
		t.Fatalf("expected step 1 start %s, got %s", start, repo.step1Start)
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

	startDay, err := service.CompleteOnboardingForUser(context.Background(), onboardingFixtureUserID, location)
	if err != nil {
		t.Fatalf("CompleteOnboardingForUser() unexpected error: %v", err)
	}
	if !repo.completeCalled {
		t.Fatal("expected CompleteOnboarding() to be called")
	}
	if repo.findUserID != onboardingFixtureUserID {
		t.Fatalf("expected the baseline read to be scoped to owner %d, got %d", onboardingFixtureUserID, repo.findUserID)
	}
	if repo.completeUserID != onboardingFixtureUserID {
		t.Fatalf("expected completion to be written for owner %d, got %d", onboardingFixtureUserID, repo.completeUserID)
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
