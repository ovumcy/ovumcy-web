package services

import (
	"strconv"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type OnboardingViewState struct {
	Step            int
	MinDate         time.Time
	MaxDate         time.Time
	LastPeriodStart *time.Time
	CycleLength     int
	PeriodLength    int
	AutoPeriodFill  bool
	IrregularCycle  bool
	AgeGroup        string
	UsageGoal       string
}

func ResolveOnboardingStep(raw string) int {
	step, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 1
	}
	if step < 1 {
		return 1
	}
	if step > 2 {
		return 2
	}
	return step
}

func BuildOnboardingViewState(user *models.User, stepRaw string, now time.Time, location *time.Location) OnboardingViewState {
	minDate, maxDate := OnboardingDateBounds(now, location)

	cycleLength := models.DefaultCycleLength
	periodLength := models.DefaultPeriodLength
	autoPeriodFill := false
	irregularCycle := false
	ageGroup := models.AgeGroupUnknown
	usageGoal := models.UsageGoalHealth
	var lastPeriodStart *time.Time

	if user != nil {
		cycleLength, periodLength = ResolveCycleAndPeriodDefaults(user.CycleLength, user.PeriodLength)
		autoPeriodFill = user.AutoPeriodFill
		irregularCycle = user.IrregularCycle
		ageGroup = NormalizeAgeGroup(user.AgeGroup)
		usageGoal = NormalizeUsageGoal(user.UsageGoal)
		if user.LastPeriodStart != nil {
			day := CalendarDay(*user.LastPeriodStart, location)
			lastPeriodStart = &day
		}
	}

	step := ResolveOnboardingStep(stepRaw)
	if strings.TrimSpace(stepRaw) == "" && lastPeriodStart != nil {
		step = 2
	}

	return OnboardingViewState{
		Step:            step,
		MinDate:         minDate,
		MaxDate:         maxDate,
		LastPeriodStart: lastPeriodStart,
		CycleLength:     cycleLength,
		PeriodLength:    periodLength,
		AutoPeriodFill:  autoPeriodFill,
		IrregularCycle:  irregularCycle,
		AgeGroup:        ageGroup,
		UsageGoal:       usageGoal,
	}
}
