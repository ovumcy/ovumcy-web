package services

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type dayLogRepositoryStub struct {
	entries        map[string]models.DailyLog
	nextID         uint
	findErrByDay   map[string]error
	createErrByDay map[string]error
	saveErrByDay   map[string]error
	// saveErrFromCall makes saveErrByDay fire only from the N-th Save call
	// for that day (1-based), so a test can pass an earlier upsert and fail
	// a later flag write on the same calendar day.
	saveErrFromCall map[string]int
	saveCalls       map[string]int
	// listCalls counts ListByUser calls so a test can assert a guard returned
	// before reading anything, rather than only that it did not panic.
	listCalls int
	// listErr fails every ListByUser, which the day write only reaches when it
	// applies a confirmed cycle start.
	listErr error
}

func newDayLogRepositoryStub() *dayLogRepositoryStub {
	return &dayLogRepositoryStub{
		entries:         make(map[string]models.DailyLog),
		nextID:          1,
		findErrByDay:    make(map[string]error),
		createErrByDay:  make(map[string]error),
		saveErrByDay:    make(map[string]error),
		saveErrFromCall: make(map[string]int),
		saveCalls:       make(map[string]int),
	}
}

func (stub *dayLogRepositoryStub) dayKey(value time.Time) string {
	return value.Format("2006-01-02")
}

func (stub *dayLogRepositoryStub) ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	stub.listCalls++
	if stub.listErr != nil {
		return nil, stub.listErr
	}
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

func (stub *dayLogRepositoryStub) ListByUserRange(ctx context.Context, userID uint, fromStart *time.Time, toEnd *time.Time) ([]models.DailyLog, error) {
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

func (stub *dayLogRepositoryStub) ListByUserDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) ([]models.DailyLog, error) {
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

func (stub *dayLogRepositoryStub) FindByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
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

func (stub *dayLogRepositoryStub) Create(ctx context.Context, entry *models.DailyLog) error {
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

func (stub *dayLogRepositoryStub) CreateBatch(ctx context.Context, entries []models.DailyLog) error {
	for index := range entries {
		if err := stub.Create(ctx, &entries[index]); err != nil {
			return err
		}
	}
	return nil
}

func (stub *dayLogRepositoryStub) Save(ctx context.Context, entry *models.DailyLog) error {
	key := stub.dayKey(entry.Date)
	stub.saveCalls[key]++
	if err, ok := stub.saveErrByDay[key]; ok {
		if from, gated := stub.saveErrFromCall[key]; !gated || stub.saveCalls[key] >= from {
			return err
		}
	}
	stub.entries[key] = *entry
	return nil
}

func (stub *dayLogRepositoryStub) DeleteByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) error {
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
	// loadErrFromCall makes loadErr fire only from the N-th LoadSettingsByID
	// call (1-based), so a test can pass the autofill settings read and fail a
	// later read in the same write — the counterpart of saveErrFromCall above.
	loadErrFromCall int
	loadCalls       int
	// userIDs records the owner id of every LoadSettingsByID and UpdateByID
	// call in arrival order. Discarding it left owner scoping unfalsifiable at
	// this layer: any mutation of the id argument passed the whole day suite,
	// so a settings write aimed at another account read as a clean run.
	userIDs []uint
}

// assertUserRepositoryCallsTargetOwner fails unless the stub saw at least one
// call and every one of them carried want. The "at least one" half matters as
// much as the comparison: a seam that was never reached is unobservable, not
// proven, and a loop over an empty slice would report success about nothing.
func (stub *dayUserRepositoryStub) assertUserRepositoryCallsTargetOwner(t *testing.T, want uint) {
	t.Helper()
	if len(stub.userIDs) == 0 {
		t.Fatalf("expected at least one user-repository call for owner %d, saw none", want)
	}
	for index, got := range stub.userIDs {
		if got != want {
			t.Fatalf("user-repository call %d targeted owner %d, want the acting owner %d", index+1, got, want)
		}
	}
}

func (stub *dayUserRepositoryStub) LoadSettingsByID(_ context.Context, userID uint) (models.User, error) {
	stub.userIDs = append(stub.userIDs, userID)
	stub.loadCalls++
	if stub.loadErr != nil && stub.loadCalls >= stub.loadErrFromCall {
		return models.User{}, stub.loadErr
	}
	return stub.settings, nil
}

func (stub *dayUserRepositoryStub) UpdateByID(ctx context.Context, userID uint, updates map[string]any) error {
	stub.userIDs = append(stub.userIDs, userID)
	if updates == nil {
		return nil
	}
	if value, exists := updates["luteal_phase"]; exists {
		if lutealPhase, ok := value.(int); ok {
			stub.settings.LutealPhase = lutealPhase
		}
	}
	if value, exists := updates["long_period_warning_cycle_start"]; exists {
		if warnedAt, ok := value.(time.Time); ok {
			copyValue := warnedAt
			stub.settings.LongPeriodWarnedAt = &copyValue
		}
	}
	return nil
}

func TestUpsertDayEntryWithAutoFillNormalizesNonPeriodInput(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	entry, err := service.UpsertDayEntryWithAutoFill(context.Background(),
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

// seedInlineCycleStartAnchor prepares the state the day form's inline question
// is asked in: one explicit cycle start 28 days back, nothing since.
func seedInlineCycleStartAnchor(t *testing.T) (*DayService, *dayLogRepositoryStub, time.Time, time.Time) {
	t.Helper()

	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{settings: models.User{PeriodLength: 4}}
	service := NewDayService(logs, users)

	anchor := time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)
	logs.entries["2026-02-01"] = models.DailyLog{
		ID: 1, UserID: 10, Date: anchor, IsPeriod: true, CycleStart: true,
	}
	day := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	return service, logs, day, day
}

// A yes answered inline is carried by the day save itself, so the same request
// that records the bleeding records the cycle start — no second hunt for the
// manual control.
func TestUpsertDayEntryAppliesTheConfirmedInlineCycleStart(t *testing.T) {
	service, logs, day, now := seedInlineCycleStartAnchor(t)

	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, ConfirmCycleStart: true}, now, time.UTC)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt() unexpected error: %v", err)
	}
	if !entry.CycleStart {
		t.Fatal("expected the confirmed inline answer to mark the saved day as a cycle start")
	}
	if !logs.entries["2026-03-01"].CycleStart {
		t.Fatal("expected the persisted row to carry the cycle start, not only the returned entry")
	}
}

// The question is asked, never assumed: an untouched control leaves the day a
// plain period day, which is also what declining sends.
func TestUpsertDayEntryWithoutTheInlineAnswerMarksNoCycleStart(t *testing.T) {
	service, logs, day, now := seedInlineCycleStartAnchor(t)

	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}, now, time.UTC)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt() unexpected error: %v", err)
	}
	if entry.CycleStart || logs.entries["2026-03-01"].CycleStart {
		t.Fatal("expected an unanswered inline question to write no cycle start")
	}
}

// The answer is re-checked against the policy that raised the question, so a
// yes on a day the form would never have asked about marks nothing: here the
// previous start is four days back, which is the manual control's short-gap
// confirmation flow, not a one-tap question.
func TestUpsertDayEntryIgnoresAnInlineAnswerThePolicyWouldNotAskFor(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{settings: models.User{PeriodLength: 4}}
	service := NewDayService(logs, users)
	logs.entries["2026-02-25"] = models.DailyLog{
		ID: 1, UserID: 10, Date: time.Date(2026, time.February, 25, 0, 0, 0, 0, time.UTC),
		IsPeriod: true, CycleStart: true,
	}

	day := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, ConfirmCycleStart: true}, day, time.UTC)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt() unexpected error: %v", err)
	}
	if entry.CycleStart || logs.entries["2026-03-01"].CycleStart {
		t.Fatal("expected an answer outside the question's own policy state to mark nothing")
	}
	if !logs.entries["2026-02-25"].CycleStart {
		t.Fatal("expected the existing cycle start to survive an ignored inline answer")
	}
}

// A day saved as a non-period day carries no cycle start, however the answer
// arrived — normalization drops it before the write.
func TestUpsertDayEntryDropsTheInlineAnswerOnANonPeriodDay(t *testing.T) {
	service, logs, day, now := seedInlineCycleStartAnchor(t)

	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: false, Flow: models.FlowNone, ConfirmCycleStart: true}, now, time.UTC)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt() unexpected error: %v", err)
	}
	if entry.CycleStart || logs.entries["2026-03-01"].CycleStart {
		t.Fatal("expected a non-period save to mark no cycle start")
	}
}

// A day that already is a cycle start is left alone: the answer changes
// nothing, so the write stops before it reads anything.
func TestUpsertDayEntryLeavesAnExistingCycleStartUntouched(t *testing.T) {
	service, logs, day, now := seedInlineCycleStartAnchor(t)
	logs.entries["2026-03-01"] = models.DailyLog{
		ID: 2, UserID: 10, Date: day, IsPeriod: true, Flow: models.FlowMedium, CycleStart: true,
	}
	// A failing history read is the probe: the same setup on a day that is not
	// yet a cycle start reports ErrDayEntryLoadFailed (the table below), so a
	// clean save here can only mean the confirmation stopped before reading.
	logs.listErr = errors.New("history unavailable")

	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, ConfirmCycleStart: true}, now, time.UTC)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt() unexpected error: %v", err)
	}
	if !entry.CycleStart {
		t.Fatal("expected the existing cycle start to survive the save")
	}
}

func TestUpsertDayEntryReportsAFailedCycleStartConfirmation(t *testing.T) {
	loadFailure := errors.New("settings unavailable")
	saveFailure := errors.New("write rejected")

	testCases := []struct {
		name      string
		arrange   func(logs *dayLogRepositoryStub, users *dayUserRepositoryStub)
		expected  error
		seedEntry bool
	}{
		{
			name: "the log history cannot be read",
			arrange: func(logs *dayLogRepositoryStub, _ *dayUserRepositoryStub) {
				logs.listErr = errors.New("history unavailable")
			},
			expected: ErrDayEntryLoadFailed,
		},
		{
			name: "the owner settings cannot be read",
			arrange: func(_ *dayLogRepositoryStub, users *dayUserRepositoryStub) {
				users.loadErr = loadFailure
				// The first read is the autofill settings inside the same write.
				users.loadErrFromCall = 2
			},
			expected: ErrDayEntryLoadFailed,
		},
		{
			name: "the cycle-start flag cannot be written",
			arrange: func(logs *dayLogRepositoryStub, _ *dayUserRepositoryStub) {
				logs.saveErrByDay["2026-03-01"] = saveFailure
				// The first save is the day entry itself.
				logs.saveErrFromCall["2026-03-01"] = 2
			},
			expected:  ErrDayEntryUpdateFailed,
			seedEntry: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, logs, day, now := seedInlineCycleStartAnchor(t)
			users := &dayUserRepositoryStub{settings: models.User{PeriodLength: 4}}
			service := NewDayService(logs, users)
			if testCase.seedEntry {
				logs.entries["2026-03-01"] = models.DailyLog{
					ID: 2, UserID: 10, Date: day, IsPeriod: true, Flow: models.FlowLight,
				}
			}
			testCase.arrange(logs, users)

			_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day,
				DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, ConfirmCycleStart: true}, now, time.UTC)
			if !errors.Is(err, testCase.expected) {
				t.Fatalf("expected %v when %s, got %v", testCase.expected, testCase.name, err)
			}
			if logs.entries["2026-03-01"].CycleStart {
				t.Fatal("expected no cycle start to survive a failed confirmation")
			}
		})
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
	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(),
		10,
		day,
		DayEntryInput{
			IsPeriod: true,
			Flow:     models.FlowLight,
			Notes:    "period",
		},
		time.Date(2026, time.February, 12, 8, 0, 0, 0, time.UTC),
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

func TestUpsertDayEntryWithAutoFillStopsAtToday(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{
		settings: models.User{
			PeriodLength:   4,
			AutoPeriodFill: true,
		},
	}
	service := NewDayService(logs, users)

	day := time.Date(2026, time.February, 10, 8, 0, 0, 0, time.UTC)
	_, err := service.UpsertDayEntryWithAutoFillAt(context.Background(),
		10,
		day,
		DayEntryInput{
			IsPeriod: true,
			Flow:     models.FlowLight,
		},
		time.Date(2026, time.February, 11, 8, 0, 0, 0, time.UTC),
		time.UTC,
	)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt() unexpected error: %v", err)
	}

	if _, ok := logs.entries["2026-02-10"]; !ok {
		t.Fatalf("expected start day to be persisted")
	}
	if _, ok := logs.entries["2026-02-11"]; !ok {
		t.Fatalf("expected autofill to include today's follow-up day")
	}
	if _, ok := logs.entries["2026-02-12"]; ok {
		t.Fatalf("did not expect autofill to create future day 2026-02-12")
	}
	if _, ok := logs.entries["2026-02-13"]; ok {
		t.Fatalf("did not expect autofill to create future day 2026-02-13")
	}
}

func TestUpsertDayEntryWithAutoFillReturnsTypedLoadError(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{loadErr: errors.New("load settings failed")}
	service := NewDayService(logs, users)

	_, err := service.UpsertDayEntryWithAutoFill(context.Background(),
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

	_, err := service.UpsertDayEntryWithAutoFill(context.Background(),
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

	_, err := service.UpsertDayEntryWithAutoFill(context.Background(),
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

	entry, err := service.UpsertDayEntryWithAutoFill(context.Background(),
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

	if err := service.MarkCycleStartManually(context.Background(), 10, targetDay, targetDay, time.UTC, ManualCycleStartOptions{ReplaceExisting: true}); err != nil {
		t.Fatalf("MarkCycleStartManually() unexpected error: %v", err)
	}

	if logs.entries["2026-02-13"].CycleStart {
		t.Fatalf("expected previous explicit cycle start to be cleared")
	}
	if !logs.entries["2026-02-08"].CycleStart {
		t.Fatalf("expected selected day to become the explicit cycle start")
	}
}

func TestMarkCycleStartManuallyRequiresReplaceConfirmationWithinCluster(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	logs.entries["2026-02-13"] = models.DailyLog{
		ID:         1,
		UserID:     10,
		Date:       time.Date(2026, time.February, 13, 0, 0, 0, 0, time.UTC),
		IsPeriod:   true,
		CycleStart: true,
	}
	logs.entries["2026-02-08"] = models.DailyLog{
		ID:       2,
		UserID:   10,
		Date:     time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC),
		IsPeriod: true,
	}

	err := service.MarkCycleStartManually(context.Background(), 10, time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC), time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC), time.UTC, ManualCycleStartOptions{})
	if !errors.Is(err, ErrManualCycleStartReplaceRequired) {
		t.Fatalf("expected ErrManualCycleStartReplaceRequired, got %v", err)
	}
}

func TestMarkCycleStartManuallyRequiresShortGapConfirmationAndMarksUncertain(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{
		settings: models.User{
			LastPeriodStart: ptrTime(time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC)),
		},
	}
	service := NewDayService(logs, users)

	targetDay := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	logs.entries["2026-02-10"] = models.DailyLog{
		ID:       1,
		UserID:   10,
		Date:     targetDay,
		IsPeriod: true,
	}

	err := service.MarkCycleStartManually(context.Background(), 10, targetDay, targetDay, time.UTC, ManualCycleStartOptions{})
	if !errors.Is(err, ErrManualCycleStartConfirmationNeeded) {
		t.Fatalf("expected ErrManualCycleStartConfirmationNeeded, got %v", err)
	}

	if err := service.MarkCycleStartManually(context.Background(), 10, targetDay, targetDay, time.UTC, ManualCycleStartOptions{MarkUncertain: true}); err != nil {
		t.Fatalf("expected short-gap cycle start to save with confirmation, got %v", err)
	}
	if !logs.entries["2026-02-10"].IsUncertain {
		t.Fatalf("expected confirmed short-gap cycle start to be marked uncertain")
	}
}

func TestMarkCycleStartManuallyRejectsFarFutureDate(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	now := time.Date(2026, time.March, 12, 10, 0, 0, 0, time.UTC)
	futureDay := time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC)
	if err := service.MarkCycleStartManually(context.Background(), 10, futureDay, now, time.UTC, ManualCycleStartOptions{}); !errors.Is(err, ErrManualCycleStartDateInvalid) {
		t.Fatalf("expected ErrManualCycleStartDateInvalid, got %v", err)
	}
}

func TestMarkCycleStartManuallyAllowsFutureDateWithinTwoDays(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	now := time.Date(2026, time.March, 12, 10, 0, 0, 0, time.UTC)
	dayAfterTomorrow := time.Date(2026, time.March, 14, 0, 0, 0, 0, time.UTC)
	if err := service.MarkCycleStartManually(context.Background(), 10, dayAfterTomorrow, now, time.UTC, ManualCycleStartOptions{}); err != nil {
		t.Fatalf("expected future cycle start within two days to be allowed, got %v", err)
	}

	entry, ok := logs.entries["2026-03-14"]
	if !ok {
		t.Fatal("expected future entry to be created")
	}
	if !entry.IsPeriod || !entry.CycleStart {
		t.Fatalf("expected tomorrow entry to be period+cycle_start, got %#v", entry)
	}
}

// TestMarkCycleStartManuallyWrapsPersistenceFailure pins the typed-error
// contract for the manual cycle-start write path: repository failures while
// persisting the cycle-start flags surface as ErrManualCycleStartFailed
// (errors.Is-matchable for the API error mapping), not as a raw repo error.
func TestMarkCycleStartManuallyWrapsPersistenceFailure(t *testing.T) {
	logs := newDayLogRepositoryStub()
	users := &dayUserRepositoryStub{}
	service := NewDayService(logs, users)

	targetDay := time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC)
	logs.entries["2026-02-08"] = models.DailyLog{
		ID:       2,
		UserID:   10,
		Date:     targetDay,
		IsPeriod: true,
		Flow:     models.FlowLight,
	}
	logs.saveErrByDay["2026-02-08"] = errors.New("disk full")
	// Let the day upsert (first Save) succeed and fail the cycle-start flag
	// write (second Save), so the error surfaces from the persist stage.
	logs.saveErrFromCall["2026-02-08"] = 2

	err := service.MarkCycleStartManually(context.Background(), 10, targetDay, targetDay, time.UTC, ManualCycleStartOptions{})
	if !errors.Is(err, ErrManualCycleStartFailed) {
		t.Fatalf("expected ErrManualCycleStartFailed wrap, got %v", err)
	}
	if logs.entries["2026-02-08"].CycleStart {
		t.Fatalf("failed persistence must not leave the cycle-start flag set in the read model")
	}
}

// TestDayServiceUserRepositoryCallsTargetTheActingOwner is the owner-scope
// guard for every seam where DayService reaches the user repository: the
// autofill settings read, the inline cycle-start confirmation, the manual
// cycle-start policy read, the derived-settings refresh and both
// acknowledgements. The privacy boundary scopes a per-user read or write to the
// acting account and to no other (docs/SECURITY_INVARIANTS.md), and until the
// stub recorded the id nothing at this layer could tell a correctly scoped call
// from one aimed at a different row: replacing every id with a literal 1 left
// the whole day suite — and the API suite — green.
func TestDayServiceUserRepositoryCallsTargetTheActingOwner(t *testing.T) {
	const actingOwner uint = 10

	testCases := []struct {
		name    string
		arrange func(logs *dayLogRepositoryStub)
		act     func(t *testing.T, service *DayService)
	}{
		{
			name: "the day save reads the autofill settings, confirms the cycle start and refreshes the derived ones",
			arrange: func(logs *dayLogRepositoryStub) {
				logs.entries["2026-02-01"] = models.DailyLog{
					ID: 1, UserID: actingOwner,
					Date:     time.Date(2026, time.February, 1, 0, 0, 0, 0, time.UTC),
					IsPeriod: true, CycleStart: true,
				}
			},
			act: func(t *testing.T, service *DayService) {
				t.Helper()
				day := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
				if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), actingOwner, day,
					DayEntryInput{IsPeriod: true, Flow: models.FlowMedium, ConfirmCycleStart: true}, day, time.UTC); err != nil {
					t.Fatalf("UpsertDayEntryWithAutoFillAt() unexpected error: %v", err)
				}
			},
		},
		{
			name: "the manual cycle start reads the policy settings and refreshes the derived ones",
			arrange: func(logs *dayLogRepositoryStub) {
				logs.entries["2026-02-08"] = models.DailyLog{
					ID: 2, UserID: actingOwner,
					Date:     time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC),
					IsPeriod: true, Flow: models.FlowLight,
				}
			},
			act: func(t *testing.T, service *DayService) {
				t.Helper()
				targetDay := time.Date(2026, time.February, 8, 0, 0, 0, 0, time.UTC)
				if err := service.MarkCycleStartManually(context.Background(), actingOwner, targetDay, targetDay, time.UTC, ManualCycleStartOptions{}); err != nil {
					t.Fatalf("MarkCycleStartManually() unexpected error: %v", err)
				}
			},
		},
		{
			name:    "the period tip acknowledgement",
			arrange: func(*dayLogRepositoryStub) {},
			act: func(t *testing.T, service *DayService) {
				t.Helper()
				if err := service.AcknowledgePeriodTip(context.Background(), actingOwner); err != nil {
					t.Fatalf("AcknowledgePeriodTip() unexpected error: %v", err)
				}
			},
		},
		{
			name:    "the long-period warning acknowledgement",
			arrange: func(*dayLogRepositoryStub) {},
			act: func(t *testing.T, service *DayService) {
				t.Helper()
				cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
				if err := service.AcknowledgeLongPeriodWarning(context.Background(), actingOwner, cycleStart, time.UTC); err != nil {
					t.Fatalf("AcknowledgeLongPeriodWarning() unexpected error: %v", err)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			logs := newDayLogRepositoryStub()
			users := &dayUserRepositoryStub{settings: models.User{PeriodLength: 4}}
			service := NewDayService(logs, users)
			testCase.arrange(logs)

			testCase.act(t, service)

			users.assertUserRepositoryCallsTargetOwner(t, actingOwner)
		})
	}
}
