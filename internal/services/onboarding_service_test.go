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
	user               models.User
	findErr            error
	completeErr        error
	completeCalled     bool
	completeStartDay   time.Time
	completeStartNil   bool
	clearCalled        bool
	clearUserID        uint
	completeFillEndDay time.Time
	completeAutoFill   bool
	completeLanguage   string
	completeWeekStart  string
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

func (stub *stubOnboardingRepo) ClearOnboardingStart(_ context.Context, userID uint) error {
	stub.clearCalled = true
	stub.clearUserID = userID
	return nil
}

func (stub *stubOnboardingRepo) CompleteOnboarding(ctx context.Context, userID uint, start *time.Time, fillEndDay time.Time, autoPeriodFill bool, language string, weekStart string) error {
	stub.completeLanguage = language
	stub.completeWeekStart = weekStart
	stub.completeCalled = true
	stub.completeUserID = userID
	stub.completeStartNil = start == nil
	if start != nil {
		stub.completeStartDay = *start
	}
	stub.completeFillEndDay = fillEndDay
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

	_, err := service.CompleteOnboardingForUser(context.Background(), onboardingFixtureUserID, time.Now(), time.UTC, "")
	if !errors.Is(err, ErrOnboardingStepsRequired) {
		t.Fatalf("expected ErrOnboardingStepsRequired, got %v", err)
	}
	if repo.findUserID != onboardingFixtureUserID {
		t.Fatalf("expected the eligibility read to be scoped to owner %d, got %d", onboardingFixtureUserID, repo.findUserID)
	}
	if repo.completeCalled {
		t.Fatal("a completion without a start and without the skip must write nothing")
	}
}

// TestCompleteOnboardingSkippingStartCompletesWithoutAStart pins the skip's
// completion: no start reaches the repository (a typed nil, never a zero time
// that would walk the fill from year 1), the baseline preferences still settle,
// and the account the step-1 skip cleared is scoped to the caller's owner id.
func TestCompleteOnboardingSkippingStartCompletesWithoutAStart(t *testing.T) {
	repo := &stubOnboardingRepo{user: models.User{CycleLength: 28, PeriodLength: 5, AutoPeriodFill: true}}
	service := NewOnboardingService(repo)

	if err := service.SkipStep1(context.Background(), onboardingFixtureUserID); err != nil {
		t.Fatalf("SkipStep1() unexpected error: %v", err)
	}
	if !repo.clearCalled || repo.clearUserID != onboardingFixtureUserID {
		t.Fatalf("expected the start cleared for owner %d, got called=%v owner=%d", onboardingFixtureUserID, repo.clearCalled, repo.clearUserID)
	}

	startDay, err := service.CompleteOnboardingSkippingStart(context.Background(), onboardingFixtureUserID, time.Now(), time.UTC, "de")
	if err != nil {
		t.Fatalf("CompleteOnboardingSkippingStart() unexpected error: %v", err)
	}
	if !repo.completeCalled || repo.completeUserID != onboardingFixtureUserID {
		t.Fatalf("expected completion for owner %d, got called=%v owner=%d", onboardingFixtureUserID, repo.completeCalled, repo.completeUserID)
	}
	if !repo.completeStartNil {
		t.Fatal("expected no start to reach the repository after a skip")
	}
	if !startDay.IsZero() || !repo.completeFillEndDay.IsZero() {
		t.Fatalf("expected no start day and no fill bound, got %s and %s", startDay, repo.completeFillEndDay)
	}
	if repo.completeLanguage != "de" || repo.completeWeekStart != models.WeekStartMonday {
		t.Fatalf("expected the locale preferences settled, got language %q week start %q", repo.completeLanguage, repo.completeWeekStart)
	}
}

// TestCompleteOnboardingSkippingStartReturnsTheRepositoryRefusal: a failed
// start-less completion reports the failure and no day, never a completed
// onboarding the account does not hold.
func TestCompleteOnboardingSkippingStartReturnsTheRepositoryRefusal(t *testing.T) {
	refusal := errors.New("complete refused")
	repo := &stubOnboardingRepo{user: models.User{CycleLength: 28, PeriodLength: 5}, completeErr: refusal}
	service := NewOnboardingService(repo)

	startDay, err := service.CompleteOnboardingSkippingStart(context.Background(), onboardingFixtureUserID, time.Now(), time.UTC, "en")
	if !errors.Is(err, refusal) {
		t.Fatalf("expected the repository refusal, got %v", err)
	}
	if !repo.completeCalled || !repo.completeStartNil {
		t.Fatalf("expected the start-less completion to have been attempted, got called=%v nil=%v", repo.completeCalled, repo.completeStartNil)
	}
	if !startDay.IsZero() {
		t.Fatalf("expected no start day on a refusal, got %s", startDay)
	}
}

// TestCompleteOnboardingSkippingStartKeepsAStartGivenAfterTheSkip: a date the
// account holds wins over the skip flag a stale form still carries.
func TestCompleteOnboardingSkippingStartKeepsAStartGivenAfterTheSkip(t *testing.T) {
	start := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	repo := &stubOnboardingRepo{user: models.User{CycleLength: 28, PeriodLength: 5, LastPeriodStart: &start}}
	service := NewOnboardingService(repo)

	startDay, err := service.CompleteOnboardingSkippingStart(context.Background(), onboardingFixtureUserID, start.AddDate(0, 1, 0), time.UTC, "")
	if err != nil {
		t.Fatalf("CompleteOnboardingSkippingStart() unexpected error: %v", err)
	}
	if repo.completeStartNil || !repo.completeStartDay.Equal(start) || !startDay.Equal(start) {
		t.Fatalf("expected the held start %s to be completed with, got nil=%v repo=%s service=%s", start, repo.completeStartNil, repo.completeStartDay, startDay)
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

	// Well past the period's end, so the fill bound is the sanitized length.
	now := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)
	startDay, err := service.CompleteOnboardingForUser(context.Background(), onboardingFixtureUserID, now, location, "")
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
	// Sanitized period length 12 from 2026-02-10: the fill ends on 2026-02-21,
	// handed over as the UTC midnight the repository iterates.
	wantFillEnd := time.Date(2026, 2, 21, 0, 0, 0, 0, time.UTC)
	if !repo.completeFillEndDay.Equal(wantFillEnd) || repo.completeFillEndDay.Location() != time.UTC {
		t.Fatalf("expected fill end %s (sanitized period length 12), got %s", wantFillEnd.Format(time.RFC3339), repo.completeFillEndDay.Format(time.RFC3339))
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

// TestCompleteOnboardingForUserSettlesTheAccountLocale pins WEB-216 and
// WEB-257: the onboarding language is stored only while the account has none,
// and the week start follows the account's language, a stored one first.
func TestCompleteOnboardingForUserSettlesTheAccountLocale(t *testing.T) {
	start := time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name          string
		stored        string
		rendered      string
		wantLanguage  string
		wantWeekStart string
	}{
		{name: "nothing stored, German pages", stored: "", rendered: "de", wantLanguage: "de", wantWeekStart: models.WeekStartMonday},
		{name: "nothing stored, English pages", stored: "", rendered: "en", wantLanguage: "en", wantWeekStart: models.WeekStartSunday},
		{name: "stored language wins", stored: "ru", rendered: "en", wantLanguage: "", wantWeekStart: models.WeekStartMonday},
		{name: "no language at all", stored: "", rendered: "", wantLanguage: "", wantWeekStart: models.DefaultWeekStart},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lastStart := start
			repo := &stubOnboardingRepo{user: models.User{
				CycleLength:       28,
				PeriodLength:      5,
				LastPeriodStart:   &lastStart,
				InterfaceLanguage: tc.stored,
			}}
			service := NewOnboardingService(repo)

			if _, err := service.CompleteOnboardingForUser(context.Background(), onboardingFixtureUserID, start.AddDate(0, 1, 0), time.UTC, tc.rendered); err != nil {
				t.Fatalf("CompleteOnboardingForUser() unexpected error: %v", err)
			}
			if repo.completeLanguage != tc.wantLanguage {
				t.Fatalf("expected language %q to be stored, got %q", tc.wantLanguage, repo.completeLanguage)
			}
			if repo.completeWeekStart != tc.wantWeekStart {
				t.Fatalf("expected week start %q, got %q", tc.wantWeekStart, repo.completeWeekStart)
			}
		})
	}
}
