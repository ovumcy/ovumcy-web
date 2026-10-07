package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrDayEntryLoadFailed     = errors.New("load day entry failed")
	ErrDayEntryCreateFailed   = errors.New("create day entry failed")
	ErrDayEntryUpdateFailed   = errors.New("update day entry failed")
	ErrDayAutoFillLoadFailed  = errors.New("load day autofill settings failed")
	ErrDayAutoFillCheckFailed = errors.New("check day autofill failed")
	ErrDayAutoFillApplyFailed = errors.New("apply day autofill failed")
	ErrDeleteDayFailed        = errors.New("delete day failed")
	ErrManualCycleStartFailed = errors.New("manual cycle start failed")
)

type DayEntryInput struct {
	IsPeriod        bool
	Flow            string
	Mood            int
	SexActivity     string
	BBT             *float64
	CervicalMucus   string
	PregnancyTest   string
	CycleFactorKeys []string
	Notes           string
	SymptomIDs      []uint
	// ConfirmCycleStart carries the owner's answer to the inline question the
	// day form asks next to the period toggle ("does a new cycle begin here?").
	// It is set only for an explicit yes: an untouched control writes nothing.
	// The write below re-checks the policy that raised the question, so an
	// answer for a day the question was never asked on marks nothing.
	ConfirmCycleStart bool
	// PeriodFromStoredStart says the form showed the period ticked only
	// because the date is the stored onboarding start and has no row
	// (withOnboardingStartTicked); the day forms post it as a hidden field next
	// to that tick. Saving such a form without the period is the owner
	// un-ticking the start, so the write withdraws it. Without the field a
	// write of a row-less day carries no answer about the period at all (a
	// mood-only JSON PUT), and the start stays. Form transport only: the JSON
	// body has no key for it.
	PeriodFromStoredStart bool
	PreserveSexActivity   bool
	PreserveBBT           bool
	PreserveCervicalMucus bool
	PreserveCycleFactors  bool
	PreserveNotes         bool
}

type ManualCycleStartOptions struct {
	ReplaceExisting bool
	MarkUncertain   bool
}

type DayLogRepository interface {
	ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error)
	ListByUserRange(ctx context.Context, userID uint, fromStart *time.Time, toEnd *time.Time) ([]models.DailyLog, error)
	ListByUserDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) ([]models.DailyLog, error)
	FindByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error)
	// FindByUserAndDayRangeForUpdate is the read a write merges onto: inside a
	// transaction it holds the row until commit, where the database has row
	// locks.
	FindByUserAndDayRangeForUpdate(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error)
	Create(ctx context.Context, entry *models.DailyLog) error
	CreateBatch(ctx context.Context, entries []models.DailyLog) error
	Save(ctx context.Context, entry *models.DailyLog) error
	DeleteByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) error
}

// DayLogTxRunner executes fn against a transaction-scoped DayLogRepository so
// that all writes performed through the supplied repository commit or roll
// back atomically. Reads are also tx-scoped, so fn observes its own writes.
// Injected from the composition root; when nil the DayService falls back to a
// pass-through (non-atomic) execution, which is what unit tests with in-memory
// stubs rely on.
type DayLogTxRunner func(ctx context.Context, fn func(DayLogRepository) error) error

type DayUserRepository interface {
	LoadSettingsByID(ctx context.Context, userID uint) (models.User, error)
	UpdateByID(ctx context.Context, userID uint, updates map[string]any) error
}

type DayService struct {
	logs    DayLogRepository
	users   DayUserRepository
	runInTx DayLogTxRunner
}

// NewDayServiceWithTx wires a transaction runner so multi-step writes commit
// atomically. The composition root supplies runInTx; tests may omit it.
func NewDayServiceWithTx(logs DayLogRepository, users DayUserRepository, runInTx DayLogTxRunner) *DayService {
	return &DayService{
		logs:    logs,
		users:   users,
		runInTx: runInTx,
	}
}

// withinTransaction runs fn atomically when a runner is configured, otherwise
// it executes fn directly against the non-transactional repository.
func (service *DayService) withinTransaction(ctx context.Context, fn func(DayLogRepository) error) error {
	if service.runInTx != nil {
		return service.runInTx(ctx, fn)
	}
	return fn(service.logs)
}

func (service *DayService) FetchLogsForUser(ctx context.Context, userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error) {
	fromStart, _ := DayRange(from, location)
	_, toEnd := DayRange(to, location)
	return service.logs.ListByUserRange(ctx, userID, &fromStart, &toEnd)
}

func (service *DayService) FetchLogsForOptionalRange(ctx context.Context, userID uint, from *time.Time, to *time.Time, location *time.Location) ([]models.DailyLog, error) {
	var fromStart *time.Time
	var toEnd *time.Time
	if from != nil {
		start, _ := DayRange(*from, location)
		fromStart = &start
	}
	if to != nil {
		_, end := DayRange(*to, location)
		toEnd = &end
	}
	return service.logs.ListByUserRange(ctx, userID, fromStart, toEnd)
}

func (service *DayService) FetchAllLogsForUser(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	return service.logs.ListByUser(ctx, userID)
}

func (service *DayService) FetchLogByDate(ctx context.Context, userID uint, day time.Time, location *time.Location) (models.DailyLog, error) {
	return fetchLogByDate(ctx, service.logs.FindByUserAndDayRange, userID, day, location)
}

// fetchLogByDateForUpdate is FetchLogByDate through the locking read, for a
// write that saves the row it returns or builds its values from it: the row
// it writes back is then the row it read, not one a concurrent write of the
// same day has since replaced.
func (service *DayService) fetchLogByDateForUpdate(ctx context.Context, userID uint, day time.Time, location *time.Location) (models.DailyLog, error) {
	return fetchLogByDate(ctx, service.logs.FindByUserAndDayRangeForUpdate, userID, day, location)
}

func fetchLogByDate(ctx context.Context, find func(context.Context, uint, time.Time, time.Time) (models.DailyLog, bool, error), userID uint, day time.Time, location *time.Location) (models.DailyLog, error) {
	dayStart, dayEnd := DayRange(day, location)
	entry, found, err := find(ctx, userID, dayStart, dayEnd)
	if err != nil {
		return models.DailyLog{}, err
	}
	if !found {
		return models.DailyLog{
			UserID:          userID,
			Date:            dayStart,
			Flow:            models.FlowNone,
			Mood:            0,
			SexActivity:     models.SexActivityNone,
			CervicalMucus:   models.CervicalMucusNone,
			PregnancyTest:   models.PregnancyTestNone,
			CycleFactorKeys: []string{},
			SymptomIDs:      []uint{},
		}, nil
	}
	entry.SexActivity = NormalizeDaySexActivity(entry.SexActivity)
	entry.CervicalMucus = NormalizeDayCervicalMucus(entry.CervicalMucus)
	entry.PregnancyTest = NormalizeDayPregnancyTest(entry.PregnancyTest)
	entry.CycleFactorKeys, _ = NormalizeDayCycleFactorKeys(entry.CycleFactorKeys)
	if !IsValidDayBBT(entry.BBT) {
		entry.BBT = nil
	}
	return entry, nil
}

func (service *DayService) DayHasDataForDate(ctx context.Context, userID uint, day time.Time, location *time.Location) (bool, error) {
	dayStart, dayEnd := DayRange(day, location)
	entries, err := service.logs.ListByUserDayRange(ctx, userID, dayStart, dayEnd)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if DayHasData(entry) {
			return true, nil
		}
	}
	return false, nil
}

// UpsertDayEntry writes one owner's day. Its precondition is that callers
// normalise the payload through NormalizeDayEntryInput first: only the update
// branch merges anything (mergePreservedDayEntryInput), while the create branch
// writes the fields exactly as given and applies no preservation of its own.
// It returns the saved entry and the period mark and flow the day carried
// before the write (the zero value when the day did not exist), so the
// auto-fill side effects can read the anchor's prior state.
// Both branches first hold the write to the observation bound
// (validateDayObservationDate) against the row they locked, at now.
func (service *DayService) UpsertDayEntry(ctx context.Context, userID uint, dayStart time.Time, payload DayEntryInput, now time.Time, location *time.Location) (models.DailyLog, priorDayState, error) {
	// Defensive normalization: collapse any time-of-day or non-UTC offset on
	// the incoming dayStart back to canonical UTC-midnight. The intended
	// contract is "caller already invoked DayRange and is passing canonical
	// UTC-midnight", but if a future caller passes time.Now() or a
	// location-local midnight, the window below would silently drift and
	// re-introduce issue #64 — second upsert misses the existing row and the
	// follow-up Create collides with uidx_user_date. Cheaper to normalize
	// than to debug a regression.
	if !dayStart.IsZero() {
		year, month, day := dayStart.UTC().Date()
		dayStart = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	dayRangeStart := dayStart
	dayRangeEnd := dayStart.AddDate(0, 0, 1)
	// The locking read: the update below writes back every column of the row
	// it read — the preserved hidden fields, cycle_start and is_uncertain
	// included — so a plain read would let it revert a concurrent write of the
	// same day that committed after this read.
	entry, found, err := service.logs.FindByUserAndDayRangeForUpdate(ctx, userID, dayRangeStart, dayRangeEnd)
	if err != nil {
		return models.DailyLog{}, priorDayState{}, ErrDayEntryLoadFailed
	}
	if !found {
		entry = models.DailyLog{}
	}
	if err := validateDayObservationDate(entry, payload, dayStart, now, location); err != nil {
		return models.DailyLog{}, priorDayState{}, err
	}

	if found {
		previous := priorDayState{IsPeriod: entry.IsPeriod, Flow: entry.Flow, Found: true}
		payload = mergePreservedDayEntryInput(entry, payload)
		entry.IsPeriod = payload.IsPeriod
		if !payload.IsPeriod {
			entry.CycleStart = false
			entry.IsUncertain = false
		}
		entry.Flow = payload.Flow
		entry.Mood = payload.Mood
		entry.SexActivity = payload.SexActivity
		entry.BBT = payload.BBT
		entry.CervicalMucus = payload.CervicalMucus
		entry.PregnancyTest = payload.PregnancyTest
		entry.CycleFactorKeys = payload.CycleFactorKeys
		entry.SymptomIDs = payload.SymptomIDs
		entry.Notes = payload.Notes
		if err := service.logs.Save(ctx, &entry); err != nil {
			return models.DailyLog{}, priorDayState{}, ErrDayEntryUpdateFailed
		}
		return entry, previous, nil
	}

	entry = models.DailyLog{
		UserID:          userID,
		Date:            dayStart,
		IsPeriod:        payload.IsPeriod,
		Flow:            payload.Flow,
		Mood:            payload.Mood,
		SexActivity:     payload.SexActivity,
		BBT:             payload.BBT,
		CervicalMucus:   payload.CervicalMucus,
		PregnancyTest:   payload.PregnancyTest,
		CycleFactorKeys: payload.CycleFactorKeys,
		Notes:           payload.Notes,
		SymptomIDs:      payload.SymptomIDs,
	}
	if err := service.logs.Create(ctx, &entry); err != nil {
		var uniqueErr interface{ UniqueConstraint() string }
		if errors.As(err, &uniqueErr) {
			return models.DailyLog{}, priorDayState{}, errDayEntryCreatedConcurrently
		}
		return models.DailyLog{}, priorDayState{}, ErrDayEntryCreateFailed
	}
	return entry, priorDayState{}, nil
}

// priorDayState is the part of a day the auto-fill side effects read from
// before the write: whether it was a period day and the flow it carried.
// Found reports whether the day had a row at all.
type priorDayState struct {
	IsPeriod bool
	Flow     string
	Found    bool
}

// errDayEntryCreatedConcurrently is the create failure in which the day had no
// row when this write read it and a concurrent write inserted one before this
// insert: the unique (user_id, date) index refused it. It is still
// ErrDayEntryCreateFailed to every caller that does not retry; the partial
// write retries it once, onto the row that won.
var errDayEntryCreatedConcurrently = fmt.Errorf("%w: the day was created by a concurrent write", ErrDayEntryCreateFailed)

func mergePreservedDayEntryInput(existing models.DailyLog, payload DayEntryInput) DayEntryInput {
	if payload.PreserveSexActivity {
		payload.SexActivity = NormalizeDaySexActivity(existing.SexActivity)
	}
	if payload.PreserveBBT {
		if IsValidDayBBT(existing.BBT) {
			payload.BBT = existing.BBT
		} else {
			payload.BBT = nil
		}
	}
	if payload.PreserveCervicalMucus {
		payload.CervicalMucus = NormalizeDayCervicalMucus(existing.CervicalMucus)
	}
	if payload.PreserveCycleFactors {
		normalized, _ := NormalizeDayCycleFactorKeys(existing.CycleFactorKeys)
		payload.CycleFactorKeys = normalized
	}
	if payload.PreserveNotes {
		payload.Notes = TrimDayNotes(existing.Notes)
	}
	return payload
}

// DayEntryFields names the day fields one partial write states. A field it
// does not name is not that write's subject: PatchDayEntryWithAutoFillAt keeps
// its stored value, so an absent field never clears anything.
//
// The cycle-start flag is deliberately not a member. It is not a value a
// write states but a consequence of the stored period flag (a day that stops
// being a period day stops being a cycle start) or of an explicit mark, so a
// partial write that leaves is_period alone leaves cycle_start alone too.
type DayEntryFields struct {
	IsPeriod        bool
	Flow            bool
	Mood            bool
	SexActivity     bool
	BBT             bool
	CervicalMucus   bool
	PregnancyTest   bool
	CycleFactorKeys bool
	Notes           bool
	SymptomIDs      bool
}

// mergeDayEntryPatch builds the full day a partial write leaves behind: every
// field the write names takes the stated value, every other field the stored
// one. existing is the zero row when the day has none, so an absent field on a
// new day starts neutral. Stored values are carried in their normalized form,
// which keeps a legacy spelling already on disk from refusing a write that
// does not touch it; the stated values are validated afterwards exactly as a
// full write's are (NormalizeDayEntryInput), and the derived rules apply to
// the merged day — a stated is_period=false still clears flow and the cycle
// start, the same way a full write does.
//
// PeriodFromStoredStart is an answer about the period, so it travels only with
// a stated is_period. A partial write that leaves is_period out says nothing
// about the period — on a date without a row the merged day reads "no period"
// only because the zero row does — so it never withdraws the stored onboarding
// start, even when a form posts the hidden marker beside an unchecked box
// (which a partial write reads as "not stated", never as an un-tick).
func mergeDayEntryPatch(existing models.DailyLog, patch DayEntryInput, fields DayEntryFields) DayEntryInput {
	merged := patch
	if !fields.IsPeriod {
		merged.IsPeriod = existing.IsPeriod
		merged.PeriodFromStoredStart = false
	}
	if !fields.Flow {
		merged.Flow = NormalizeDayFlow(existing.Flow)
	}
	if !fields.Mood {
		merged.Mood = 0
		if IsValidDayMood(existing.Mood) {
			merged.Mood = existing.Mood
		}
	}
	if !fields.SexActivity {
		merged.SexActivity = NormalizeDaySexActivity(existing.SexActivity)
	}
	if !fields.BBT {
		merged.BBT = nil
		if IsValidDayBBT(existing.BBT) {
			merged.BBT = existing.BBT
		}
	}
	if !fields.CervicalMucus {
		merged.CervicalMucus = NormalizeDayCervicalMucus(existing.CervicalMucus)
	}
	if !fields.PregnancyTest {
		merged.PregnancyTest = NormalizeDayPregnancyTest(existing.PregnancyTest)
	}
	if !fields.CycleFactorKeys {
		merged.CycleFactorKeys, _ = NormalizeDayCycleFactorKeys(existing.CycleFactorKeys)
	}
	if !fields.Notes {
		merged.Notes = TrimDayNotes(existing.Notes)
	}
	if !fields.SymptomIDs {
		merged.SymptomIDs = append([]uint{}, existing.SymptomIDs...)
	}
	return merged
}

func (service *DayService) UpsertDayEntryWithAutoFillAt(ctx context.Context, userID uint, day time.Time, payload DayEntryInput, now time.Time, location *time.Location) (models.DailyLog, error) {
	if location == nil {
		location = time.UTC
	}

	normalized, err := NormalizeDayEntryInput(payload)
	if err != nil {
		return models.DailyLog{}, err
	}

	dayStart, _ := DayRange(day, location)
	return service.writeDayEntryWithAutoFill(ctx, userID, dayStart, now, location, func(DayLogRepository) (DayEntryInput, error) {
		return normalized, nil
	})
}

// PatchDayEntryWithAutoFillAt is the partial day write: only the fields named
// in fields change, and every other field keeps its stored value
// (mergeDayEntryPatch). The merge reads the stored row inside the same
// transaction as the write, so the day it merges onto is the day it replaces;
// a per-field precondition on the stored values belongs in that same step.
func (service *DayService) PatchDayEntryWithAutoFillAt(ctx context.Context, userID uint, day time.Time, patch DayEntryInput, fields DayEntryFields, now time.Time, location *time.Location) (models.DailyLog, error) {
	if location == nil {
		location = time.UTC
	}

	dayStart, dayEnd := DayRange(day, location)
	resolve := func(txLogs DayLogRepository) (DayEntryInput, error) {
		// The locking read: a concurrent partial write of the same day waits
		// for this transaction and then merges onto the row it leaves, rather
		// than both merging onto one old row and the later commit erasing the
		// fields the earlier one stated.
		existing, found, err := txLogs.FindByUserAndDayRangeForUpdate(ctx, userID, dayStart, dayEnd)
		if err != nil {
			return DayEntryInput{}, ErrDayEntryLoadFailed
		}
		if !found {
			existing = models.DailyLog{}
		}
		return NormalizeDayEntryInput(mergeDayEntryPatch(existing, patch, fields))
	}
	entry, err := service.writeDayEntryWithAutoFill(ctx, userID, dayStart, now, location, resolve)
	if errors.Is(err, errDayEntryCreatedConcurrently) {
		// The day had no row to lock and a concurrent write created it first.
		// One more transaction reads that row and merges onto it; the row now
		// exists, so the retry updates rather than inserts, and a second
		// refusal answers as the failed create it is.
		entry, err = service.writeDayEntryWithAutoFill(ctx, userID, dayStart, now, location, resolve)
	}
	return entry, err
}

// writeDayEntryWithAutoFill runs one day write, its period autofill and an
// inline cycle-start answer in one transaction. resolve yields the normalized
// full day to write; it runs inside that transaction against its repository,
// so a write that depends on the stored row reads the row it replaces.
func (service *DayService) writeDayEntryWithAutoFill(ctx context.Context, userID uint, dayStart time.Time, now time.Time, location *time.Location, resolve func(DayLogRepository) (DayEntryInput, error)) (models.DailyLog, error) {
	var entry models.DailyLog
	if err := service.withinTransaction(ctx, func(txLogs DayLogRepository) error {
		txService := &DayService{logs: txLogs, users: service.users}
		normalized, innerErr := resolve(txLogs)
		if innerErr != nil {
			return innerErr
		}
		entry, innerErr = txService.applyDayWriteAndAutoFill(ctx, userID, dayStart, normalized, now, location)
		if innerErr != nil {
			return innerErr
		}
		if !normalized.ConfirmCycleStart {
			return nil
		}
		entry, innerErr = txService.applyConfirmedCycleStart(ctx, userID, entry, dayStart, now, location)
		return innerErr
	}); err != nil {
		return models.DailyLog{}, err
	}

	service.refreshDerivedCycleSettings(ctx, userID, now, location)
	return entry, nil
}

// applyDayWriteAndAutoFill performs the anchor day write and its period
// autofill side effects. It carries no transaction of its own so callers can
// compose it inside a single WithinTransaction boundary.
func (service *DayService) applyDayWriteAndAutoFill(ctx context.Context, userID uint, dayStart time.Time, normalized DayEntryInput, now time.Time, location *time.Location) (models.DailyLog, error) {
	entry, previous, err := service.UpsertDayEntry(ctx, userID, dayStart, normalized, now, location)
	if err != nil {
		return models.DailyLog{}, err
	}
	// The day forms show the period ticked on a stored onboarding start that
	// has no row and say so in a hidden field (PeriodFromStoredStart), so a
	// save of that form without the period is the same un-tick as one over a
	// stored period day. Row absence alone is not that signal: a write that
	// never showed the tick (a mood-only JSON PUT) keeps the start.
	if !normalized.IsPeriod && (previous.IsPeriod || (!previous.Found && normalized.PeriodFromStoredStart)) {
		if err := service.withdrawOnboardingStartOn(ctx, userID, dayStart); err != nil {
			return models.DailyLog{}, err
		}
	}
	if err := service.applyPeriodAutoFillSideEffects(ctx, userID, dayStart, normalized, previous, now, location); err != nil {
		return models.DailyLog{}, err
	}
	return entry, nil
}

// lastPeriodStartClearer is the day-log repository's write of the one users
// column a day save may change: the stored onboarding start. The production
// repository implements it on its transaction handle (pinned by a compile-time
// assertion in the day service's integration test).
type lastPeriodStartClearer interface {
	ClearLastPeriodStartOn(ctx context.Context, userID uint, dayStart time.Time) error
}

// withdrawOnboardingStartOn handles the explicit un-mark on the date of the
// stored onboarding start (users.last_period_start): a save that turned a
// period day into a non-period day — or un-ticked the period a day form showed
// ticked from the stored start on a date without a row (the form posts
// PeriodFromStoredStart beside that tick) — and a delete of the day both clear
// that start, so the boundary it inserts is gone with the period day. A save
// that keeps the period ticked, one that only adds to an existing non-period
// row on the start date (a mood, a symptom), and a write of a row-less start
// date that carries no form tick (a mood-only JSON PUT) leave the start in
// place. dayStart is the canonical UTC-midnight
// key of the owner's calendar day, the shape the stored start has.
func (service *DayService) withdrawOnboardingStartOn(ctx context.Context, userID uint, dayStart time.Time) error {
	if clearer, ok := service.logs.(lastPeriodStartClearer); ok {
		if err := clearer.ClearLastPeriodStartOn(ctx, userID, dayStart); err != nil {
			return ErrDayEntryUpdateFailed
		}
		return nil
	}
	// A repository without the transactional write refuses when there is a
	// start to withdraw, as the Settings start mover does: clearing it through
	// the user repository would commit outside the day write's transaction, and
	// leaving it would keep a boundary the owner just un-marked.
	stored, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return ErrDayEntryLoadFailed
	}
	if stored.LastPeriodStart == nil || !dateOnly(*stored.LastPeriodStart).Equal(dateOnly(dayStart)) {
		return nil
	}
	return errLastPeriodStartClearUnsupported
}

var errLastPeriodStartClearUnsupported = errors.New("day log repository cannot withdraw the onboarding start with the day write")

// applyConfirmedCycleStart marks the day the owner just saved as a cycle start,
// but only when the same policy that raised the inline question still holds for
// the saved entry. Nothing here is inferred: without the explicit yes the
// caller never reaches this function, and a yes that no longer matches the
// policy (the day is not a period day, the day already is a cycle start, a
// competing start sits in the same period cluster) leaves the entry exactly as
// saved — corrections of that kind belong to the separate manual control with
// its own confirmations. It carries no transaction of its own so it composes
// inside the caller's boundary.
func (service *DayService) applyConfirmedCycleStart(ctx context.Context, userID uint, entry models.DailyLog, dayStart time.Time, now time.Time, location *time.Location) (models.DailyLog, error) {
	if !entry.IsPeriod || entry.CycleStart {
		return entry, nil
	}

	logs, err := service.logs.ListByUser(ctx, userID)
	if err != nil {
		return models.DailyLog{}, ErrDayEntryLoadFailed
	}
	userSettings, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return models.DailyLog{}, ErrDayEntryLoadFailed
	}
	// dayStart is the canonical UTC-midnight write key; the policy works on the
	// location-midnight calendar day, and CalendarDay converts without the
	// In(location) shift that would move the day backwards in UTC-minus locales.
	day := CalendarDay(dayStart, location)
	if !ShouldAskCycleStartQuestion(&userSettings, logs, entry, day, now, location) {
		return entry, nil
	}

	entry.CycleStart = true
	if err := service.logs.Save(ctx, &entry); err != nil {
		return models.DailyLog{}, ErrDayEntryUpdateFailed
	}
	return entry, nil
}

func (service *DayService) applyPeriodAutoFillSideEffects(ctx context.Context, userID uint, dayStart time.Time, normalized DayEntryInput, previous priorDayState, now time.Time, location *time.Location) error {
	wasPeriod := previous.IsPeriod
	if !normalized.IsPeriod && !wasPeriod {
		return nil
	}

	periodLength, autoPeriodFillEnabled, err := service.LoadAutoFillSettings(ctx, userID)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillLoadFailed, err)
	}

	if normalized.IsPeriod {
		return service.autoFillNewPeriodAnchor(ctx, userID, dayStart, wasPeriod, autoPeriodFillEnabled, periodLength, normalized.Flow, now, location)
	}
	if !autoPeriodFillEnabled {
		return nil
	}
	return service.clearAutoFilledNeighborsIfBare(ctx, userID, dayStart, periodLength, previous.Flow, location)
}

func (service *DayService) autoFillNewPeriodAnchor(ctx context.Context, userID uint, dayStart time.Time, wasPeriod bool, autoPeriodFillEnabled bool, periodLength int, flow string, now time.Time, location *time.Location) error {
	shouldAutoFill, err := service.ShouldAutoFillPeriodDays(ctx, userID, dayStart, wasPeriod, autoPeriodFillEnabled, periodLength, location)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillCheckFailed, err)
	}
	if !shouldAutoFill {
		return nil
	}
	if err := service.AutoFillFollowingPeriodDays(ctx, userID, dayStart, periodLength, flow, now, location); err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
	}
	return nil
}

func (service *DayService) clearAutoFilledNeighborsIfBare(ctx context.Context, userID uint, dayStart time.Time, periodLength int, propagatedFlow string, location *time.Location) error {
	shouldClear, err := service.shouldClearAutoFilledNeighbors(ctx, userID, dayStart, location)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillCheckFailed, err)
	}
	if !shouldClear {
		return nil
	}
	if err := service.ClearAutoFilledPeriodNeighbors(ctx, userID, dayStart, periodLength, propagatedFlow, location); err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
	}
	return nil
}

func (service *DayService) shouldClearAutoFilledNeighbors(ctx context.Context, userID uint, dayStart time.Time, location *time.Location) (bool, error) {
	previousDay := AddCalendarDays(dayStart, -1, location)
	previousEntry, err := service.FetchLogByDate(ctx, userID, previousDay, location)
	if err != nil {
		return false, err
	}
	return !previousEntry.IsPeriod, nil
}

// ClearAutoFilledPeriodNeighbors walks the periodLength-1 days following
// startDay and clears IsPeriod (plus the propagated Flow) on every contiguous
// auto-fill candidate. It stops at the first day that carries any manual
// signal so user edits are preserved; a flow other than propagatedFlow (the
// flow the unchecked anchor carried, which is the one value auto-fill writes
// into its neighbours) is such a signal. Mirrors the ovumcy-app
// `collectAutoFilledPeriodDaysToClear` heuristic.
func (service *DayService) ClearAutoFilledPeriodNeighbors(ctx context.Context, userID uint, startDay time.Time, periodLength int, propagatedFlow string, location *time.Location) error {
	if periodLength <= 1 {
		return nil
	}
	if location == nil {
		location = time.UTC
	}

	for offset := 1; offset < periodLength; offset++ {
		targetDay := AddCalendarDays(startDay, offset, location)
		dayRangeStart, dayRangeEnd := DayRange(targetDay, location)
		entry, found, err := service.logs.FindByUserAndDayRangeForUpdate(ctx, userID, dayRangeStart, dayRangeEnd)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if !IsAutoFilledPeriodCandidate(entry, propagatedFlow) {
			break
		}

		entry.IsPeriod = false
		entry.Flow = models.FlowNone
		if err := service.logs.Save(ctx, &entry); err != nil {
			return err
		}
	}

	return nil
}

// DeleteDayEntry removes the day's row and, when the day is the stored
// onboarding start, withdraws that start in the same transaction: deleting the
// day is an un-mark like un-ticking its period, and a start left behind would
// keep the calendar painting a period day the owner just removed.
func (service *DayService) DeleteDayEntry(ctx context.Context, userID uint, day time.Time, location *time.Location) error {
	if location == nil {
		location = time.UTC
	}
	dayStart, _ := DayRange(day, location)
	if err := service.withinTransaction(ctx, func(txLogs DayLogRepository) error {
		txService := &DayService{logs: txLogs, users: service.users}
		if err := txService.DeleteDailyLogByDate(ctx, userID, day, location); err != nil {
			return err
		}
		return txService.withdrawOnboardingStartOn(ctx, userID, dayStart)
	}); err != nil {
		return ErrDeleteDayFailed
	}
	// DeleteDayEntry carries no instant of its own — the transport never needed
	// one — and the derived column must still be bounded at the owner's today.
	// Reading the clock here keeps that bound rather than threading `now` through
	// the delete route for this one line.
	service.refreshDerivedCycleSettings(ctx, userID, time.Now(), location)
	return nil
}

func (service *DayService) ResolveManualCycleStartPolicy(ctx context.Context, user *models.User, day time.Time, now time.Time, location *time.Location) (ManualCycleStartPolicy, error) {
	logs, err := service.logs.ListByUser(ctx, user.ID)
	if err != nil {
		return ManualCycleStartPolicy{}, err
	}
	return ResolveManualCycleStartPolicy(user, logs, day, now, location), nil
}

func (service *DayService) AcknowledgePeriodTip(ctx context.Context, userID uint) error {
	if service == nil || service.users == nil {
		return nil
	}
	return service.users.UpdateByID(ctx, userID, map[string]any{
		"shown_period_tip": true,
	})
}

func (service *DayService) MarkCycleStartManually(ctx context.Context, userID uint, day time.Time, now time.Time, location *time.Location, options ManualCycleStartOptions) error {
	if !IsAllowedManualCycleStartDate(day, now, location) {
		return ErrManualCycleStartDateInvalid
	}

	policy, err := service.loadManualCycleStartPolicy(ctx, userID, day, now, location)
	if err != nil {
		return ErrDayEntryLoadFailed
	}
	if err := validateManualCycleStartOptions(policy, options); err != nil {
		return err
	}

	dayStart, _ := DayRange(day, location)
	if err := service.withinTransaction(ctx, func(txLogs DayLogRepository) error {
		txService := &DayService{logs: txLogs, users: service.users}
		// The payload carries every stored field of the day back into the
		// write, so it is read inside the write's transaction, through the
		// locking read: a concurrent write of the day committed before this
		// read is carried, and one after it waits for this commit.
		payload, err := txService.manualCycleStartPayload(ctx, userID, day, location)
		if err != nil {
			return ErrDayEntryLoadFailed
		}
		if _, err := txService.applyDayWriteAndAutoFill(ctx, userID, dayStart, payload, now, location); err != nil {
			return err
		}
		entry, err := txService.persistManualCycleStartFlags(ctx, userID, day, location, options, policy)
		if err != nil {
			return err
		}
		return txService.clearCompetingManualCycleStarts(ctx, userID, entry, location)
	}); err != nil {
		return err
	}
	service.refreshDerivedCycleSettings(ctx, userID, now, location)

	return nil
}

func (service *DayService) loadManualCycleStartPolicy(ctx context.Context, userID uint, day time.Time, now time.Time, location *time.Location) (ManualCycleStartPolicy, error) {
	logs, err := service.logs.ListByUser(ctx, userID)
	if err != nil {
		return ManualCycleStartPolicy{}, err
	}
	userSettings, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return ManualCycleStartPolicy{}, err
	}
	return ResolveManualCycleStartPolicy(&userSettings, logs, day, now, location), nil
}

func validateManualCycleStartOptions(policy ManualCycleStartPolicy, options ManualCycleStartOptions) error {
	if !policy.ConflictDate.IsZero() && !options.ReplaceExisting {
		return ErrManualCycleStartReplaceRequired
	}
	if policy.ShortGapDays > 0 && !options.MarkUncertain {
		return ErrManualCycleStartConfirmationNeeded
	}
	return nil
}

func (service *DayService) manualCycleStartPayload(ctx context.Context, userID uint, day time.Time, location *time.Location) (DayEntryInput, error) {
	existingEntry, err := service.fetchLogByDateForUpdate(ctx, userID, day, location)
	if err != nil {
		return DayEntryInput{}, err
	}

	symptomIDs := make([]uint, len(existingEntry.SymptomIDs))
	copy(symptomIDs, existingEntry.SymptomIDs)

	payload := DayEntryInput{
		IsPeriod:        true,
		Flow:            existingEntry.Flow,
		Mood:            existingEntry.Mood,
		SexActivity:     NormalizeDaySexActivity(existingEntry.SexActivity),
		BBT:             existingEntry.BBT,
		CervicalMucus:   NormalizeDayCervicalMucus(existingEntry.CervicalMucus),
		PregnancyTest:   NormalizeDayPregnancyTest(existingEntry.PregnancyTest),
		CycleFactorKeys: append([]string{}, existingEntry.CycleFactorKeys...),
		Notes:           existingEntry.Notes,
		SymptomIDs:      symptomIDs,
	}
	if !IsValidDayFlow(payload.Flow) {
		payload.Flow = models.FlowNone
	}
	return payload, nil
}

func (service *DayService) persistManualCycleStartFlags(ctx context.Context, userID uint, day time.Time, location *time.Location, options ManualCycleStartOptions, policy ManualCycleStartPolicy) (models.DailyLog, error) {
	dayStart, _ := DayRange(day, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	entry, found, err := service.logs.FindByUserAndDayRangeForUpdate(ctx, userID, dayStart, dayEnd)
	if err != nil {
		return models.DailyLog{}, wrapManualCycleStartFailure(err)
	}
	if !found {
		return models.DailyLog{}, ErrManualCycleStartFailed
	}

	entry.CycleStart = true
	entry.IsUncertain = options.MarkUncertain && policy.ShortGapDays > 0
	if err := service.logs.Save(ctx, &entry); err != nil {
		return models.DailyLog{}, wrapManualCycleStartFailure(err)
	}
	return entry, nil
}

func (service *DayService) clearCompetingManualCycleStarts(ctx context.Context, userID uint, entry models.DailyLog, location *time.Location) error {
	allLogs, err := service.logs.ListByUser(ctx, userID)
	if err != nil {
		return wrapManualCycleStartFailure(err)
	}
	if err := service.clearCompetingCycleStarts(ctx, userID, allLogs, entry, location); err != nil {
		return wrapManualCycleStartFailure(err)
	}
	return nil
}

func wrapManualCycleStartFailure(err error) error {
	return fmt.Errorf("%w: %v", ErrManualCycleStartFailed, err)
}

func (service *DayService) DeleteDailyLogByDate(ctx context.Context, userID uint, day time.Time, location *time.Location) error {
	dayStart, dayEnd := DayRange(day, location)
	return service.logs.DeleteByUserAndDayRange(ctx, userID, dayStart, dayEnd)
}

func (service *DayService) LoadAutoFillSettings(ctx context.Context, userID uint) (int, bool, error) {
	persisted, err := service.users.LoadSettingsByID(ctx, userID)
	if err != nil {
		return models.DefaultPeriodLength, false, err
	}
	periodLength := persisted.PeriodLength
	if periodLength < 1 || periodLength > 14 {
		periodLength = models.DefaultPeriodLength
	}
	return periodLength, persisted.AutoPeriodFill, nil
}

func (service *DayService) ShouldAutoFillPeriodDays(ctx context.Context, userID uint, dayStart time.Time, wasPeriod bool, autoPeriodFillEnabled bool, periodLength int, location *time.Location) (bool, error) {
	if !autoPeriodFillEnabled || periodLength <= 1 || wasPeriod {
		return false, nil
	}

	previousDay := AddCalendarDays(dayStart, -1, location)
	previousEntry, err := service.FetchLogByDate(ctx, userID, previousDay, location)
	if err != nil {
		return false, err
	}
	hasRecentPeriod, err := service.hasPeriodInRecentDays(ctx, userID, dayStart, 3, location)
	if err != nil {
		return false, err
	}
	return !previousEntry.IsPeriod && !hasRecentPeriod, nil
}

func (service *DayService) AutoFillFollowingPeriodDays(ctx context.Context, userID uint, startDay time.Time, periodLength int, flow string, now time.Time, location *time.Location) error {
	if periodLength <= 1 {
		return nil
	}
	if location == nil {
		location = time.UTC
	}

	lastDay := periodFillLastDay(startDay, periodLength, now, location)
	for offset := 1; offset < periodLength; offset++ {
		targetDay := AddCalendarDays(startDay, offset, location)
		if targetDay.After(lastDay) {
			break
		}
		entry, err := service.fetchLogByDateForUpdate(ctx, userID, targetDay, location)
		if err != nil {
			return err
		}

		if entry.ID != 0 {
			if DayHasData(entry) && !entry.IsPeriod {
				break
			}
			if entry.IsPeriod {
				continue
			}

			entry.IsPeriod = true
			entry.Flow = flow
			if err := service.logs.Save(ctx, &entry); err != nil {
				return err
			}
			continue
		}

		newEntry := models.DailyLog{
			UserID:          userID,
			Date:            targetDay,
			IsPeriod:        true,
			Flow:            flow,
			SexActivity:     models.SexActivityNone,
			CervicalMucus:   models.CervicalMucusNone,
			PregnancyTest:   models.PregnancyTestNone,
			CycleFactorKeys: []string{},
			SymptomIDs:      []uint{},
		}
		if err := service.logs.Create(ctx, &newEntry); err != nil {
			return err
		}
	}

	return nil
}

// periodFillLastDay is the last day a period auto-fill starting on startDay may
// write, as a midnight in location: the period's own last day, or the owner's
// local today when that comes first. A fill never records a period day the owner
// has not reached yet. Both auto-fills — the one behind a logged period start and
// the one behind onboarding completion — take their bound from here, so the two
// cannot drift apart.
func periodFillLastDay(startDay time.Time, periodLength int, now time.Time, location *time.Location) time.Time {
	lastDay := AddCalendarDays(startDay, periodLength-1, location)
	today := DateAtLocation(now, location)
	if !today.IsZero() && lastDay.After(today) {
		return today
	}
	return lastDay
}

func (service *DayService) hasPeriodInRecentDays(ctx context.Context, userID uint, day time.Time, lookbackDays int, location *time.Location) (bool, error) {
	if lookbackDays <= 0 {
		return false, nil
	}
	for offset := 1; offset <= lookbackDays; offset++ {
		previousDay := AddCalendarDays(day, -offset, location)
		entry, err := service.FetchLogByDate(ctx, userID, previousDay, location)
		if err != nil {
			return false, err
		}
		if entry.IsPeriod {
			return true, nil
		}
	}
	return false, nil
}

func (service *DayService) clearCompetingCycleStarts(ctx context.Context, userID uint, logs []models.DailyLog, selectedEntry models.DailyLog, location *time.Location) error {
	clusterStart, clusterEnd, ok := manualCycleStartClusterBounds(logs, selectedEntry.Date, location)
	if !ok {
		return nil
	}

	selectedDay := CalendarDay(selectedEntry.Date, location)
	for _, logEntry := range logs {
		if logEntry.UserID != userID || !logEntry.CycleStart {
			continue
		}

		logDay := CalendarDay(logEntry.Date, location)
		if !withinPeriodCluster(logDay, clusterStart, clusterEnd) {
			continue
		}
		if sameCalendarDay(logDay, selectedDay) && logEntry.ID == selectedEntry.ID {
			continue
		}

		// The save writes back every column of the row, so it writes the row
		// the locking read returns, not the list's copy: a concurrent write of
		// that day committed since the list was read is kept, not reverted.
		dayStart := logEntry.Date
		competing, found, err := service.logs.FindByUserAndDayRangeForUpdate(ctx, userID, dayStart, dayStart.AddDate(0, 0, 1))
		if err != nil {
			return err
		}
		if !found || !competing.CycleStart {
			continue
		}
		competing.CycleStart = false
		competing.IsUncertain = false
		if err := service.logs.Save(ctx, &competing); err != nil {
			return err
		}
	}

	return nil
}

// refreshDerivedCycleSettings is the single place the "day save" writer
// family (upsert, delete, manual cycle-start mark — every caller below)
// recomputes the persisted users.luteal_phase cache. The bound is the
// OWNER's today, never the request's: this column is also read by the
// request-free boot recompute (LutealPhaseRecomputer), which has no request
// zone to agree with and resolves purely through resolveOwnerLocation, so a
// write here bounded at the request's zone would silently disagree with the
// boot pass on any day the two zones name a different date — the disagreement
// self-heals only on the NEXT write, not before. `location` (the caller's
// request-resolved zone) stays the fallback for an owner with no captured
// timezone yet, exactly as calendar_feed_service.go's feedLocation already
// does. This calls resolveOwnerLocation, the one owner-timezone resolver
// (webhook_notify_service.go) — it does not add a second one.
func (service *DayService) refreshDerivedCycleSettings(ctx context.Context, userID uint, now time.Time, location *time.Location) {
	if service == nil || service.users == nil || service.logs == nil {
		return
	}

	logs, err := service.logs.ListByUser(ctx, userID)
	if err != nil {
		log.Printf("refreshDerivedCycleSettings: load logs for user %d failed: %v", userID, err)
		return
	}

	ownerLocation := location
	boundaryCtx := BoundaryContext{}
	if userSettings, err := service.users.LoadSettingsByID(ctx, userID); err != nil {
		log.Printf("refreshDerivedCycleSettings: load timezone for user %d failed: %v", userID, err)
	} else {
		ownerLocation = resolveOwnerLocation(userSettings.Timezone, location)
		boundaryCtx = BoundaryContextFor(&userSettings, time.Time{})
	}

	if err := service.users.UpdateByID(ctx, userID, map[string]any{
		"luteal_phase": deriveUserLutealPhase(logs, now, ownerLocation, boundaryCtx),
	}); err != nil {
		log.Printf("refreshDerivedCycleSettings: update luteal_phase for user %d failed: %v", userID, err)
	}
}
