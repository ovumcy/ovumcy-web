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

// TestPatchDayEntryMergesOntoTheLockingRead pins which read the merge depends
// on: the locking one, so a concurrent partial write of the same day waits for
// this one instead of merging onto the same old row. The write's own update
// reads the day through the lock as well (the row is already held by then),
// and a full write, which writes back the stored columns it does not state,
// reads through it too.
func TestPatchDayEntryMergesOntoTheLockingRead(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	if _, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{Mood: 3}, DayEntryFields{Mood: true}, time.UTC); err != nil {
		t.Fatalf("PatchDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if logs.lockingReads != 2 {
		t.Fatalf("expected the partial write's merge and its update to read through the lock, got %d locking reads", logs.lockingReads)
	}

	if _, err := service.UpsertDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowLight}, time.UTC); err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if logs.lockingReads != 3 {
		t.Fatalf("expected the full write to read the day it updates through the lock, got %d locking reads in all", logs.lockingReads)
	}
}

// dayUniqueRefusal is what the repository's Create returns when the unique
// (user_id, date) index refuses the insert.
type dayUniqueRefusal struct{}

func (dayUniqueRefusal) Error() string            { return "unique constraint violation" }
func (dayUniqueRefusal) UniqueConstraint() string { return "daily_logs(user_id, date)" }

// racingCreateDayLogStub plays a concurrent first write of the same day: its
// first Create stores the competitor's row and then refuses this insert, as the
// unique index does when the competitor commits first. refuseAlways keeps
// refusing every insert.
type racingCreateDayLogStub struct {
	*dayLogRepositoryStub
	competitor   *models.DailyLog
	refuseAlways bool
	creates      int
}

func (stub *racingCreateDayLogStub) Create(ctx context.Context, entry *models.DailyLog) error {
	stub.creates++
	if stub.competitor != nil {
		row := *stub.competitor
		stub.competitor = nil
		if err := stub.dayLogRepositoryStub.Create(ctx, &row); err != nil {
			return err
		}
		return dayUniqueRefusal{}
	}
	if stub.refuseAlways {
		return dayUniqueRefusal{}
	}
	return stub.dayLogRepositoryStub.Create(ctx, entry)
}

func TestPatchDayEntryRetriesOntoTheDayAConcurrentWriteCreated(t *testing.T) {
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	logs := &racingCreateDayLogStub{
		dayLogRepositoryStub: newDayLogRepositoryStub(),
		competitor:           &models.DailyLog{UserID: 10, Date: day, Mood: 4},
	}
	service := NewDayService(logs, &dayUserRepositoryStub{})

	saved, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{Notes: "mine"}, DayEntryFields{Notes: true}, time.UTC)
	if err != nil {
		t.Fatalf("expected the losing first write to retry onto the winner's row, got %v", err)
	}
	if saved.Mood != 4 || saved.Notes != "mine" {
		t.Fatalf("expected the winner's mood kept and this write's notes added, got mood=%d notes=%q", saved.Mood, saved.Notes)
	}
	stored := logs.entries[logs.dayKey(day)]
	if stored.Mood != 4 || stored.Notes != "mine" {
		t.Fatalf("expected the stored day to hold both writes, got mood=%d notes=%q", stored.Mood, stored.Notes)
	}
	// Each of the two transactions reads the day through the lock twice: the
	// merge, then the write's own update-or-insert decision.
	if logs.creates != 1 || logs.lockingReads != 4 {
		t.Fatalf("expected one refused insert, then one re-read that updates, got creates=%d locking reads=%d", logs.creates, logs.lockingReads)
	}
}

func TestPatchDayEntryRetriesARefusedInsertOnlyOnce(t *testing.T) {
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	logs := &racingCreateDayLogStub{dayLogRepositoryStub: newDayLogRepositoryStub(), refuseAlways: true}
	service := NewDayService(logs, &dayUserRepositoryStub{})

	_, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{Notes: "mine"}, DayEntryFields{Notes: true}, time.UTC)
	if !errors.Is(err, ErrDayEntryCreateFailed) {
		t.Fatalf("expected a second refusal to answer as ErrDayEntryCreateFailed, got %v", err)
	}
	if logs.creates != 2 {
		t.Fatalf("expected exactly one retry, got %d inserts", logs.creates)
	}
}

func TestUpsertDayEntryDoesNotRetryARefusedInsert(t *testing.T) {
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	logs := &racingCreateDayLogStub{dayLogRepositoryStub: newDayLogRepositoryStub(), refuseAlways: true}
	service := NewDayService(logs, &dayUserRepositoryStub{})

	_, err := service.UpsertDayEntryWithAutoFill(context.Background(), 10, day, DayEntryInput{Flow: models.FlowNone, Notes: "mine"}, time.UTC)
	if !errors.Is(err, ErrDayEntryCreateFailed) {
		t.Fatalf("expected a refused full-write insert to stay ErrDayEntryCreateFailed, got %v", err)
	}
	if logs.creates != 1 {
		t.Fatalf("expected the full write not to retry, got %d inserts", logs.creates)
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

// The auto-fill clear behind unchecking a period start runs on the partial
// write too, and it reads the anchor's flow from the stored row, not from the
// merged day: a hand-logged flow on a following day survives, a stored legacy
// spelling of the propagated flow still reads as the fill's own value, and a
// fill whose anchor was edited after it is kept.
func TestPatchUncheckingAnAutoFilledAnchorAppliesTheClearFlowRules(t *testing.T) {
	anchor := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	now := anchor.AddDate(0, 0, 14) // every fill day is in the past
	type patchStep struct {
		day    time.Time
		input  DayEntryInput
		fields DayEntryFields
	}
	for _, tc := range []struct {
		name        string
		anchorFlow  string
		afterFill   func(logs *dayLogRepositoryStub)
		steps       []patchStep
		wantCleared []string
		wantKept    map[string]string
	}{
		{
			name:       "hand-logged flow on a following day",
			anchorFlow: models.FlowMedium,
			steps: []patchStep{
				{day: anchor.AddDate(0, 0, 2), input: DayEntryInput{Flow: models.FlowHeavy}, fields: DayEntryFields{Flow: true}},
			},
			wantCleared: []string{"2026-09-08"},
			wantKept:    map[string]string{"2026-09-09": models.FlowHeavy, "2026-09-10": models.FlowMedium, "2026-09-11": models.FlowMedium},
		},
		{
			name:       "anchor flow stored in a legacy spelling",
			anchorFlow: models.FlowMedium,
			afterFill: func(logs *dayLogRepositoryStub) {
				stored := logs.entries["2026-09-07"]
				stored.Flow = " MEDIUM "
				logs.entries["2026-09-07"] = stored
			},
			wantCleared: []string{"2026-09-08", "2026-09-09", "2026-09-10", "2026-09-11"},
		},
		{
			name:       "anchor flow edited after the fill",
			anchorFlow: models.FlowLight,
			steps: []patchStep{
				{day: anchor, input: DayEntryInput{Flow: models.FlowHeavy}, fields: DayEntryFields{Flow: true}},
			},
			wantKept: map[string]string{"2026-09-08": models.FlowLight, "2026-09-09": models.FlowLight, "2026-09-10": models.FlowLight, "2026-09-11": models.FlowLight},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := newDayLogRepositoryStub()
			service := NewDayService(logs, &dayUserRepositoryStub{settings: models.User{PeriodLength: 5, AutoPeriodFill: true}})
			ctx := context.Background()

			if _, err := service.PatchDayEntryWithAutoFillAt(ctx, 10, anchor,
				DayEntryInput{IsPeriod: true, Flow: tc.anchorFlow}, DayEntryFields{IsPeriod: true, Flow: true}, now, time.UTC); err != nil {
				t.Fatalf("log the anchor: %v", err)
			}
			for _, key := range []string{"2026-09-08", "2026-09-09", "2026-09-10", "2026-09-11"} {
				if !logs.entries[key].IsPeriod {
					t.Fatalf("precondition: %s should be an auto-filled period day", key)
				}
			}
			if tc.afterFill != nil {
				tc.afterFill(logs)
			}
			for _, step := range tc.steps {
				if _, err := service.PatchDayEntryWithAutoFillAt(ctx, 10, step.day, step.input, step.fields, now, time.UTC); err != nil {
					t.Fatalf("patch %s: %v", logs.dayKey(step.day), err)
				}
			}
			if _, err := service.PatchDayEntryWithAutoFillAt(ctx, 10, anchor,
				DayEntryInput{IsPeriod: false}, DayEntryFields{IsPeriod: true}, now, time.UTC); err != nil {
				t.Fatalf("uncheck the anchor: %v", err)
			}

			if logs.entries["2026-09-07"].IsPeriod {
				t.Fatal("the unchecked anchor must be off")
			}
			for _, key := range tc.wantCleared {
				if entry := logs.entries[key]; entry.IsPeriod || entry.Flow != models.FlowNone {
					t.Fatalf("expected %s cleared, got IsPeriod=%t Flow=%q", key, entry.IsPeriod, entry.Flow)
				}
			}
			for key, flow := range tc.wantKept {
				if entry := logs.entries[key]; !entry.IsPeriod || entry.Flow != flow {
					t.Fatalf("expected %s kept as a %q period day, got IsPeriod=%t Flow=%q", key, flow, entry.IsPeriod, entry.Flow)
				}
			}
		})
	}
}
