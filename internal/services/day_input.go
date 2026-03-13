package services

import (
	"errors"

	"github.com/terraincognita07/ovumcy/internal/models"
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
	if !input.IsPeriod {
		input.Flow = models.FlowNone
	}
	input.SexActivity = NormalizeDaySexActivity(input.SexActivity)
	input.CervicalMucus = NormalizeDayCervicalMucus(input.CervicalMucus)
	input.BBT = normalizeStoredDayBBT(input.BBT)
	input.Notes = TrimDayNotes(input.Notes)
	return input, nil
}

func IsValidDayFlow(flow string) bool {
	switch flow {
	case models.FlowNone, models.FlowLight, models.FlowMedium, models.FlowHeavy:
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
