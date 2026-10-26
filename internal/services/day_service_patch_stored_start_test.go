package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// A day without a row on the stored onboarding start renders its period ticked
// (withOnboardingStartTicked). A partial write reads that day the same way: a
// save that leaves is_period out keeps the tick the owner saw, a base of
// is_period=true is current rather than a change made elsewhere, and a stated
// is_period=false un-ticks the start.

func storedStartPatchFixture(day time.Time) (*startClearingDayLogs, *DayService) {
	logs := &startClearingDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub()}
	users := &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &day}}
	return logs, NewDayService(logs, users)
}

func TestPatchDayEntryKeepsTheStoredStartTickOnARowlessDay(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs, service := storedStartPatchFixture(day)

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true},
		DayEntryBase{Values: DayEntryInput{IsPeriod: true}, Fields: DayEntryFields{Mood: true}}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected a mood save on the row-less stored start to save, got %v", err)
	}
	if !saved.IsPeriod || saved.Mood != 4 {
		t.Fatalf("expected the rendered period tick kept beside the mood, got is_period=%v mood=%d", saved.IsPeriod, saved.Mood)
	}
	if len(logs.clearedDays) != 0 {
		t.Fatalf("a save that leaves is_period out must keep the stored start, got clears %v", logs.clearedDays)
	}
}

func TestPatchDayEntryUnTicksARowlessStoredStartAgainstItsRenderedBase(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs, service := storedStartPatchFixture(day)
	fields := DayEntryFields{IsPeriod: true}

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{IsPeriod: false}, fields,
		DayEntryBase{Values: DayEntryInput{IsPeriod: true}, Fields: fields}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected the un-tick based on the rendered tick to save, got %v", err)
	}
	if saved.IsPeriod {
		t.Fatal("expected the un-tick written")
	}
	if len(logs.clearedDays) != 1 || !logs.clearedDays[0].Equal(day) {
		t.Fatalf("expected the un-tick to withdraw the start on %s, got clears %v", day, logs.clearedDays)
	}
}

func TestPatchDayEntryLeavesARowlessDayOffTheStoredStartUnticked(t *testing.T) {
	start := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	day := start.AddDate(0, 0, 1)
	_, service := storedStartPatchFixture(start)

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true}, DayEntryBase{}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected the mood save to save, got %v", err)
	}
	if saved.IsPeriod {
		t.Fatal("a row-less day other than the stored start must not gain a period")
	}
}

// Without the stored start the row-less day cannot be read as the forms render
// it, so the write is refused rather than merged onto a guess.
func TestPatchDayEntryRefusesARowlessDayWhenTheStoredStartCannotBeRead(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := newDayLogRepositoryStub()
	service := NewDayService(logs, &dayUserRepositoryStub{loadErr: errors.New("driver: database is locked")})

	_, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true}, DayEntryBase{}, day.Add(12*time.Hour), time.UTC)
	if !errors.Is(err, ErrDayEntryLoadFailed) {
		t.Fatalf("expected ErrDayEntryLoadFailed, got %v", err)
	}
	if len(logs.entries) != 0 {
		t.Fatalf("a refused write must store nothing, got %v", logs.entries)
	}
}

// txStartDayLogs is a day-log stub whose transaction handle holds its own view
// of the stored start: txStart is what the write's transaction reads, while the
// settings projection beside it may still hold an older start.
type txStartDayLogs struct {
	*startClearingDayLogs
	txStart    *time.Time
	startReads int
	startErr   error
	// createdDuringStartRead is the row a concurrent write of the same day
	// commits while this write's locking read of the start waits for it.
	createdDuringStartRead *models.DailyLog
	// calls records the locking reads in order: "start" for the stored start,
	// "day" for a day row.
	calls []string
}

func (stub *txStartDayLogs) LoadLastPeriodStartForUpdate(ctx context.Context, _ uint) (*time.Time, error) {
	stub.startReads++
	stub.calls = append(stub.calls, "start")
	if stub.createdDuringStartRead != nil {
		if err := stub.Create(ctx, stub.createdDuringStartRead); err != nil {
			return nil, err
		}
		stub.createdDuringStartRead = nil
	}
	return stub.txStart, stub.startErr
}

func (stub *txStartDayLogs) FindByUserAndDayRangeForUpdate(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	stub.calls = append(stub.calls, "day")
	return stub.startClearingDayLogs.FindByUserAndDayRangeForUpdate(ctx, userID, dayStart, dayEnd)
}

func newTxStartDayLogs() *txStartDayLogs {
	return &txStartDayLogs{startClearingDayLogs: &startClearingDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub()}}
}

// The locking read of the start can wait for a concurrent write of the same
// row-less day; the row that write committed is the one this write merges
// onto, so neither write erases the other's field.
func TestPatchDayEntryMergesOntoTheRowCommittedWhileTheStartReadWaited(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := newTxStartDayLogs()
	logs.createdDuringStartRead = &models.DailyLog{UserID: 7, Date: day, Mood: 3}
	service := NewDayService(logs, &dayUserRepositoryStub{})

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Notes: "after"}, DayEntryFields{Notes: true}, DayEntryBase{}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected the notes save to save, got %v", err)
	}
	if saved.Mood != 3 || saved.Notes != "after" {
		t.Fatalf("expected both writes' fields kept (mood=3, notes=after), got mood=%d notes=%q", saved.Mood, saved.Notes)
	}
}

func TestPatchDayEntryRefusesWhenTheStartReadFails(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := newTxStartDayLogs()
	logs.startErr = errors.New("driver: bad connection")
	_, err := NewDayService(logs, &dayUserRepositoryStub{}).PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true}, DayEntryBase{}, day.Add(12*time.Hour), time.UTC)
	if !errors.Is(err, ErrDayEntryLoadFailed) {
		t.Fatalf("expected ErrDayEntryLoadFailed, got %v", err)
	}
}

// One lock order for every day write: the owner's stored-start lock first,
// then any day row. A write that took a day row first and the users row after
// would deadlock against one taking them the other way round.
func TestEveryDayWriteLocksTheStoredStartBeforeAnyDayRow(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	now := day.Add(12 * time.Hour)
	ctx := context.Background()
	writes := map[string]func(*DayService) error{
		"partial write": func(service *DayService) error {
			_, err := service.PatchDayEntryWithAutoFillAt(ctx, 7, day, DayEntryInput{Mood: 4}, DayEntryFields{Mood: true}, DayEntryBase{}, now, time.UTC)
			return err
		},
		"full write with a cycle-start answer": func(service *DayService) error {
			_, err := service.UpsertDayEntryWithAutoFillAt(ctx, 7, day, DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, ConfirmCycleStart: true}, now, time.UTC)
			return err
		},
		"manual cycle-start mark": func(service *DayService) error {
			return service.MarkCycleStartManually(ctx, 7, day, now, time.UTC, ManualCycleStartOptions{})
		},
	}
	for name, write := range writes {
		logs := newTxStartDayLogs()
		if err := write(NewDayService(logs, &dayUserRepositoryStub{})); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		starts := 0
		for _, call := range logs.calls {
			if call == "start" {
				starts++
			}
		}
		if len(logs.calls) < 2 || logs.calls[0] != "start" || starts != 1 {
			t.Fatalf("%s: expected the stored start locked once, before every day row, got %v", name, logs.calls)
		}
	}
}

// The manual mark's policy judges the stored start its transaction holds: a
// start withdrawn before the mark no longer makes the marked day a short
// cycle, though the settings projection beside the transaction still has it.
func TestManualCycleStartPolicyReadsTheStartTheTransactionHolds(t *testing.T) {
	day := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)
	stale := day.AddDate(0, 0, -10)
	ctx := context.Background()

	withdrawn := newTxStartDayLogs()
	service := NewDayService(withdrawn, &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &stale}})
	if err := service.MarkCycleStartManually(ctx, 7, day, day.Add(12*time.Hour), time.UTC, ManualCycleStartOptions{}); err != nil {
		t.Fatalf("expected the mark judged against the withdrawn start to save, got %v", err)
	}

	held := newTxStartDayLogs()
	held.txStart = &stale
	service = NewDayService(held, &dayUserRepositoryStub{})
	if err := service.MarkCycleStartManually(ctx, 7, day, day.Add(12*time.Hour), time.UTC, ManualCycleStartOptions{}); !errors.Is(err, ErrManualCycleStartConfirmationNeeded) {
		t.Fatalf("expected the start the transaction holds to make the mark a short cycle, got %v", err)
	}
}

// The inline cycle-start answer's policy judges the start the write's own
// transaction holds; a failed read of it refuses the answer's write.
func TestConfirmedCycleStartRefusesWhenTheTransactionsStartReadFails(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := newTxStartDayLogs()
	logs.startErr = errors.New("driver: bad connection")
	service := NewDayService(logs, &dayUserRepositoryStub{})

	_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, ConfirmCycleStart: true}, day.Add(12*time.Hour), time.UTC)
	if !errors.Is(err, ErrDayEntryLoadFailed) {
		t.Fatalf("expected ErrDayEntryLoadFailed, got %v", err)
	}
	if logs.startReads == 0 {
		t.Fatal("expected the policy's start read on the transaction handle")
	}
}

// A Settings withdrawal of the start committed before the day write is the
// start that write reads: the row-less day it merges onto is no longer the
// ticked start, so a mood save does not materialize a period day from a start
// the owner already withdrew. The settings projection here still holds the old
// start — the snapshot a read taken beside the transaction would see.
func TestPatchDayEntryReadsTheStoredStartOnTheWriteTransaction(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := &txStartDayLogs{startClearingDayLogs: &startClearingDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub()}}
	users := &dayUserRepositoryStub{settings: models.User{LastPeriodStart: &day}}
	service := NewDayService(logs, users)

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true}, DayEntryBase{}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected the mood save to save, got %v", err)
	}
	if saved.IsPeriod {
		t.Fatal("a period day was materialized from a stored start the write's transaction no longer holds")
	}
	if logs.startReads == 0 {
		t.Fatal("expected the stored start read on the write's transaction handle")
	}
}

// The positive control of the test above: the start the transaction holds
// still ticks the row-less day, so the read is the one the tick comes from.
func TestPatchDayEntryTicksTheStartTheWriteTransactionHolds(t *testing.T) {
	day := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	logs := &txStartDayLogs{startClearingDayLogs: &startClearingDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub()}, txStart: &day}
	service := NewDayService(logs, &dayUserRepositoryStub{})

	saved, err := service.PatchDayEntryWithAutoFillAt(context.Background(), 7, day,
		DayEntryInput{Mood: 4}, DayEntryFields{Mood: true}, DayEntryBase{}, day.Add(12*time.Hour), time.UTC)
	if err != nil {
		t.Fatalf("expected the mood save to save, got %v", err)
	}
	if !saved.IsPeriod {
		t.Fatal("expected the start the transaction holds to keep the row-less day ticked")
	}
}
