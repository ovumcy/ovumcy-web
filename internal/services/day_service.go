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
	IsPeriod              bool
	Flow                  string
	Mood                  int
	SexActivity           string
	BBT                   *float64
	CervicalMucus         string
	PregnancyTest         string
	CycleFactorKeys       []string
	Notes                 string
	SymptomIDs            []uint
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
	Create(ctx context.Context, entry *models.DailyLog) error
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

func NewDayService(logs DayLogRepository, users DayUserRepository) *DayService {
	return &DayService{
		logs:  logs,
		users: users,
	}
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
	dayStart, dayEnd := DayRange(day, location)
	entry, found, err := service.logs.FindByUserAndDayRange(ctx, userID, dayStart, dayEnd)
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

func (service *DayService) UpsertDayEntry(ctx context.Context, userID uint, dayStart time.Time, payload DayEntryInput, location *time.Location) (models.DailyLog, bool, error) {
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
	entry, found, err := service.logs.FindByUserAndDayRange(ctx, userID, dayRangeStart, dayRangeEnd)
	if err != nil {
		return models.DailyLog{}, false, ErrDayEntryLoadFailed
	}

	wasPeriod := false
	if found {
		wasPeriod = entry.IsPeriod
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
			return models.DailyLog{}, false, ErrDayEntryUpdateFailed
		}
		return entry, wasPeriod, nil
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
		return models.DailyLog{}, false, ErrDayEntryCreateFailed
	}
	return entry, false, nil
}

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

func (service *DayService) UpsertDayEntryWithAutoFill(ctx context.Context, userID uint, day time.Time, payload DayEntryInput, location *time.Location) (models.DailyLog, error) {
	return service.UpsertDayEntryWithAutoFillAt(ctx, userID, day, payload, time.Now(), location)
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

	var entry models.DailyLog
	if err := service.withinTransaction(ctx, func(txLogs DayLogRepository) error {
		txService := &DayService{logs: txLogs, users: service.users}
		var innerErr error
		entry, innerErr = txService.applyDayWriteAndAutoFill(ctx, userID, dayStart, normalized, now, location)
		return innerErr
	}); err != nil {
		return models.DailyLog{}, err
	}

	service.refreshDerivedCycleSettings(ctx, userID, location)
	return entry, nil
}

// applyDayWriteAndAutoFill performs the anchor day write and its period
// autofill side effects. It carries no transaction of its own so callers can
// compose it inside a single WithinTransaction boundary.
func (service *DayService) applyDayWriteAndAutoFill(ctx context.Context, userID uint, dayStart time.Time, normalized DayEntryInput, now time.Time, location *time.Location) (models.DailyLog, error) {
	entry, wasPeriod, err := service.UpsertDayEntry(ctx, userID, dayStart, normalized, location)
	if err != nil {
		return models.DailyLog{}, err
	}
	if err := service.applyPeriodAutoFillSideEffects(ctx, userID, dayStart, normalized, wasPeriod, now, location); err != nil {
		return models.DailyLog{}, err
	}
	return entry, nil
}

func (service *DayService) applyPeriodAutoFillSideEffects(ctx context.Context, userID uint, dayStart time.Time, normalized DayEntryInput, wasPeriod bool, now time.Time, location *time.Location) error {
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
	return service.clearAutoFilledNeighborsIfBare(ctx, userID, dayStart, periodLength, location)
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

func (service *DayService) clearAutoFilledNeighborsIfBare(ctx context.Context, userID uint, dayStart time.Time, periodLength int, location *time.Location) error {
	shouldClear, err := service.shouldClearAutoFilledNeighbors(ctx, userID, dayStart, location)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillCheckFailed, err)
	}
	if !shouldClear {
		return nil
	}
	if err := service.ClearAutoFilledPeriodNeighbors(ctx, userID, dayStart, periodLength, location); err != nil {
		return fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
	}
	return nil
}

func (service *DayService) shouldClearAutoFilledNeighbors(ctx context.Context, userID uint, dayStart time.Time, location *time.Location) (bool, error) {
	previousDay := dayStart.AddDate(0, 0, -1)
	previousEntry, err := service.FetchLogByDate(ctx, userID, previousDay, location)
	if err != nil {
		return false, err
	}
	return !previousEntry.IsPeriod, nil
}

// ClearAutoFilledPeriodNeighbors walks the periodLength-1 days following
// startDay and clears IsPeriod (plus the propagated Flow) on every contiguous
// auto-fill candidate. It stops at the first day that carries any manual
// signal so user edits are preserved. Mirrors the ovumcy-app
// `collectAutoFilledPeriodDaysToClear` heuristic.
func (service *DayService) ClearAutoFilledPeriodNeighbors(ctx context.Context, userID uint, startDay time.Time, periodLength int, location *time.Location) error {
	if periodLength <= 1 {
		return nil
	}
	if location == nil {
		location = time.UTC
	}

	for offset := 1; offset < periodLength; offset++ {
		targetDay := CalendarDay(startDay.AddDate(0, 0, offset), location)
		dayRangeStart, dayRangeEnd := DayRange(targetDay, location)
		entry, found, err := service.logs.FindByUserAndDayRange(ctx, userID, dayRangeStart, dayRangeEnd)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		if !IsAutoFilledPeriodCandidate(entry) {
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

func (service *DayService) DeleteDayEntry(ctx context.Context, userID uint, day time.Time, location *time.Location) error {
	if err := service.DeleteDailyLogByDate(ctx, userID, day, location); err != nil {
		return ErrDeleteDayFailed
	}
	service.refreshDerivedCycleSettings(ctx, userID, location)
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

	payload, err := service.manualCycleStartPayload(ctx, userID, day, location)
	if err != nil {
		return ErrDayEntryLoadFailed
	}

	dayStart, _ := DayRange(day, location)
	if err := service.withinTransaction(ctx, func(txLogs DayLogRepository) error {
		txService := &DayService{logs: txLogs, users: service.users}
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
	service.refreshDerivedCycleSettings(ctx, userID, location)

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
	existingEntry, err := service.FetchLogByDate(ctx, userID, day, location)
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
	entry, found, err := service.logs.FindByUserAndDayRange(ctx, userID, dayStart, dayEnd)
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

	previousDay := dayStart.AddDate(0, 0, -1)
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

	today := DateAtLocation(now, location)
	for offset := 1; offset < periodLength; offset++ {
		targetDay := CalendarDay(startDay.AddDate(0, 0, offset), location)
		if !today.IsZero() && targetDay.After(today) {
			break
		}
		entry, err := service.FetchLogByDate(ctx, userID, targetDay, location)
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

func (service *DayService) hasPeriodInRecentDays(ctx context.Context, userID uint, day time.Time, lookbackDays int, location *time.Location) (bool, error) {
	if lookbackDays <= 0 {
		return false, nil
	}
	for offset := 1; offset <= lookbackDays; offset++ {
		previousDay := day.AddDate(0, 0, -offset)
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
		if logDay.Before(clusterStart) || logDay.After(clusterEnd) {
			continue
		}
		if sameCalendarDay(logDay, selectedDay) && logEntry.ID == selectedEntry.ID {
			continue
		}

		logEntry.CycleStart = false
		logEntry.IsUncertain = false
		if err := service.logs.Save(ctx, &logEntry); err != nil {
			return err
		}
	}

	return nil
}

func (service *DayService) refreshDerivedCycleSettings(ctx context.Context, userID uint, location *time.Location) {
	if service == nil || service.users == nil || service.logs == nil {
		return
	}

	logs, err := service.logs.ListByUser(ctx, userID)
	if err != nil {
		log.Printf("refreshDerivedCycleSettings: load logs for user %d failed: %v", userID, err)
		return
	}

	lutealPhase, ok := InferUserLutealPhase(logs, location)
	if !ok {
		lutealPhase = defaultLutealPhaseDays
	}
	if err := service.users.UpdateByID(ctx, userID, map[string]any{
		"luteal_phase": lutealPhase,
	}); err != nil {
		log.Printf("refreshDerivedCycleSettings: update luteal_phase for user %d failed: %v", userID, err)
	}
}
