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
	ErrInvalidDayCycleFactors  = errors.New("invalid day cycle factors")
)

func NormalizeDayEntryInput(input DayEntryInput) (DayEntryInput, error) {
	input = dropPreservedDayEntryFields(input)
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
	normalizedCycleFactors, allCycleFactorsValid := NormalizeDayCycleFactorKeys(input.CycleFactorKeys)
	if !allCycleFactorsValid {
		return input, ErrInvalidDayCycleFactors
	}
	if !input.IsPeriod {
		input.Flow = models.FlowNone
		// A cycle start is the first day of bleeding, so the inline answer only
		// means anything on a day that is being saved as a period day.
		input.ConfirmCycleStart = false
	}
	input.Flow = NormalizeDayFlow(input.Flow)
	input.SexActivity = NormalizeDaySexActivity(input.SexActivity)
	input.CervicalMucus = NormalizeDayCervicalMucus(input.CervicalMucus)
	input.PregnancyTest = NormalizeDayPregnancyTest(input.PregnancyTest)
	input.CycleFactorKeys = normalizedCycleFactors
	input.BBT = normalizeStoredDayBBT(input.BBT)
	input.Notes = TrimDayNotes(input.Notes)
	return input, nil
}

// dropPreservedDayEntryFields neutralises every field this write marked
// Preserve*, before the validation above ever reads one. A preserved field's
// incoming value is not this write's subject — the save replaces it with the
// value already stored — so it may not reject the day, and merging against an
// EMPTY stored row is exactly that neutralisation: the same path an update
// takes with the real row, run against no row at all, so the drop and the merge
// cannot come to list different fields.
//
// Transport declines to read a hidden field at all (parseDayPayload), so no
// HTTP request delivers a value here any more; keeping the rule in the service
// is what makes it the service's own rather than a property of one transport.
// Even before that, only bbt and cycle factors could refuse a day this way —
// transport normalizes sex activity and cervical mucus to a valid spelling on
// the way in and merely trims notes — and all five are dropped so the rule does
// not have to be re-derived from what each transport happens to sanitize.
//
// On a day that does not exist yet there is nothing to merge, and this is then
// the whole answer: a hidden field starts neutral instead of storing a value
// the account can neither see nor correct.
func dropPreservedDayEntryFields(input DayEntryInput) DayEntryInput {
	return mergePreservedDayEntryInput(models.DailyLog{}, input)
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

// TrimDayNotes caps a note at MaxDayNotesLength CHARACTERS, the unit the notes
// textareas declare through maxlength. Measured in bytes the same number cut
// what the browser had already accepted, silently: 2000 typed Cyrillic
// characters are 4000 bytes and came back as roughly 1000 characters.
//
// HTML maxlength counts UTF-16 code units, so a character (rune) count is at
// worst MORE permissive than the browser — an astral character is one rune and
// two code units — and the server therefore never truncates a value the browser
// was willing to submit.
//
// Ranging over the string yields the byte offset of each character's first
// byte, so a single pass both counts and locates the cut: an over-limit value
// is sliced at the offset of the character after the cap — always a character
// boundary — and a value at or under the cap falls out of the loop and is
// returned whole.
func TrimDayNotes(value string) string {
	characters := 0
	for index := range value {
		if characters == MaxDayNotesLength {
			return value[:index]
		}
		characters++
	}
	return value
}
