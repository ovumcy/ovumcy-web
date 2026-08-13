package services

import (
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrSettingsCycleLengthOutOfRange    = errors.New("settings cycle length out of range")
	ErrSettingsPeriodLengthOutOfRange   = errors.New("settings period length out of range")
	ErrSettingsPeriodLengthIncompatible = errors.New("settings period length incompatible with cycle length")
	ErrSettingsCycleStartDateInvalid    = errors.New("settings cycle start date invalid")
)

type CycleSettingsValidationInput struct {
	CycleLength        int
	PeriodLength       int
	AutoPeriodFill     bool
	IrregularCycle     bool
	UnpredictableCycle bool
	AgeGroup           string
	UsageGoal          string
	LastPeriodStartRaw string
	LastPeriodStartSet bool
}

func (service *SettingsService) ValidateCycleSettings(input CycleSettingsValidationInput, now time.Time, location *time.Location) (CycleSettingsUpdate, error) {
	if !IsValidOnboardingCycleLength(input.CycleLength) {
		return CycleSettingsUpdate{}, ErrSettingsCycleLengthOutOfRange
	}
	if !IsValidOnboardingPeriodLength(input.PeriodLength) {
		return CycleSettingsUpdate{}, ErrSettingsPeriodLengthOutOfRange
	}

	if !IsCompatibleCycleAndPeriod(input.CycleLength, input.PeriodLength) {
		return CycleSettingsUpdate{}, ErrSettingsPeriodLengthIncompatible
	}

	update := CycleSettingsUpdate{
		CycleLength:        input.CycleLength,
		PeriodLength:       input.PeriodLength,
		AutoPeriodFill:     input.AutoPeriodFill,
		IrregularCycle:     input.IrregularCycle,
		UnpredictableCycle: input.UnpredictableCycle,
		AgeGroup:           NormalizeAgeGroup(input.AgeGroup),
		UsageGoal:          NormalizeUsageGoal(input.UsageGoal),
		LastPeriodStartSet: input.LastPeriodStartSet,
	}

	if !input.LastPeriodStartSet {
		return update, nil
	}

	rawDate := strings.TrimSpace(input.LastPeriodStartRaw)
	if rawDate == "" {
		update.LastPeriodStart = nil
		return update, nil
	}

	if location == nil {
		location = time.UTC
	}
	parsedDay, err := ParseDayDate(rawDate, location)
	if err != nil {
		return CycleSettingsUpdate{}, ErrSettingsCycleStartDateInvalid
	}

	minCycleStart, today := SettingsCycleStartDateBounds(now, location)
	if parsedDay.Before(minCycleStart) || parsedDay.After(today) {
		return CycleSettingsUpdate{}, ErrSettingsCycleStartDateInvalid
	}

	// Stored date-only field: canonicalize to UTC midnight of the same
	// calendar day (see day_utils.go on the two shapes).
	canonical := CalendarDay(parsedDay, time.UTC)
	update.LastPeriodStart = &canonical
	return update, nil
}

func (service *SettingsService) ApplyCycleSettings(user *models.User, update CycleSettingsUpdate) {
	if user == nil {
		return
	}

	user.CycleLength = update.CycleLength
	user.PeriodLength = update.PeriodLength
	user.AutoPeriodFill = update.AutoPeriodFill
	user.IrregularCycle = update.IrregularCycle
	user.UnpredictableCycle = update.UnpredictableCycle
	user.AgeGroup = NormalizeAgeGroup(update.AgeGroup)
	user.UsageGoal = NormalizeUsageGoal(update.UsageGoal)

	if !update.LastPeriodStartSet {
		return
	}
	if update.LastPeriodStart == nil {
		user.LastPeriodStart = nil
		return
	}

	day := *update.LastPeriodStart
	user.LastPeriodStart = &day
}

// ApplyUsageGoal mirrors a goal-only save onto the in-request user so the rest
// of the response is framed by the mode just chosen, without touching any other
// cycle field (the partial counterpart of ApplyCycleSettings).
func (service *SettingsService) ApplyUsageGoal(user *models.User, usageGoal string) {
	if user == nil {
		return
	}
	user.UsageGoal = NormalizeUsageGoal(usageGoal)
}

// AlternativeUsageGoals lists the modes the owner is not currently in, in a
// stable order, so a quick-switch surface can offer every other mode without
// re-offering the one already in force. The order matches the chooser on every
// other surface: the neutral default first, the two alternative modes after it.
func AlternativeUsageGoals(current string) []string {
	normalized := NormalizeUsageGoal(current)
	alternatives := make([]string, 0, 2)
	for _, goal := range []string{models.UsageGoalHealth, models.UsageGoalAvoid, models.UsageGoalTrying} {
		if goal != normalized {
			alternatives = append(alternatives, goal)
		}
	}
	return alternatives
}

func SettingsCycleStartDateBounds(now time.Time, location *time.Location) (time.Time, time.Time) {
	if location == nil {
		location = time.UTC
	}
	today := DateAtLocation(now.In(location), location)
	minDate := time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, location)
	return minDate, today
}
