package services

import (
	"errors"
	"strings"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const MaxDayNotesLength = 2000

const (
	MinDayMood = 1
	MaxDayMood = 5
)

var (
	ErrInvalidDayFlow          = errors.New("invalid day flow")
	ErrInvalidDayMood          = errors.New("invalid day mood")
	ErrInvalidDaySexActivity   = errors.New("invalid day sex activity")
	ErrInvalidDayBBT           = errors.New("invalid day bbt")
	ErrInvalidDayCervicalMucus = errors.New("invalid day cervical mucus")
	ErrInvalidDayPregnancyTest = errors.New("invalid day pregnancy test")
	ErrInvalidDayLHTest        = errors.New("invalid day lh test")
	ErrInvalidDayCycleFactors  = errors.New("invalid day cycle factors")
)

func NormalizeDayEntryInput(input DayEntryInput) (DayEntryInput, error) {
	if !IsValidDayFlow(input.Flow) {
		return input, ErrInvalidDayFlow
	}
	if !IsValidDayMood(input.Mood) {
		return input, ErrInvalidDayMood
	}
	if !IsValidDaySexActivity(input.SexActivity) {
		return input, ErrInvalidDaySexActivity
	}
	if !IsValidDayBBT(input.BBT) {
		return input, ErrInvalidDayBBT
	}
	if !IsValidDayCervicalMucus(input.CervicalMucus) {
		return input, ErrInvalidDayCervicalMucus
	}
	if !IsValidDayPregnancyTest(input.PregnancyTest) {
		return input, ErrInvalidDayPregnancyTest
	}
	if !IsValidDayLHTest(input.LHTest) {
		return input, ErrInvalidDayLHTest
	}
	normalizedCycleFactors, allCycleFactorsValid := NormalizeDayCycleFactorKeys(input.CycleFactorKeys)
	if !allCycleFactorsValid {
		return input, ErrInvalidDayCycleFactors
	}
	if !input.IsPeriod {
		input.Flow = models.FlowNone
	}
	input.Flow = NormalizeDayFlow(input.Flow)
	input.SexActivity = NormalizeDaySexActivity(input.SexActivity)
	input.CervicalMucus = NormalizeDayCervicalMucus(input.CervicalMucus)
	input.PregnancyTest = NormalizeDayPregnancyTest(input.PregnancyTest)
	input.LHTest = NormalizeDayLHTest(input.LHTest)
	input.CycleFactorKeys = normalizedCycleFactors
	input.BBT = normalizeStoredDayBBT(input.BBT)
	input.Notes = TrimDayNotes(input.Notes)
	return input, nil
}

func NormalizeDayFlow(flow string) string {
	switch strings.ToLower(strings.TrimSpace(flow)) {
	case models.FlowSpotting:
		return models.FlowSpotting
	case models.FlowLight:
		return models.FlowLight
	case models.FlowMedium:
		return models.FlowMedium
	case models.FlowHeavy:
		return models.FlowHeavy
	default:
		return models.FlowNone
	}
}

func IsValidDayFlow(flow string) bool {
	switch flow {
	case models.FlowNone, models.FlowSpotting, models.FlowLight, models.FlowMedium, models.FlowHeavy:
		return true
	default:
		return false
	}
}

func IsValidDayMood(value int) bool {
	return value == 0 || (value >= MinDayMood && value <= MaxDayMood)
}

func TrimDayNotes(value string) string {
	if len(value) <= MaxDayNotesLength {
		return value
	}
	return value[:MaxDayNotesLength]
}
