package services

import (
	"context"
	"errors"
	"testing"
	"time"
)

// onboardingserviceCovStep2Repo is a stub that supports injecting a SaveOnboardingStep2 error.
type onboardingserviceCovStep2Repo struct {
	stubOnboardingRepo
	saveStep2Err    error
	saveStep2Called bool
	savedCycle      int
	savedPeriod     int
}

func (s *onboardingserviceCovStep2Repo) SaveOnboardingStep2(ctx context.Context, userID uint, cycleLength int, periodLength int, autoPeriodFill bool, irregularCycle bool, ageGroup string, usageGoal string) error {
	s.saveStep2Called = true
	s.savedCycle = cycleLength
	s.savedPeriod = periodLength
	return s.saveStep2Err
}

// ---------------------------------------------------------------------------
// Line 80: NOT COVERED — SaveStep2 error path
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovSaveStep2PropagatesRepoError(t *testing.T) {
	sentinel := errors.New("db unavailable")
	repo := &onboardingserviceCovStep2Repo{saveStep2Err: sentinel}
	svc := NewOnboardingService(repo)

	_, _, err := svc.SaveStep2(context.Background(), 1, 28, 5, false, false, "", "")
	if !errors.Is(err, sentinel) {
		t.Fatalf("SaveStep2 should propagate repo error, got %v", err)
	}
}

func TestOnboardingserviceCovSaveStep2ReturnsSanitizedValues(t *testing.T) {
	repo := &onboardingserviceCovStep2Repo{}
	svc := NewOnboardingService(repo)

	// cycle=100 → clamped to 90, period=20 → clamped to 14, then capped to MaxPeriodLengthForCycle(90)=14
	gotCycle, gotPeriod, err := svc.SaveStep2(context.Background(), 1, 100, 20, false, false, "", "")
	if err != nil {
		t.Fatalf("SaveStep2 unexpected error: %v", err)
	}
	if gotCycle != 90 {
		t.Fatalf("SaveStep2 cycle: got %d, want 90", gotCycle)
	}
	if gotPeriod != 14 {
		t.Fatalf("SaveStep2 period: got %d, want 14", gotPeriod)
	}
	if repo.savedCycle != 90 || repo.savedPeriod != 14 {
		t.Fatalf("repo received cycle=%d period=%d, want 90/14", repo.savedCycle, repo.savedPeriod)
	}
}

// ---------------------------------------------------------------------------
// Line 122: if safePeriodLength > maxPeriodLength — should NOT clamp when equal
// ---------------------------------------------------------------------------

// When safePeriodLength == maxPeriodLength the period must NOT be reduced.
// A >= mutant would leave period unchanged here (same result), but testing the
// boundary explicitly ensures the > predicate is correct.
func TestOnboardingserviceCovSanitizePeriodNotCappedWhenExactlyAtMax(t *testing.T) {
	// cycle=20 → maxPeriodLength = 20-10 = 10
	// period=10 → ClampOnboardingPeriodLength(10) = 10
	// safePeriodLength(10) == maxPeriodLength(10): must NOT reduce period.
	_, period := SanitizeOnboardingCycleAndPeriod(20, 10)
	if period != 10 {
		t.Fatalf("SanitizeOnboardingCycleAndPeriod(20,10): got period %d, want 10", period)
	}
}

// When safePeriodLength == maxPeriodLength + 1 it must be capped down.
func TestOnboardingserviceCovSanitizePeriodCappedWhenOneAboveMax(t *testing.T) {
	// cycle=20 → maxPeriodLength=10; period=11 → ClampOnboardingPeriodLength(11)=11 > 10 → must become 10
	_, period := SanitizeOnboardingCycleAndPeriod(20, 11)
	if period != 10 {
		t.Fatalf("SanitizeOnboardingCycleAndPeriod(20,11): got period %d, want 10", period)
	}
}

// ---------------------------------------------------------------------------
// Line 132: if maxPeriodLength < 1 — unreachable (ClampOnboardingCycleLength
// guarantees safeCycleLength >= 15, so maxPeriodLength >= 5). EQUIVALENT.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Line 135: if maxPeriodLength > 14 — boundary at cycle=24 (max=14) vs cycle=25 (max=15→capped to 14)
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovMaxPeriodLengthForCycleCapsAt14(t *testing.T) {
	// cycle=25: maxPeriodLength = 25-10 = 15 > 14 → must return 14
	got := MaxPeriodLengthForCycle(25)
	if got != 14 {
		t.Fatalf("MaxPeriodLengthForCycle(25): got %d, want 14", got)
	}
}

func TestOnboardingserviceCovMaxPeriodLengthForCycleNotCappedAt14(t *testing.T) {
	// cycle=24: maxPeriodLength = 24-10 = 14, not > 14 → must return 14 (not reduced further)
	got := MaxPeriodLengthForCycle(24)
	if got != 14 {
		t.Fatalf("MaxPeriodLengthForCycle(24): got %d, want 14", got)
	}
}

func TestOnboardingserviceCovMaxPeriodLengthForCycleBelowCap(t *testing.T) {
	// cycle=20: maxPeriodLength = 20-10 = 10, not > 14 → must return 10
	got := MaxPeriodLengthForCycle(20)
	if got != 10 {
		t.Fatalf("MaxPeriodLengthForCycle(20): got %d, want 10", got)
	}
}

func TestOnboardingserviceCovMaxPeriodLengthForCycleAtMaxCycle(t *testing.T) {
	// cycle=90: maxPeriodLength = 90-10 = 80 > 14 → must return 14
	got := MaxPeriodLengthForCycle(90)
	if got != 14 {
		t.Fatalf("MaxPeriodLengthForCycle(90): got %d, want 14", got)
	}
}

// ---------------------------------------------------------------------------
// Line 142: IsCompatibleCycleAndPeriod boundary
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovIsCompatibleCycleAndPeriodBoundaries(t *testing.T) {
	tests := []struct {
		cycle  int
		period int
		want   bool
		desc   string
	}{
		// cycle=20, maxPeriod=10: period=10 is compatible
		{20, 10, true, "period equals max, compatible"},
		// cycle=20, maxPeriod=10: period=11 is NOT compatible
		{20, 11, false, "period one above max, incompatible"},
		// cycle=15, maxPeriod=5: period=5 is compatible
		{15, 5, true, "min cycle with max allowed period, compatible"},
		// cycle=15, maxPeriod=5: period=6 is NOT compatible
		{15, 6, false, "min cycle one above max, incompatible"},
		// cycle=90, maxPeriod=14: period=14 is compatible
		{90, 14, true, "max cycle with max period, compatible"},
		// cycle=90, maxPeriod=14: period=15 → clamped to 14, compatible
		{90, 15, true, "max cycle with out-of-range period clamped to 14, compatible"},
	}
	for _, tt := range tests {
		got := IsCompatibleCycleAndPeriod(tt.cycle, tt.period)
		if got != tt.want {
			t.Fatalf("IsCompatibleCycleAndPeriod(%d,%d) [%s]: got %v, want %v",
				tt.cycle, tt.period, tt.desc, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Line 146: ClampOnboardingCycleLength — lower boundary at 15
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovClampCycleLengthLowerBoundary(t *testing.T) {
	tests := []struct {
		input int
		want  int
		desc  string
	}{
		{14, 15, "one below min clamped to 15"},
		{15, 15, "exactly min returned as-is"},
		{16, 16, "one above min returned as-is"},
		{0, 15, "zero clamped to 15"},
		{-1, 15, "negative clamped to 15"},
	}
	for _, tt := range tests {
		got := ClampOnboardingCycleLength(tt.input)
		if got != tt.want {
			t.Fatalf("ClampOnboardingCycleLength(%d) [%s]: got %d, want %d",
				tt.input, tt.desc, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Line 149: ClampOnboardingCycleLength — upper boundary at 90
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovClampCycleLengthUpperBoundary(t *testing.T) {
	tests := []struct {
		input int
		want  int
		desc  string
	}{
		{89, 89, "one below max returned as-is"},
		{90, 90, "exactly max returned as-is"},
		{91, 90, "one above max clamped to 90"},
		{200, 90, "far above max clamped to 90"},
	}
	for _, tt := range tests {
		got := ClampOnboardingCycleLength(tt.input)
		if got != tt.want {
			t.Fatalf("ClampOnboardingCycleLength(%d) [%s]: got %d, want %d",
				tt.input, tt.desc, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Line 156: ClampOnboardingPeriodLength — lower boundary at 1
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovClampPeriodLengthLowerBoundary(t *testing.T) {
	tests := []struct {
		input int
		want  int
		desc  string
	}{
		{0, 1, "zero clamped to 1"},
		{1, 1, "exactly min returned as-is"},
		{2, 2, "one above min returned as-is"},
		{-5, 1, "negative clamped to 1"},
	}
	for _, tt := range tests {
		got := ClampOnboardingPeriodLength(tt.input)
		if got != tt.want {
			t.Fatalf("ClampOnboardingPeriodLength(%d) [%s]: got %d, want %d",
				tt.input, tt.desc, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Line 159: ClampOnboardingPeriodLength — upper boundary at 14
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovClampPeriodLengthUpperBoundary(t *testing.T) {
	tests := []struct {
		input int
		want  int
		desc  string
	}{
		{13, 13, "one below max returned as-is"},
		{14, 14, "exactly max returned as-is"},
		{15, 14, "one above max clamped to 14"},
		{100, 14, "far above max clamped to 14"},
	}
	for _, tt := range tests {
		got := ClampOnboardingPeriodLength(tt.input)
		if got != tt.want {
			t.Fatalf("ClampOnboardingPeriodLength(%d) [%s]: got %d, want %d",
				tt.input, tt.desc, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Line 166: IsValidOnboardingCycleLength — boundary semantics
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovIsValidCycleLengthBoundaries(t *testing.T) {
	tests := []struct {
		value int
		want  bool
		desc  string
	}{
		{14, false, "one below min is invalid"},
		{15, true, "exactly min is valid"},
		{90, true, "exactly max is valid"},
		{91, false, "one above max is invalid"},
		{0, false, "zero is invalid"},
		{28, true, "typical cycle is valid"},
	}
	for _, tt := range tests {
		got := IsValidOnboardingCycleLength(tt.value)
		if got != tt.want {
			t.Fatalf("IsValidOnboardingCycleLength(%d) [%s]: got %v, want %v",
				tt.value, tt.desc, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Line 170: IsValidOnboardingPeriodLength — boundary semantics
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovIsValidPeriodLengthBoundaries(t *testing.T) {
	tests := []struct {
		value int
		want  bool
		desc  string
	}{
		{0, false, "zero is invalid"},
		{1, true, "exactly min is valid"},
		{14, true, "exactly max is valid"},
		{15, false, "one above max is invalid"},
		{-1, false, "negative is invalid"},
		{5, true, "typical period is valid"},
	}
	for _, tt := range tests {
		got := IsValidOnboardingPeriodLength(tt.value)
		if got != tt.want {
			t.Fatalf("IsValidOnboardingPeriodLength(%d) [%s]: got %v, want %v",
				tt.value, tt.desc, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Sanity: validate the SaveStep2 happy path passes sanitized values to repo
// and returns them to caller (regression guard for line 80 error path context)
// ---------------------------------------------------------------------------

func TestOnboardingserviceCovSaveStep2SanitizesBeforePersist(t *testing.T) {
	repo := &onboardingserviceCovStep2Repo{}
	svc := NewOnboardingService(repo)

	// cycle=10 (below min) → 15; period=20 (above max) → clamped to 14, then
	// MaxPeriodLengthForCycle(15)=5 → further capped to 5.
	gotCycle, gotPeriod, err := svc.SaveStep2(context.Background(), 7, 10, 20, true, false, "", "")
	if err != nil {
		t.Fatalf("SaveStep2 unexpected error: %v", err)
	}
	if gotCycle != 15 {
		t.Fatalf("SaveStep2 cycle: got %d, want 15", gotCycle)
	}
	if gotPeriod != 5 {
		t.Fatalf("SaveStep2 period: got %d, want 5", gotPeriod)
	}
	if !repo.saveStep2Called {
		t.Fatal("expected SaveOnboardingStep2 to be called")
	}
	if repo.savedCycle != 15 || repo.savedPeriod != 5 {
		t.Fatalf("repo received cycle=%d period=%d, want 15/5", repo.savedCycle, repo.savedPeriod)
	}
}

// Verify the zero-time guard: CompleteOnboardingForUser with a repo error
// propagates it and does not call CompleteOnboarding.
func TestOnboardingserviceCovCompleteOnboardingForUserPropagatesRepoFindError(t *testing.T) {
	sentinel := errors.New("connection refused")
	repo := &stubOnboardingRepo{findErr: sentinel}
	svc := NewOnboardingService(repo)

	_, err := svc.CompleteOnboardingForUser(context.Background(), 1, time.UTC)
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel error from FindByID, got %v", err)
	}
	if repo.completeCalled {
		t.Fatal("CompleteOnboarding must not be called when FindByID fails")
	}
}
