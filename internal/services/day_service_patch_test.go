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
// this one instead of merging onto the same old row. A full write keeps its
// plain read.
func TestPatchDayEntryMergesOntoTheLockingRead(t *testing.T) {
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{})
	day := time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC)
	seedPatchCycleStartDay(t, logs, day)

	if _, err := service.PatchDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{Mood: 3}, DayEntryFields{Mood: true}, time.UTC); err != nil {
		t.Fatalf("PatchDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if logs.lockingReads != 1 {
		t.Fatalf("expected the partial write to merge onto one locking read, got %d", logs.lockingReads)
	}

	if _, err := service.UpsertDayEntryWithAutoFill(context.Background(), 10, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowLight}, time.UTC); err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if logs.lockingReads != 1 {
		t.Fatalf("expected the full write to keep its plain read, got %d locking reads", logs.lockingReads)
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
	if logs.creates != 1 || logs.lockingReads != 2 {
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
