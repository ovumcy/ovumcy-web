package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A partial day write changes exactly the fields it names. These pin the
// service half of PATCH /api/v1/days/{date}: the merge onto the stored row,
// the derived period rules applied to the merged day, and the read failure
// that must refuse the write rather than merge onto nothing.

func seedPatchCycleStartDay(t *testing.T, logs *dayLogRepositoryStub, day time.Time) {
	t.Helper()
	logs.entries[logs.dayKey(day)] = models.DailyLog{
		ID:              1,
		UserID:          10,
		Date:            day,
		IsPeriod:        true,
		CycleStart:      true,
		Flow:            models.FlowMedium,
		Mood:            2,
		SexActivity:     models.SexActivityProtected,
		BBT:             new(36.55),
		CervicalMucus:   models.CervicalMucusCreamy,
		PregnancyTest:   models.PregnancyTestNegative,
		CycleFactorKeys: []string{models.CycleFactorStress},
		SymptomIDs:      []uint{4},
		Notes:           "keep me",
	}
	logs.nextID = 2
}

func TestPatchDayEntryChangesOnlyTheNamedField(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	saved, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{Mood: 3}, DayEntryFields{Mood: true}, time.UTC)
	if err != nil {
		t.Fatalf("PatchDayEntryWithAutoFill() unexpected error: %v", err)
	}

	if saved.Mood != 3 {
		t.Fatalf("expected the named mood to change to 3, got %d", saved.Mood)
	}
	if !saved.IsPeriod || !saved.CycleStart || saved.Flow != models.FlowMedium {
		t.Fatalf("expected period, cycle start and flow kept, got is_period=%v cycle_start=%v flow=%q", saved.IsPeriod, saved.CycleStart, saved.Flow)
	}
	if saved.BBT == nil || *saved.BBT != 36.55 || saved.Notes != "keep me" {
		t.Fatalf("expected bbt and notes kept, got bbt=%v notes=%q", saved.BBT, saved.Notes)
	}
	if saved.SexActivity != models.SexActivityProtected || saved.CervicalMucus != models.CervicalMucusCreamy || saved.PregnancyTest != models.PregnancyTestNegative {
		t.Fatalf("expected owner fields kept, got sex=%q mucus=%q test=%q", saved.SexActivity, saved.CervicalMucus, saved.PregnancyTest)
	}
	if len(saved.CycleFactorKeys) != 1 || saved.CycleFactorKeys[0] != models.CycleFactorStress || len(saved.SymptomIDs) != 1 || saved.SymptomIDs[0] != 4 {
		t.Fatalf("expected cycle factors and symptoms kept, got %v / %v", saved.CycleFactorKeys, saved.SymptomIDs)
	}
}

func TestPatchDayEntryStatingNoPeriodClearsTheFieldsThatFollowFromIt(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	saved, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{IsPeriod: false}, DayEntryFields{IsPeriod: true}, time.UTC)
	if err != nil {
		t.Fatalf("PatchDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if saved.IsPeriod || saved.CycleStart || saved.Flow != models.FlowNone {
		t.Fatalf("expected is_period=false to clear cycle start and flow as a full write does, got is_period=%v cycle_start=%v flow=%q", saved.IsPeriod, saved.CycleStart, saved.Flow)
	}
	if saved.Mood != 2 || saved.Notes != "keep me" || saved.BBT == nil {
		t.Fatalf("expected the fields is_period does not govern kept, got mood=%d notes=%q bbt=%v", saved.Mood, saved.Notes, saved.BBT)
	}
}

func TestPatchDayEntryRefusesAnInvalidStatedValueAndKeepsTheDay(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	_, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{Mood: MaxDayMood + 1}, DayEntryFields{Mood: true}, nil)
	if !errors.Is(err, ErrInvalidDayMood) {
		t.Fatalf("expected ErrInvalidDayMood, got %v", err)
	}
	if stored := logs.entries[logs.dayKey(day)]; stored.Mood != 2 || !stored.CycleStart {
		t.Fatalf("expected the refused write to leave the day as stored, got mood=%d cycle_start=%v", stored.Mood, stored.CycleStart)
	}
}

func TestPatchDayEntryRefusesWhenTheStoredDayCannotBeRead(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	logs.findErrByDay[logs.dayKey(day)] = errors.New("read error")

	_, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{Mood: 3}, DayEntryFields{Mood: true}, time.UTC)
	if !errors.Is(err, ErrDayEntryLoadFailed) {
		t.Fatalf("expected ErrDayEntryLoadFailed, got %v", err)
	}
}

func TestMergeDayEntryPatchCarriesStoredValuesNormalized(t *testing.T) {
	stored := models.DailyLog{
		IsPeriod: true,
		Flow:     "HEAVY",
		Mood:     MaxDayMood + 4,
		BBT:      new(-1.0),
		Notes:    "kept",
	}
	merged := mergeDayEntryPatch(stored, DayEntryInput{PregnancyTest: models.PregnancyTestPositive}, DayEntryFields{PregnancyTest: true})

	if merged.PregnancyTest != models.PregnancyTestPositive {
		t.Fatalf("expected the stated pregnancy test, got %q", merged.PregnancyTest)
	}
	if merged.Flow != models.FlowHeavy || !merged.IsPeriod || merged.Notes != "kept" {
		t.Fatalf("expected stored period, flow and notes carried, got is_period=%v flow=%q notes=%q", merged.IsPeriod, merged.Flow, merged.Notes)
	}
	if merged.Mood != 0 || merged.BBT != nil {
		t.Fatalf("expected an unreadable stored mood and temperature carried as unset, got mood=%d bbt=%v", merged.Mood, merged.BBT)
	}
	if _, err := NormalizeDayEntryInput(merged); err != nil {
		t.Fatalf("expected a stored legacy value not to refuse a write that does not touch it, got %v", err)
	}
}
