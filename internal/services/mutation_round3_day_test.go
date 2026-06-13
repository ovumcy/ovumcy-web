package services

import (
	"context"
	"errors"
	"sort"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// mr3dayLogStub is a self-contained DayLogRepository stub for the round-3 day
// mutation tests. It is intentionally local to this file (own type name) to
// avoid collisions with the shared dayLogRepositoryStub while giving these
// tests full control over write capture and error injection.
//
// Entries are keyed by the canonical UTC-midnight calendar date of the stored
// Date value. Writes emulate models.DailyLog.BeforeSave (re-anchor the value's
// own y/m/d to UTC-midnight), which is the on-disk convention DayRange queries
// against. This keeps the stub coherent under non-UTC request locations.
type mr3dayLogStub struct {
	entries map[string]models.DailyLog
	nextID  uint

	// clearedKeys records the UTC date keys whose IsPeriod flag was turned off
	// by a Save (auto-fill clearing), in call order.
	clearedKeys []string

	// failSaveWhenCycleStart, when set, makes Save return this error the moment
	// a cycle-start entry is persisted. Used to fail a flag write inside a tx.
	failSaveWhenCycleStart error
}

func newMr3dayLogStub() *mr3dayLogStub {
	return &mr3dayLogStub{
		entries: make(map[string]models.DailyLog),
		nextID:  1,
	}
}

// mr3canonicalDate mirrors models.DailyLog.BeforeSave: take the calendar
// components of value in its own location and re-anchor them to UTC-midnight.
func mr3canonicalDate(value time.Time) time.Time {
	if value.IsZero() {
		return value
	}
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func mr3dayKey(value time.Time) string {
	return mr3canonicalDate(value).Format("2006-01-02")
}

func (s *mr3dayLogStub) ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0, len(s.entries))
	for _, entry := range s.entries {
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

func (s *mr3dayLogStub) ListByUserRange(ctx context.Context, userID uint, fromStart *time.Time, toEnd *time.Time) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	for _, entry := range s.entries {
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
	return logs, nil
}

func (s *mr3dayLogStub) ListByUserDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	for _, entry := range s.entries {
		if entry.UserID != userID {
			continue
		}
		if entry.Date.Before(dayStart) || !entry.Date.Before(dayEnd) {
			continue
		}
		logs = append(logs, entry)
	}
	return logs, nil
}

func (s *mr3dayLogStub) FindByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	key := mr3dayKey(dayStart)
	entry, ok := s.entries[key]
	if !ok || entry.UserID != userID || entry.Date.Before(dayStart) || !entry.Date.Before(dayEnd) {
		return models.DailyLog{}, false, nil
	}
	return entry, true, nil
}

func (s *mr3dayLogStub) Create(ctx context.Context, entry *models.DailyLog) error {
	if entry.ID == 0 {
		entry.ID = s.nextID
		s.nextID++
	}
	entry.Date = mr3canonicalDate(entry.Date)
	s.entries[mr3dayKey(entry.Date)] = *entry
	return nil
}

func (s *mr3dayLogStub) Save(ctx context.Context, entry *models.DailyLog) error {
	if s.failSaveWhenCycleStart != nil && entry.CycleStart {
		return s.failSaveWhenCycleStart
	}
	entry.Date = mr3canonicalDate(entry.Date)
	key := mr3dayKey(entry.Date)
	prev, existed := s.entries[key]
	if existed && prev.IsPeriod && !entry.IsPeriod {
		s.clearedKeys = append(s.clearedKeys, key)
	}
	s.entries[key] = *entry
	return nil
}

func (s *mr3dayLogStub) DeleteByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) error {
	for key, entry := range s.entries {
		if entry.UserID != userID {
			continue
		}
		if entry.Date.Before(dayStart) || !entry.Date.Before(dayEnd) {
			continue
		}
		delete(s.entries, key)
	}
	return nil
}

// mr3dayUserStub captures the values passed to UpdateByID so tests can assert
// the canonicalized cycle-start day, and can inject load errors.
type mr3dayUserStub struct {
	settings    models.User
	loadErr     error
	lastUpdates map[string]any
}

func (s *mr3dayUserStub) LoadSettingsByID(context.Context, uint) (models.User, error) {
	if s.loadErr != nil {
		return models.User{}, s.loadErr
	}
	return s.settings, nil
}

func (s *mr3dayUserStub) UpdateByID(ctx context.Context, _ uint, updates map[string]any) error {
	s.lastUpdates = updates
	return nil
}

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

// --- day_service.go:366 NEGATION — ClearAutoFilledPeriodNeighbors location guard ---
//
// EQUIVALENT: the negation `if location == nil { location = time.UTC }` ->
// `if location != nil { ... }` overwrites a real zone with UTC, but inside the
// loop the only consumers of `location` are CalendarDay (rebuilds the offset
// day at location-midnight using the value's OWN calendar components — the
// location arg never changes which .Date() is taken) and DayRange (re-projects
// that value via .In(location)). Because the same location feeds both, the
// rebuilt midnight and the re-projection cancel: the looked-up calendar day is
// invariant under the UTC-vs-zone swap. Verified by mutating the source and
// running the full ./internal/services suite (all green). The test below is
// retained only as non-UTC coverage of the clearing walk (the shared
// clear-neighbors test exercises UTC only); it is NOT a mutation killer.

func TestMR3Day_ClearAutoFilledPeriodNeighbors_NonUTCCoverage(t *testing.T) {
	zone := time.FixedZone("UTC+9", 9*3600)
	logs := newMr3dayLogStub()
	users := &mr3dayUserStub{settings: models.User{PeriodLength: 3, AutoPeriodFill: true}}
	service := NewDayService(logs, users)

	seed := time.Date(2026, time.February, 10, 22, 0, 0, 0, time.UTC) // local 2026-02-11
	now := seed.AddDate(0, 0, 5)                                      // all fill days in the past
	if _, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, seed,
		DayEntryInput{IsPeriod: true, Flow: models.FlowLight}, now, zone); err != nil {
		t.Fatalf("seed auto-filled period: %v", err)
	}

	// Auto-fill seeds the anchor's local day (02-11) and the two following local
	// days (02-12, 02-13), each stored at UTC-midnight of that local calendar
	// day.
	for _, key := range []string{"2026-02-11", "2026-02-12", "2026-02-13"} {
		if !logs.entries[key].IsPeriod {
			t.Fatalf("precondition: %s should be an auto-filled period day (have keys %v)", key, mr3dayKeys(logs))
		}
	}

	startDay := CalendarDay(seed.In(zone), zone) // local 2026-02-11
	logs.clearedKeys = nil
	if err := service.ClearAutoFilledPeriodNeighbors(context.Background(), 10, startDay, 3, zone); err != nil {
		t.Fatalf("ClearAutoFilledPeriodNeighbors: %v", err)
	}

	// With the real UTC+9 zone honored, the cleared neighbors are the two local
	// days following the anchor: 02-12 and 02-13. Under the negation mutant the
	// location is overwritten with UTC, so DayRange resolves the neighbors to a
	// different UTC date window and the wrong (or no) keys are cleared.
	want := []string{"2026-02-12", "2026-02-13"}
	if !equalStringSetMR3(logs.clearedKeys, want) {
		t.Fatalf("cleared keys = %v, want %v", logs.clearedKeys, want)
	}
	if !logs.entries["2026-02-11"].IsPeriod {
		t.Fatal("the anchor day 2026-02-11 must not be cleared")
	}
}

// NOTE: day_feedback_policy.go:62 NEGATION (`if location != nil { cycleStart =
// CalendarDay(cycleStart, location) }`) is EQUIVALENT and intentionally has no
// test. CalendarDay does NOT apply In(location); it takes value.Date() (the
// value's own calendar components) and rebuilds them at location-midnight. The
// line that follows re-extracts y/m/d via cycleStart.Date() and re-anchors to
// UTC-midnight. Since the persisted value depends only on value.Date(), which
// CalendarDay preserves, skipping CalendarDay yields the identical persisted
// day for any input/location. The IsZero short-circuit on line 59 also removes
// CalendarDay's only other behavioral branch. No observable difference exists.

// --- day_service.go:279 NEGATION — UpsertDayEntryWithAutoFillAt tx-close returns populated entry ---

func TestMR3Day_UpsertDayEntryWithAutoFillAt_ReturnsPopulatedEntry(t *testing.T) {
	// On the happy path the transaction close `if err != nil { return ..., err }`
	// must fall through and return the populated entry. The negation mutant
	// (`if err == nil`) returns an empty DailyLog{} on success, dropping the
	// IsPeriod/Flow fields the caller relies on.
	logs := newMr3dayLogStub()
	users := &mr3dayUserStub{settings: models.User{PeriodLength: 5, AutoPeriodFill: false}}
	service := NewDayService(logs, users)

	day := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	now := day
	entry, err := service.UpsertDayEntryWithAutoFillAt(context.Background(), 10, day,
		DayEntryInput{IsPeriod: true, Flow: models.FlowMedium}, now, time.UTC)
	if err != nil {
		t.Fatalf("UpsertDayEntryWithAutoFillAt: %v", err)
	}
	if !entry.IsPeriod {
		t.Fatal("returned entry must have IsPeriod=true (mutant returns empty DailyLog{})")
	}
	if entry.Flow != models.FlowMedium {
		t.Fatalf("returned entry Flow = %q, want %q", entry.Flow, models.FlowMedium)
	}
}

// --- day_service.go:448 NEGATION — MarkCycleStartManually tx-close propagates error ---

func TestMR3Day_MarkCycleStartManually_PropagatesTxError(t *testing.T) {
	// Inject a repository error on the cycle-start flag write inside the
	// transaction. The transaction close `if err != nil { return err }` must
	// propagate it. The negation mutant (`if err == nil`) swallows the error
	// and returns nil, hiding a failed write from the caller.
	logs := newMr3dayLogStub()
	logs.failSaveWhenCycleStart = errors.New("boom: cycle-start save failed")
	users := &mr3dayUserStub{settings: models.User{PeriodLength: 5, AutoPeriodFill: false}}
	service := NewDayService(logs, users)

	now := time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC)
	day := now // today is always an allowed manual cycle-start date
	err := service.MarkCycleStartManually(context.Background(), 10, day, now, time.UTC,
		ManualCycleStartOptions{})
	if err == nil {
		t.Fatal("expected the injected tx error to propagate, got nil")
	}
	if !errors.Is(err, ErrManualCycleStartFailed) {
		t.Fatalf("expected ErrManualCycleStartFailed wrap, got %v", err)
	}
}

func mr3dayKeys(stub *mr3dayLogStub) []string {
	keys := make([]string, 0, len(stub.entries))
	for k := range stub.entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func equalStringSetMR3(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	g := append([]string(nil), got...)
	w := append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}
