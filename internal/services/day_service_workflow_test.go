package services

import (
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type dayLogRepositoryStub struct {
	entries        map[string]models.DailyLog
	nextID         uint
	findErrByDay   map[string]error
	createErrByDay map[string]error
	saveErrByDay   map[string]error
	clearErr       error
}

func newDayLogRepositoryStub() *dayLogRepositoryStub {
	return &dayLogRepositoryStub{
		entries:        make(map[string]models.DailyLog),
		nextID:         1,
		findErrByDay:   make(map[string]error),
		createErrByDay: make(map[string]error),
		saveErrByDay:   make(map[string]error),
	}
}

func (stub *dayLogRepositoryStub) dayKey(value time.Time) string {
	return value.Format("2006-01-02")
}

func (stub *dayLogRepositoryStub) ListByUser(userID uint) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	for _, entry := range stub.entries {
		if entry.UserID == userID {
			logs = append(logs, entry)
		}
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].Date.Equal(logs[j].Date) {
			return logs[i].ID < logs[j].ID
		}
		return logs[i].Date.Before(logs[j].Date)
	})
	return logs, nil
}

func (stub *dayLogRepositoryStub) ListByUserRange(userID uint, fromStart *time.Time, toEnd *time.Time) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	for _, entry := range stub.entries {
		if entry.UserID != userID {
			continue
		}
		if fromStart != nil && entry.Date.Before(*fromStart) {
			continue
		}
		if toEnd != nil && !entry.Date.Before(*toEnd) {
			continue
		}
		logs = append(logs, entry)
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].Date.Equal(logs[j].Date) {
			return logs[i].ID < logs[j].ID
		}
		return logs[i].Date.Before(logs[j].Date)
	})
	return logs, nil
}

func (stub *dayLogRepositoryStub) ListByUserDayRange(userID uint, dayStart time.Time, dayEnd time.Time) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	for _, entry := range stub.entries {
		if entry.UserID != userID {
			continue
		}
		if entry.Date.Before(dayStart) || !entry.Date.Before(dayEnd) {
			continue
		}
		logs = append(logs, entry)
	}
	sort.Slice(logs, func(i, j int) bool {
		if logs[i].Date.Equal(logs[j].Date) {
			return logs[i].ID > logs[j].ID
		}
		return logs[i].Date.After(logs[j].Date)
	})
	return logs, nil
}

func (stub *dayLogRepositoryStub) FindByUserAndDayRange(userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	key := stub.dayKey(dayStart)
	if err, ok := stub.findErrByDay[key]; ok {
		return models.DailyLog{}, false, err
	}

	entry, ok := stub.entries[key]
	if !ok || entry.UserID != userID || entry.Date.Before(dayStart) || !entry.Date.Before(dayEnd) {
		return models.DailyLog{}, false, nil
	}
	return entry, true, nil
}

func (stub *dayLogRepositoryStub) Create(entry *models.DailyLog) error {
	key := stub.dayKey(entry.Date)
	if err, ok := stub.createErrByDay[key]; ok {
		return err
	}
	if entry.ID == 0 {
		entry.ID = stub.nextID
		stub.nextID++
	}
	stub.entries[key] = *entry
	return nil
}

func (stub *dayLogRepositoryStub) Save(entry *models.DailyLog) error {
	key := stub.dayKey(entry.Date)
	if err, ok := stub.saveErrByDay[key]; ok {
		return err
	}
	stub.entries[key] = *entry
	return nil
}

func (stub *dayLogRepositoryStub) ClearCycleStartsExcept(userID uint, dayStart time.Time, dayEnd time.Time) error {
	if stub.clearErr != nil {
		return stub.clearErr
	}
	for key, entry := range stub.entries {
		if entry.UserID != userID {
			continue
		}
		if entry.Date.Before(dayStart) || !entry.Date.Before(dayEnd) {
			entry.CycleStart = false
			stub.entries[key] = entry
		}
	}
	return nil
}

func (stub *dayLogRepositoryStub) DeleteByUserAndDayRange(userID uint, dayStart time.Time, dayEnd time.Time) error {
	for key, entry := range stub.entries {
		if entry.UserID != userID {
			continue
		}
		if entry.Date.Before(dayStart) || !entry.Date.Before(dayEnd) {
			continue
		}
		delete(stub.entries, key)
	}
	return nil
}

type dayUserRepositoryStub struct {
	settings models.User
	loadErr  error
}

func (stub *dayUserRepositoryStub) LoadSettingsByID(uint) (models.User, error) {
	if stub.loadErr != nil {
		return models.User{}, stub.loadErr
	}
	return stub.settings, nil
}

func TestUpsertDayEntryWithAutoFillNormalizesNonPeriodInput(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	entry, err := service.UpsertDayEntryWithAutoFill(
		10,
		time.Date(2026, time.February, 20, 12, 0, 0, 0, time.UTC),
		DayEntryInput{
			IsPeriod:   false,
			Flow:       models.FlowHeavy,
			SymptomIDs: []uint{5, 6},
			Notes:      strings.Repeat("x", MaxDayNotesLength+11),
		},
		time.UTC,
	)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if entry.Flow != models.FlowNone {
		t.Fatalf("expected non-period flow normalized to %q, got %q", models.FlowNone, entry.Flow)
	}
	if len(entry.SymptomIDs) != 2 || entry.SymptomIDs[0] != 5 || entry.SymptomIDs[1] != 6 {
		t.Fatalf("expected non-period symptom IDs to be preserved, got %#v", entry.SymptomIDs)
	}
	if len(entry.Notes) != MaxDayNotesLength {
		t.Fatalf("expected notes length %d, got %d", MaxDayNotesLength, len(entry.Notes))
	}
}

func TestUpsertDayEntryWithAutoFillCreatesFollowingPeriodDays(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{
		settings: models.User{
			PeriodLength:   3,
			AutoPeriodFill: true,
		},
	}
	service := NewDayService(logs, users)

	day := time.Date(2026, time.February, 10, 8, 0, 0, 0, time.UTC)
	entry, err := service.UpsertDayEntryWithAutoFill(
		10,
		day,
		DayEntryInput{
			IsPeriod: true,
			Flow:     models.FlowLight,
			Notes:    "period",
		},
		time.UTC,
	)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatalf("expected created entry to be period day")
	}

	expectedDays := []string{"2026-02-10", "2026-02-11", "2026-02-12"}
	for _, dayKey := range expectedDays {
		logEntry, ok := logs.entries[dayKey]
		if !ok {
			t.Fatalf("expected day %s to exist after autofill", dayKey)
		}
		if !logEntry.IsPeriod {
			t.Fatalf("expected day %s to be period", dayKey)
		}
	}

}

func TestUpsertDayEntryWithAutoFillReturnsTypedLoadError(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{loadErr: errors.New("load settings failed")}
	service := NewDayService(logs, users)

	_, err := service.UpsertDayEntryWithAutoFill(
		10,
		time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
		DayEntryInput{
			IsPeriod: true,
			Flow:     models.FlowLight,
		},
		time.UTC,
	)
	if !errors.Is(err, ErrDayAutoFillLoadFailed) {
		t.Fatalf("expected ErrDayAutoFillLoadFailed, got %v", err)
	}
}

func TestUpsertDayEntryWithAutoFillReturnsTypedAutofillDecisionError(t *testing.T) {
	logs := newDayLogRepositoryStub()
	logs.findErrByDay["2026-02-09"] = errors.New("previous day read failed")
	users := &dayUserRepositoryStub{
		settings: models.User{
			PeriodLength:   3,
			AutoPeriodFill: true,
		},
	}
	service := NewDayService(logs, users)

	_, err := service.UpsertDayEntryWithAutoFill(
		10,
		time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
		DayEntryInput{
			IsPeriod: true,
			Flow:     models.FlowLight,
		},
		time.UTC,
	)
	if !errors.Is(err, ErrDayAutoFillCheckFailed) {
		t.Fatalf("expected ErrDayAutoFillCheckFailed, got %v", err)
	}
}

func TestUpsertDayEntryWithAutoFillReturnsTypedAutofillApplyError(t *testing.T) {
	logs := newDayLogRepositoryStub()
	logs.createErrByDay["2026-02-11"] = errors.New("autofill create failed")
	users := &dayUserRepositoryStub{
		settings: models.User{
			PeriodLength:   3,
			AutoPeriodFill: true,
		},
	}
	service := NewDayService(logs, users)

	_, err := service.UpsertDayEntryWithAutoFill(
		10,
		time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC),
		DayEntryInput{
			IsPeriod: true,
			Flow:     models.FlowLight,
		},
		time.UTC,
	)
	if !errors.Is(err, ErrDayAutoFillApplyFailed) {
		t.Fatalf("expected ErrDayAutoFillApplyFailed, got %v", err)
	}
}

func TestUpsertDayEntryWithAutoFillClearsCycleStartWhenPeriodIsRemoved(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	existingDay := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	logs.entries["2026-02-10"] = models.DailyLog{
		ID:         1,
		UserID:     10,
		Date:       existingDay,
		IsPeriod:   true,
		CycleStart: true,
		Flow:       models.FlowHeavy,
	}

	entry, err := service.UpsertDayEntryWithAutoFill(
		10,
		existingDay,
		DayEntryInput{
			IsPeriod: false,
			Flow:     models.FlowNone,
		},
		time.UTC,
	)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFill() unexpected error: %v", err)
	}
	if entry.CycleStart {
		t.Fatalf("expected cycle_start to be cleared when period is removed")
	}
}

func TestMarkCycleStartManuallyClearsOtherExplicitStarts(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	previousDay := time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC)
	targetDay := time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC)
	logs.entries["2026-02-13"] = models.DailyLog{
		ID:         1,
		UserID:     10,
		Date:       previousDay,
		IsPeriod:   true,
		CycleStart: true,
	}
	logs.entries["2026-02-08"] = models.DailyLog{
		ID:       2,
		UserID:   10,
		Date:     targetDay,
		IsPeriod: true,
		Flow:     models.FlowLight,
	}

	if err := service.MarkCycleStartManually(10, targetDay, targetDay, time.UTC); err != nil {
		t.Fatalf("MarkCycleStartManually() unexpected error: %v", err)
	}

	if logs.entries["2026-02-13"].CycleStart {
		t.Fatalf("expected previous explicit cycle start to be cleared")
	}
	if !logs.entries["2026-02-08"].CycleStart {
		t.Fatalf("expected selected day to become the explicit cycle start")
	}
}

func TestMarkCycleStartManuallyRejectsFarFutureDate(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	now := time.Date(2026, time.March, 12, 10, 0, 0, 0, time.UTC)
	futureDay := time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC)
	if err := service.MarkCycleStartManually(10, futureDay, now, time.UTC); !errors.Is(err, ErrManualCycleStartDateInvalid) {
		t.Fatalf("expected ErrManualCycleStartDateInvalid, got %v", err)
	}
}

func TestMarkCycleStartManuallyAllowsTomorrowDate(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	now := time.Date(2026, time.March, 12, 10, 0, 0, 0, time.UTC)
	tomorrow := time.Date(2026, time.March, 13, 0, 0, 0, 0, time.UTC)
	if err := service.MarkCycleStartManually(10, tomorrow, now, time.UTC); err != nil {
		t.Fatalf("expected tomorrow cycle start to be allowed, got %v", err)
	}

	entry, ok := logs.entries["2026-03-13"]
	if !ok {
		t.Fatal("expected tomorrow entry to be created")
	}
	if !entry.IsPeriod || !entry.CycleStart {
		t.Fatalf("expected tomorrow entry to be period+cycle_start, got %#v", entry)
	}
}
