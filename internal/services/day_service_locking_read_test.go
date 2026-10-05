package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// staleReadDayLogs models a read that ran before a concurrent write of the
// same day committed. Its plain reads (FindByUserAndDayRange, ListByUser)
// answer with the row as it was; the locking read answers with the row the
// concurrent write left, which is what SELECT … FOR UPDATE returns once it has
// waited for that write's commit. A row this write saves is its own from then
// on, so the stale copy is dropped.
type staleReadDayLogs struct {
	*dayLogRepositoryStub
	stale map[string]models.DailyLog
}

func (logs *staleReadDayLogs) FindByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	if entry, ok := logs.stale[logs.dayKey(dayStart)]; ok {
		return entry, true, nil
	}
	return logs.dayLogRepositoryStub.FindByUserAndDayRange(ctx, userID, dayStart, dayEnd)
}

func (logs *staleReadDayLogs) ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	entries, err := logs.dayLogRepositoryStub.ListByUser(ctx, userID)
	for index := range entries {
		if entry, ok := logs.stale[logs.dayKey(entries[index].Date)]; ok {
			entries[index] = entry
		}
	}
	return entries, err
}

func (logs *staleReadDayLogs) Save(ctx context.Context, entry *models.DailyLog) error {
	delete(logs.stale, logs.dayKey(entry.Date))
	return logs.dayLogRepositoryStub.Save(ctx, entry)
}

// newStaleReadDayLogs stores each current row and keeps stale as the copy a
// plain read still sees.
func newStaleReadDayLogs(current []models.DailyLog, stale []models.DailyLog) *staleReadDayLogs {
	logs := &staleReadDayLogs{dayLogRepositoryStub: newDayLogRepositoryStub(), stale: map[string]models.DailyLog{}}
	for _, entry := range current {
		logs.entries[logs.dayKey(entry.Date)] = entry
	}
	for _, entry := range stale {
		logs.stale[logs.dayKey(entry.Date)] = entry
	}
	return logs
}

func (logs *staleReadDayLogs) assertNotes(t *testing.T, day time.Time, want string) models.DailyLog {
	t.Helper()
	entry := logs.entries[logs.dayKey(day)]
	if entry.Notes != want {
		t.Fatalf("expected the concurrent write's notes %q kept on %s, got %q: the write merged onto a row read before that write committed", want, logs.dayKey(day), entry.Notes)
	}
	return entry
}

var lockingReadDay = time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC)

func TestFullDayWritePreservesTheHiddenFieldAConcurrentWriteLeft(t *testing.T) {
	logs := newStaleReadDayLogs(
		[]models.DailyLog{{ID: 1, UserID: 10, Date: lockingReadDay, Mood: 1, Notes: "concurrent"}},
		[]models.DailyLog{{ID: 1, UserID: 10, Date: lockingReadDay, Mood: 1, Notes: "old"}},
	)
	service := NewDayService(logs, &dayUserRepositoryStub{})

	if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, lockingReadDay,
		DayEntryInput{Flow: models.FlowNone, Mood: 3, PreserveNotes: true}, lockingReadDay, time.UTC); err != nil {
		t.Fatalf("full day write: %v", err)
	}
	if entry := logs.assertNotes(t, lockingReadDay, "concurrent"); entry.Mood != 3 {
		t.Fatalf("expected the written mood, got %d", entry.Mood)
	}
}

func TestManualCycleStartMarkKeepsTheFieldsAConcurrentWriteLeft(t *testing.T) {
	logs := newStaleReadDayLogs(
		[]models.DailyLog{{ID: 1, UserID: 10, Date: lockingReadDay, IsPeriod: true, Flow: models.FlowLight, Notes: "concurrent"}},
		[]models.DailyLog{{ID: 1, UserID: 10, Date: lockingReadDay, IsPeriod: true, Flow: models.FlowLight}},
	)
	service := NewDayService(logs, &dayUserRepositoryStub{})

	if err := service.MarkCycleStartManually(context.Background(), 10, lockingReadDay, lockingReadDay, time.UTC, ManualCycleStartOptions{}); err != nil {
		t.Fatalf("mark cycle start: %v", err)
	}
	if entry := logs.assertNotes(t, lockingReadDay, "concurrent"); !entry.CycleStart {
		t.Fatal("expected the day marked as a cycle start")
	}
}

func TestManualCycleStartMarkClearsACompetingStartWithoutRevertingIt(t *testing.T) {
	competingDay := time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC)
	logs := newStaleReadDayLogs(
		[]models.DailyLog{
			{ID: 1, UserID: 10, Date: competingDay, IsPeriod: true, CycleStart: true, Notes: "concurrent"},
			{ID: 2, UserID: 10, Date: lockingReadDay, IsPeriod: true, Flow: models.FlowLight},
		},
		[]models.DailyLog{{ID: 1, UserID: 10, Date: competingDay, IsPeriod: true, CycleStart: true}},
	)
	service := NewDayService(logs, &dayUserRepositoryStub{})

	if err := service.MarkCycleStartManually(context.Background(), 10, lockingReadDay, lockingReadDay, time.UTC, ManualCycleStartOptions{ReplaceExisting: true}); err != nil {
		t.Fatalf("mark cycle start: %v", err)
	}
	if entry := logs.assertNotes(t, competingDay, "concurrent"); entry.CycleStart {
		t.Fatal("expected the competing cycle start cleared")
	}
}

func TestManualCycleStartMarkSkipsACompetingStartAConcurrentWriteRemoved(t *testing.T) {
	competingDay := time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC)
	logs := newStaleReadDayLogs(
		[]models.DailyLog{
			{ID: 1, UserID: 10, Date: competingDay, IsPeriod: true, Notes: "concurrent"},
			{ID: 2, UserID: 10, Date: lockingReadDay, IsPeriod: true, Flow: models.FlowLight},
		},
		[]models.DailyLog{{ID: 1, UserID: 10, Date: competingDay, IsPeriod: true, CycleStart: true}},
	)
	service := NewDayService(logs, &dayUserRepositoryStub{})

	if err := service.MarkCycleStartManually(context.Background(), 10, lockingReadDay, lockingReadDay, time.UTC, ManualCycleStartOptions{ReplaceExisting: true}); err != nil {
		t.Fatalf("mark cycle start: %v", err)
	}
	if logs.saveCalls[logs.dayKey(competingDay)] != 0 {
		t.Fatal("expected no write to a day that is no longer a cycle start")
	}
	logs.assertNotes(t, competingDay, "concurrent")
}

func TestManualCycleStartMarkReportsAFailedLockingRead(t *testing.T) {
	competingDay := time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC)
	for name, failingDay := range map[string]time.Time{"marked day": lockingReadDay, "competing start": competingDay} {
		t.Run(name, func(t *testing.T) {
			logs := newStaleReadDayLogs([]models.DailyLog{
				{ID: 1, UserID: 10, Date: competingDay, IsPeriod: true, CycleStart: true},
				{ID: 2, UserID: 10, Date: lockingReadDay, IsPeriod: true, Flow: models.FlowLight},
			}, nil)
			failing := &failingLockingReadDayLogs{staleReadDayLogs: logs, day: logs.dayKey(failingDay)}
			service := NewDayService(failing, &dayUserRepositoryStub{})

			err := service.MarkCycleStartManually(context.Background(), 10, lockingReadDay, lockingReadDay, time.UTC, ManualCycleStartOptions{ReplaceExisting: true})
			if err == nil {
				t.Fatal("expected a failed locking read to fail the mark")
			}
			if !logs.entries[logs.dayKey(competingDay)].CycleStart {
				t.Fatal("expected the competing start left as stored when its locking read fails")
			}
		})
	}
}

// failingLockingReadDayLogs fails the locking read of one day only, so the
// policy's plain reads still see the stored history.
type failingLockingReadDayLogs struct {
	*staleReadDayLogs
	day string
}

func (logs *failingLockingReadDayLogs) FindByUserAndDayRangeForUpdate(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	if logs.dayKey(dayStart) == logs.day {
		return models.DailyLog{}, false, errLockingReadUnavailable
	}
	return logs.staleReadDayLogs.FindByUserAndDayRangeForUpdate(ctx, userID, dayStart, dayEnd)
}

var errLockingReadUnavailable = errors.New("locking read unavailable")

func TestPeriodAutofillLeavesADayAConcurrentWriteFilled(t *testing.T) {
	nextDay := lockingReadDay.AddDate(0, 0, 1)
	logs := newStaleReadDayLogs(
		[]models.DailyLog{{ID: 1, UserID: 10, Date: nextDay, Notes: "concurrent"}},
		[]models.DailyLog{{ID: 1, UserID: 10, Date: nextDay}},
	)
	service := NewDayService(logs, &dayUserRepositoryStub{})

	if err := service.AutoFillFollowingPeriodDays(context.Background(), 10, lockingReadDay, 3, models.FlowLight, lockingReadDay.AddDate(0, 0, 5), time.UTC); err != nil {
		t.Fatalf("autofill: %v", err)
	}
	if entry := logs.assertNotes(t, nextDay, "concurrent"); entry.IsPeriod {
		t.Fatal("expected a day holding the owner's own entry left out of the autofill")
	}
}

func TestAutofillClearingLeavesADayAConcurrentWriteFilled(t *testing.T) {
	nextDay := lockingReadDay.AddDate(0, 0, 1)
	logs := newStaleReadDayLogs(
		[]models.DailyLog{{ID: 1, UserID: 10, Date: nextDay, IsPeriod: true, Flow: models.FlowLight, Notes: "concurrent"}},
		[]models.DailyLog{{ID: 1, UserID: 10, Date: nextDay, IsPeriod: true, Flow: models.FlowLight}},
	)
	service := NewDayService(logs, &dayUserRepositoryStub{})

	if err := service.ClearAutoFilledPeriodNeighbors(context.Background(), 10, lockingReadDay, 3, models.FlowLight, time.UTC); err != nil {
		t.Fatalf("clear autofill: %v", err)
	}
	if entry := logs.assertNotes(t, nextDay, "concurrent"); !entry.IsPeriod {
		t.Fatal("expected a day holding the owner's own entry kept as a period day")
	}
}
