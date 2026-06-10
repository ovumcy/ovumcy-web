package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestBuildOnboardingViewStateEmptyStepJumpsToStepTwoWhenLastPeriodKnown(t *testing.T) {
	location := time.UTC
	now := time.Date(2026, time.April, 15, 9, 0, 0, 0, location)
	rawLastPeriod := time.Date(2026, time.March, 11, 0, 0, 0, 0, time.UTC)
	user := &models.User{
		LastPeriodStart: &rawLastPeriod,
	}

	// No explicit step requested (empty query param). Because the user already
	// has a known last period start, the policy must skip step 1 and land on step 2.
	state := BuildOnboardingViewState(user, "", now, location)
	if state.Step != 2 {
		t.Fatalf("expected step 2 for empty stepRaw with known last period start, got %d", state.Step)
	}

	// Whitespace-only step must behave identically to empty (TrimSpace -> "").
	whitespaceState := BuildOnboardingViewState(user, "   ", now, location)
	if whitespaceState.Step != 2 {
		t.Fatalf("expected step 2 for whitespace stepRaw with known last period start, got %d", whitespaceState.Step)
	}

	// Guard the other diverging direction: an explicit "1" with a known last
	// period start must stay on step 1 (the auto-jump only applies to empty input).
	explicitState := BuildOnboardingViewState(user, "1", now, location)
	if explicitState.Step != 1 {
		t.Fatalf("expected step 1 for explicit stepRaw \"1\", got %d", explicitState.Step)
	}

	// And with no last period start, empty input must NOT jump (stays at default step 1).
	noPeriodState := BuildOnboardingViewState(&models.User{}, "", now, location)
	if noPeriodState.Step != 1 {
		t.Fatalf("expected step 1 for empty stepRaw without last period start, got %d", noPeriodState.Step)
	}
}
