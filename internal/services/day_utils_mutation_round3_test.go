package services

import (
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// --- day_utils.go:99 BOUNDARY — DayHasData mood lower bound ---

func TestMR3Day_DayHasData_MoodAtMinIsData(t *testing.T) {
	// Mood == MinDayMood is real logged data. The boundary mutant turns
	// `>= MinDayMood` into `> MinDayMood`, which would drop a min-mood-only
	// entry to "no data".
	entry := models.DailyLog{Mood: MinDayMood, Flow: models.FlowNone}
	if !DayHasData(entry) {
		t.Fatalf("DayHasData(Mood=MinDayMood=%d) = false, want true", MinDayMood)
	}
}

// --- day_utils.go:141 BOUNDARY — IsAutoFilledPeriodCandidate mood upper bound ---

func TestMR3Day_IsAutoFilledPeriodCandidate_MoodAtMaxBlocksClearing(t *testing.T) {
	// A bare auto-filled period day becomes a NON-candidate the moment it
	// carries any manual mood signal. Mood == MaxDayMood is the inclusive upper
	// bound: such a day must NOT be treated as an auto-fill candidate (it would
	// be wrongly cleared otherwise). The boundary mutant turns `<= MaxDayMood`
	// into `< MaxDayMood`, which would make a max-mood period day look bare.
	entry := models.DailyLog{IsPeriod: true, Mood: MaxDayMood, Flow: models.FlowNone}
	if IsAutoFilledPeriodCandidate(entry) {
		t.Fatalf("IsAutoFilledPeriodCandidate(period day, Mood=MaxDayMood=%d) = true, want false", MaxDayMood)
	}
}
