package services

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrOnboardingStepsRequired       = errors.New("complete onboarding steps first")
	ErrOnboardingCompletionNotNeeded = errors.New("onboarding completion not required")
	ErrOnboardingStartDateRequired   = errors.New("onboarding start date is required")
	ErrOnboardingStartDateOutOfRange = errors.New("onboarding start date out of range")
	ErrOnboardingStartDateInvalid    = errors.New("onboarding start date invalid")
	ErrOnboardingStep2InputInvalid   = errors.New("onboarding step2 input invalid")
)

type OnboardingUserRepository interface {
	FindByID(ctx context.Context, userID uint) (models.User, error)
	SaveOnboardingStep1(ctx context.Context, userID uint, start time.Time) error
	ClearOnboardingStart(ctx context.Context, userID uint) error
	SaveOnboardingStep2(ctx context.Context, userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, usageGoal string) error
	CompleteOnboarding(ctx context.Context, userID uint, start *time.Time, fillEndDay time.Time, autoPeriodFill bool, language string, weekStart string) error
}

type OnboardingService struct {
	users OnboardingUserRepository
}

func NewOnboardingService(users OnboardingUserRepository) *OnboardingService {
	return &OnboardingService{users: users}
}

func (service *OnboardingService) SaveStep1(ctx context.Context, userID uint, valuesStart time.Time) error {
	return service.users.SaveOnboardingStep1(ctx, userID, valuesStart)
}

// SkipStep1 answers step 1 with "I'll add it later". The account is left with
// no last period start — a date an earlier step-1 submit of the same onboarding
// stored is withdrawn — which is the state the account holds after clear-data,
// so every surface already knows how to render it. The answer itself is not
// stored: the caller carries it to the completion (see
// CompleteOnboardingSkippingStart), because the schema has no column for it.
func (service *OnboardingService) SkipStep1(ctx context.Context, userID uint) error {
	return service.users.ClearOnboardingStart(ctx, userID)
}

func (service *OnboardingService) ValidateStep1StartDate(start time.Time, now time.Time, location *time.Location) error {
	if start.IsZero() {
		return ErrOnboardingStartDateRequired
	}

	minDate, maxDate := OnboardingDateBounds(now, location)
	day := DateAtLocation(start, location)
	if day.Before(minDate) || day.After(maxDate) {
		return ErrOnboardingStartDateOutOfRange
	}

	return nil
}

func (service *OnboardingService) ValidateAndParseStep1StartDate(raw string, now time.Time, location *time.Location) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, ErrOnboardingStartDateRequired
	}

	parsed, err := ParseDayDate(value, location)
	if err != nil {
		return time.Time{}, ErrOnboardingStartDateInvalid
	}
	if err := service.ValidateStep1StartDate(parsed, now, location); err != nil {
		return time.Time{}, err
	}
	// Stored date-only field: canonicalize to UTC midnight of the same
	// calendar day (see day_utils.go on the two shapes).
	return CalendarDay(parsed, time.UTC), nil
}

// SaveStep2 persists the cycle defaults collected by the second onboarding
// step plus the usage goal. The age bracket is deliberately NOT among them:
// onboarding stopped asking for it, and the column is owned by the settings
// cycle form alone, so a step-2 save can never overwrite an answer given there.
func (service *OnboardingService) SaveStep2(ctx context.Context, userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, usageGoal string) (int, int, error) {
	safeCycleLength, safePeriodLength := SanitizeOnboardingCycleAndPeriod(cycleLength, periodLength)
	if err := service.users.SaveOnboardingStep2(
		ctx,
		userID,
		safeCycleLength,
		safePeriodLength,
		autoPeriodFill,
		irregularCycle,
		NormalizeUsageGoal(usageGoal),
	); err != nil {
		return 0, 0, err
	}
	return safeCycleLength, safePeriodLength, nil
}

// ParseAndNormalizeStep2Input normalizes the raw step-2 submission. An unset or
// unknown usage goal resolves to the neutral default, which is what the visible
// skip action submits.
func (service *OnboardingService) ParseAndNormalizeStep2Input(cycleRaw string, periodRaw string, autoPeriodFill bool, irregularCycle bool, usageGoal string) (int, int, bool, bool, string, error) {
	cycleLength, err := strconv.Atoi(strings.TrimSpace(cycleRaw))
	if err != nil {
		return 0, 0, false, false, "", ErrOnboardingStep2InputInvalid
	}
	periodLength, err := strconv.Atoi(strings.TrimSpace(periodRaw))
	if err != nil {
		return 0, 0, false, false, "", ErrOnboardingStep2InputInvalid
	}

	safeCycleLength, safePeriodLength := SanitizeOnboardingCycleAndPeriod(cycleLength, periodLength)
	return safeCycleLength, safePeriodLength, autoPeriodFill, irregularCycle, NormalizeUsageGoal(usageGoal), nil
}

// CompleteOnboardingForUser also settles the two locale-bound preferences the
// owner has had no chance to choose yet. language is the normalized, supported
// language the onboarding pages rendered in ("" when none applies): it is
// stored only while the account has none (WEB-216), and the week start follows
// the account's language — Settings, where it is chosen, is unreachable before
// onboarding completes (WEB-257).
func (service *OnboardingService) CompleteOnboardingForUser(ctx context.Context, userID uint, now time.Time, location *time.Location, language string) (time.Time, error) {
	return service.completeOnboarding(ctx, userID, now, location, language, false)
}

// CompleteOnboardingSkippingStart completes onboarding for an owner who
// answered step 1 with the skip. Where the account holds a start it behaves as
// CompleteOnboardingForUser (a start given after the skip wins); without one it
// completes with no start and seeds no day, and the returned day is the zero
// time. The refusal CompleteOnboardingForUser gives a start-less account stays
// for every caller that did not skip.
func (service *OnboardingService) CompleteOnboardingSkippingStart(ctx context.Context, userID uint, now time.Time, location *time.Location, language string) (time.Time, error) {
	return service.completeOnboarding(ctx, userID, now, location, language, true)
}

func (service *OnboardingService) completeOnboarding(ctx context.Context, userID uint, now time.Time, location *time.Location, language string, skippedStart bool) (time.Time, error) {
	current, err := service.users.FindByID(ctx, userID)
	if err != nil {
		return time.Time{}, err
	}
	if current.LastPeriodStart == nil && !skippedStart {
		return time.Time{}, ErrOnboardingStepsRequired
	}

	accountLanguage := strings.TrimSpace(current.InterfaceLanguage)
	languageToStore := ""
	if accountLanguage == "" {
		accountLanguage = language
		languageToStore = language
	}
	weekStart := DefaultWeekStartForLanguage(accountLanguage)

	if current.LastPeriodStart == nil {
		// Typed nil: the repository settles the flag and the preferences and
		// writes no day, so a zero time never reaches its fill loop.
		if err := service.users.CompleteOnboarding(ctx, userID, nil, time.Time{}, current.AutoPeriodFill, languageToStore, weekStart); err != nil {
			return time.Time{}, err
		}
		return time.Time{}, nil
	}

	startDay := CalendarDay(*current.LastPeriodStart, time.UTC)
	_, periodLength := SanitizeOnboardingCycleAndPeriod(current.CycleLength, current.PeriodLength)
	// The seeded period stops at the owner's local today, on the same bound as
	// the day auto-fill: a period still in progress is recorded only through the
	// day the owner has reached, never ahead of it. The bound is resolved in the
	// owner's location and handed over as the UTC-midnight calendar day the
	// repository iterates.
	fillEndDay := CalendarDay(periodFillLastDay(startDay, periodLength, now, location), time.UTC)
	if err := service.users.CompleteOnboarding(ctx, userID, &startDay, fillEndDay, current.AutoPeriodFill, languageToStore, weekStart); err != nil {
		return time.Time{}, err
	}
	return startDay, nil
}

func SanitizeOnboardingCycleAndPeriod(cycleLength int, periodLength int) (int, int) {
	safeCycleLength := ClampOnboardingCycleLength(cycleLength)
	safePeriodLength := ClampOnboardingPeriodLength(periodLength)

	maxPeriodLength := MaxPeriodLengthForCycle(safeCycleLength)
	if safePeriodLength > maxPeriodLength {
		safePeriodLength = maxPeriodLength
	}

	return safeCycleLength, safePeriodLength
}

func MaxPeriodLengthForCycle(cycleLength int) int {
	safeCycleLength := ClampOnboardingCycleLength(cycleLength)
	maxPeriodLength := safeCycleLength - minCycleReserveDays
	if maxPeriodLength < 1 {
		return 1
	}
	if maxPeriodLength > 14 {
		return 14
	}
	return maxPeriodLength
}

func IsCompatibleCycleAndPeriod(cycleLength int, periodLength int) bool {
	return ClampOnboardingPeriodLength(periodLength) <= MaxPeriodLengthForCycle(cycleLength)
}

func ClampOnboardingCycleLength(value int) int {
	if value < 15 {
		return 15
	}
	if value > 90 {
		return 90
	}
	return value
}

func ClampOnboardingPeriodLength(value int) int {
	if value < 1 {
		return 1
	}
	if value > 14 {
		return 14
	}
	return value
}

// MinOnboardingCycleLength is the shortest cycle length onboarding and Settings
// accept.
const MinOnboardingCycleLength = 15

func IsValidOnboardingCycleLength(value int) bool {
	return value >= MinOnboardingCycleLength && value <= 90
}

func IsValidOnboardingPeriodLength(value int) bool {
	return value >= 1 && value <= 14
}

func ResolveCycleAndPeriodDefaults(cycleLength int, periodLength int) (int, int) {
	resolvedCycleLength := cycleLength
	if !IsValidOnboardingCycleLength(resolvedCycleLength) {
		resolvedCycleLength = models.DefaultCycleLength
	}

	resolvedPeriodLength := periodLength
	if !IsValidOnboardingPeriodLength(resolvedPeriodLength) {
		resolvedPeriodLength = models.DefaultPeriodLength
	}

	return resolvedCycleLength, resolvedPeriodLength
}

// OnboardingStartDateWindowDays is how far back step 1 accepts a last-period
// start, in days before today. It is also the number the range error states in
// every locale ("within the last 60 days"), and the two are held together by
// TestOnboardingRangeCopyStatesTheEnforcedWindow.
const OnboardingStartDateWindowDays = 60

// OnboardingDateBounds returns the window step 1 accepts, which is also the
// window the date picker offers: the last OnboardingStartDateWindowDays days,
// ending today.
//
// The floor used to be raised to 1 January of the current year whenever that
// was the later of the two, which made the accepted window one single day on
// 1 January and shorter than the copy promised through all of January and
// February. Completion is impossible without a start date unless step 1 was
// skipped (ValidateOnboardingCompletionEligibility), so an owner onboarding in
// that stretch could only proceed by entering a date they knew to be wrong —
// and that date anchors the first cycle and every estimate built on it. The window
// crossing into the previous calendar year is the point, not a side effect.
func OnboardingDateBounds(now time.Time, location *time.Location) (time.Time, time.Time) {
	if location == nil {
		location = time.UTC
	}

	today := DateAtLocation(now.In(location), location)
	return AddCalendarDays(today, -OnboardingStartDateWindowDays, location), today
}

func RequiresOnboarding(user *models.User) bool {
	if user == nil {
		return false
	}
	return user.Role == models.RoleOwner && !user.OnboardingCompleted
}

// ValidateOnboardingCompletionEligibility refuses a completion with no last
// period start unless the owner answered step 1 with the skip.
func ValidateOnboardingCompletionEligibility(user *models.User, skippedStart bool) error {
	if !RequiresOnboarding(user) {
		return ErrOnboardingCompletionNotNeeded
	}
	if user.LastPeriodStart == nil && !skippedStart {
		return ErrOnboardingStepsRequired
	}
	return nil
}

func PostLoginRedirectPath(user *models.User) string {
	if RequiresOnboarding(user) {
		return "/onboarding"
	}
	return "/dashboard"
}
