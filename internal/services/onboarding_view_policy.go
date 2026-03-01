package services

import (
	"strconv"
	"strings"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type OnboardingViewState struct {
	Step            int
	MinDate         time.Time
	MaxDate         time.Time
	LastPeriodStart *time.Time
	CycleLength     int
	PeriodLength    int
	AutoPeriodFill  bool
}

func ResolveOnboardingStep(raw string) int {
	step, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	if step < 0 {
		return 0
	}
	if step > 3 {
		return 3
	}
	return step
}

func BuildOnboardingViewState(user *models.User, stepRaw string, now time.Time, location *time.Location) OnboardingViewState {
	minDate, maxDate := OnboardingDateBounds(now, location)

	cycleLength := models.DefaultCycleLength
	periodLength := models.DefaultPeriodLength
	autoPeriodFill := false
	var lastPeriodStart *time.Time

	if user != nil {
		cycleLength, periodLength = ResolveCycleAndPeriodDefaults(user.CycleLength, user.PeriodLength)
		autoPeriodFill = user.AutoPeriodFill
		if user.LastPeriodStart != nil {
			day := DateAtLocation(*user.LastPeriodStart, location)
			lastPeriodStart = &day
		}
	}

	return OnboardingViewState{
		Step:            ResolveOnboardingStep(stepRaw),
		MinDate:         minDate,
		MaxDate:         maxDate,
		LastPeriodStart: lastPeriodStart,
		CycleLength:     cycleLength,
		PeriodLength:    periodLength,
		AutoPeriodFill:  autoPeriodFill,
	}
}
