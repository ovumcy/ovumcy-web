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
	parsedDay, err := time.ParseInLocation("2006-01-02", rawDate, location)
	if err != nil {
		return CycleSettingsUpdate{}, ErrSettingsCycleStartDateInvalid
	}

	minCycleStart, today := SettingsCycleStartDateBounds(now, location)
	if parsedDay.Before(minCycleStart) || parsedDay.After(today) {
		return CycleSettingsUpdate{}, ErrSettingsCycleStartDateInvalid
	}

	canonical := time.Date(parsedDay.Year(), parsedDay.Month(), parsedDay.Day(), 0, 0, 0, 0, time.UTC)
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

func SettingsCycleStartDateBounds(now time.Time, location *time.Location) (time.Time, time.Time) {
	if location == nil {
		location = time.UTC
	}
	today := DateAtLocation(now.In(location), location)
	minDate := time.Date(today.Year(), time.January, 1, 0, 0, 0, 0, location)
	return minDate, today
}
