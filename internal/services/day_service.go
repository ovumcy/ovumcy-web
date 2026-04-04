package services

import (
	"errors"
	"fmt"
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
	BBT                   float64
	CervicalMucus         string
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
	ListByUser(userID uint) ([]models.DailyLog, error)
	ListByUserRange(userID uint, fromStart *time.Time, toEnd *time.Time) ([]models.DailyLog, error)
	ListByUserDayRange(userID uint, dayStart time.Time, dayEnd time.Time) ([]models.DailyLog, error)
	FindByUserAndDayRange(userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error)
	Create(entry *models.DailyLog) error
	Save(entry *models.DailyLog) error
	DeleteByUserAndDayRange(userID uint, dayStart time.Time, dayEnd time.Time) error
}

type DayUserRepository interface {
	LoadSettingsByID(userID uint) (models.User, error)
	UpdateByID(userID uint, updates map[string]any) error
}

type DayService struct {
	logs  DayLogRepository
	users DayUserRepository
}

func NewDayService(logs DayLogRepository, users DayUserRepository) *DayService {
	return &DayService{
		logs:  logs,
		users: users,
	}
}

func (service *DayService) FetchLogsForUser(userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error) {
	fromStart, _ := DayRange(from, location)
	_, toEnd := DayRange(to, location)
	return service.logs.ListByUserRange(userID, &fromStart, &toEnd)
}

func (service *DayService) FetchLogsForOptionalRange(userID uint, from *time.Time, to *time.Time, location *time.Location) ([]models.DailyLog, error) {
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
	return service.logs.ListByUserRange(userID, fromStart, toEnd)
}

func (service *DayService) FetchAllLogsForUser(userID uint) ([]models.DailyLog, error) {
	return service.logs.ListByUser(userID)
}

func (service *DayService) FetchLogByDate(userID uint, day time.Time, location *time.Location) (models.DailyLog, error) {
	dayStart, dayEnd := DayRange(day, location)
	entry, found, err := service.logs.FindByUserAndDayRange(userID, dayStart, dayEnd)
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
			CycleFactorKeys: []string{},
			SymptomIDs:      []uint{},
		}, nil
	}
	entry.SexActivity = NormalizeDaySexActivity(entry.SexActivity)
	entry.CervicalMucus = NormalizeDayCervicalMucus(entry.CervicalMucus)
	entry.CycleFactorKeys, _ = NormalizeDayCycleFactorKeys(entry.CycleFactorKeys)
	if !IsValidDayBBT(entry.BBT) {
		entry.BBT = 0
	}
	return entry, nil
}

func (service *DayService) DayHasDataForDate(userID uint, day time.Time, location *time.Location) (bool, error) {
	dayStart, dayEnd := DayRange(day, location)
	entries, err := service.logs.ListByUserDayRange(userID, dayStart, dayEnd)
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

func (service *DayService) UpsertDayEntry(userID uint, dayStart time.Time, payload DayEntryInput, location *time.Location) (models.DailyLog, bool, error) {
	dayRangeStart, dayRangeEnd := DayRange(dayStart, location)
	entry, found, err := service.logs.FindByUserAndDayRange(userID, dayRangeStart, dayRangeEnd)
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
		entry.CycleFactorKeys = payload.CycleFactorKeys
		entry.SymptomIDs = payload.SymptomIDs
		entry.Notes = payload.Notes
		if err := service.logs.Save(&entry); err != nil {
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
		CycleFactorKeys: payload.CycleFactorKeys,
		Notes:           payload.Notes,
		SymptomIDs:      payload.SymptomIDs,
	}
	if err := service.logs.Create(&entry); err != nil {
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
			payload.BBT = 0
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

func (service *DayService) UpsertDayEntryWithAutoFill(userID uint, day time.Time, payload DayEntryInput, location *time.Location) (models.DailyLog, error) {
	return service.UpsertDayEntryWithAutoFillAt(userID, day, payload, time.Now(), location)
}

func (service *DayService) UpsertDayEntryWithAutoFillAt(userID uint, day time.Time, payload DayEntryInput, now time.Time, location *time.Location) (models.DailyLog, error) {
	if location == nil {
		location = time.UTC
	}

	normalized, err := NormalizeDayEntryInput(payload)
	if err != nil {
		return models.DailyLog{}, err
	}

	dayStart, _ := DayRange(day, location)
	autoPeriodFillEnabled := false
	periodLength := models.DefaultPeriodLength

	if normalized.IsPeriod {
		periodLength, autoPeriodFillEnabled, err = service.LoadAutoFillSettings(userID)
		if err != nil {
			return models.DailyLog{}, fmt.Errorf("%w: %v", ErrDayAutoFillLoadFailed, err)
		}
	}

	entry, wasPeriod, err := service.UpsertDayEntry(userID, dayStart, normalized, location)
	if err != nil {
		return models.DailyLog{}, err
	}

	if normalized.IsPeriod {
		shouldAutoFill, err := service.ShouldAutoFillPeriodDays(userID, dayStart, wasPeriod, autoPeriodFillEnabled, periodLength, location)
		if err != nil {
			return models.DailyLog{}, fmt.Errorf("%w: %v", ErrDayAutoFillCheckFailed, err)
		}
		if shouldAutoFill {
			if err := service.AutoFillFollowingPeriodDays(userID, dayStart, periodLength, normalized.Flow, now, location); err != nil {
				return models.DailyLog{}, fmt.Errorf("%w: %v", ErrDayAutoFillApplyFailed, err)
			}
		}
	}

	service.refreshDerivedCycleSettings(userID, location)

	return entry, nil
}

func (service *DayService) DeleteDayEntry(userID uint, day time.Time, location *time.Location) error {
	if err := service.DeleteDailyLogByDate(userID, day, location); err != nil {
		return ErrDeleteDayFailed
	}
	service.refreshDerivedCycleSettings(userID, location)
	return nil
}

func (service *DayService) ResolveManualCycleStartPolicy(user *models.User, day time.Time, now time.Time, location *time.Location) (ManualCycleStartPolicy, error) {
	logs, err := service.logs.ListByUser(user.ID)
	if err != nil {
		return ManualCycleStartPolicy{}, err
	}
	return ResolveManualCycleStartPolicy(user, logs, day, now, location), nil
}

func (service *DayService) AcknowledgePeriodTip(userID uint) error {
	if service == nil || service.users == nil {
		return nil
	}
	return service.users.UpdateByID(userID, map[string]any{
		"shown_period_tip": true,
	})
}

func (service *DayService) MarkCycleStartManually(userID uint, day time.Time, now time.Time, location *time.Location, options ManualCycleStartOptions) error {
	if !IsAllowedManualCycleStartDate(day, now, location) {
		return ErrManualCycleStartDateInvalid
	}

	policy, err := service.loadManualCycleStartPolicy(userID, day, now, location)
	if err != nil {
		return ErrDayEntryLoadFailed
	}
	if err := validateManualCycleStartOptions(policy, options); err != nil {
		return err
	}

	payload, err := service.manualCycleStartPayload(userID, day, location)
	if err != nil {
		return ErrDayEntryLoadFailed
	}

	if _, err := service.UpsertDayEntryWithAutoFillAt(userID, day, payload, now, location); err != nil {
		return err
	}

	entry, err := service.persistManualCycleStartFlags(userID, day, location, options, policy)
	if err != nil {
		return err
	}
	if err := service.clearCompetingManualCycleStarts(userID, entry, location); err != nil {
		return err
	}
	service.refreshDerivedCycleSettings(userID, location)

	return nil
}

func (service *DayService) loadManualCycleStartPolicy(userID uint, day time.Time, now time.Time, location *time.Location) (ManualCycleStartPolicy, error) {
	logs, err := service.logs.ListByUser(userID)
	if err != nil {
		return ManualCycleStartPolicy{}, err
	}
	userSettings, err := service.users.LoadSettingsByID(userID)
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

func (service *DayService) manualCycleStartPayload(userID uint, day time.Time, location *time.Location) (DayEntryInput, error) {
	existingEntry, err := service.FetchLogByDate(userID, day, location)
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
		CycleFactorKeys: append([]string{}, existingEntry.CycleFactorKeys...),
		Notes:           existingEntry.Notes,
		SymptomIDs:      symptomIDs,
	}
	if !IsValidDayFlow(payload.Flow) {
		payload.Flow = models.FlowNone
	}
	return payload, nil
}

func (service *DayService) persistManualCycleStartFlags(userID uint, day time.Time, location *time.Location, options ManualCycleStartOptions, policy ManualCycleStartPolicy) (models.DailyLog, error) {
	dayStart, _ := DayRange(day, location)
	dayEnd := dayStart.AddDate(0, 0, 1)
	entry, found, err := service.logs.FindByUserAndDayRange(userID, dayStart, dayEnd)
	if err != nil {
		return models.DailyLog{}, wrapManualCycleStartFailure(err)
	}
	if !found {
		return models.DailyLog{}, ErrManualCycleStartFailed
	}

	entry.CycleStart = true
	entry.IsUncertain = options.MarkUncertain && policy.ShortGapDays > 0
	if err := service.logs.Save(&entry); err != nil {
		return models.DailyLog{}, wrapManualCycleStartFailure(err)
	}
	return entry, nil
}

func (service *DayService) clearCompetingManualCycleStarts(userID uint, entry models.DailyLog, location *time.Location) error {
	allLogs, err := service.logs.ListByUser(userID)
	if err != nil {
		return wrapManualCycleStartFailure(err)
	}
	if err := service.clearCompetingCycleStarts(userID, allLogs, entry, location); err != nil {
		return wrapManualCycleStartFailure(err)
	}
	return nil
}

func wrapManualCycleStartFailure(err error) error {
	return fmt.Errorf("%w: %v", ErrManualCycleStartFailed, err)
}

func (service *DayService) DeleteDailyLogByDate(userID uint, day time.Time, location *time.Location) error {
	dayStart, dayEnd := DayRange(day, location)
	return service.logs.DeleteByUserAndDayRange(userID, dayStart, dayEnd)
}

func (service *DayService) LoadAutoFillSettings(userID uint) (int, bool, error) {
	persisted, err := service.users.LoadSettingsByID(userID)
	if err != nil {
		return models.DefaultPeriodLength, false, err
	}
	periodLength := persisted.PeriodLength
	if periodLength < 1 || periodLength > 14 {
		periodLength = models.DefaultPeriodLength
	}
	return periodLength, persisted.AutoPeriodFill, nil
}

func (service *DayService) ShouldAutoFillPeriodDays(userID uint, dayStart time.Time, wasPeriod bool, autoPeriodFillEnabled bool, periodLength int, location *time.Location) (bool, error) {
	if !autoPeriodFillEnabled || periodLength <= 1 || wasPeriod {
		return false, nil
	}

	previousDay := dayStart.AddDate(0, 0, -1)
	previousEntry, err := service.FetchLogByDate(userID, previousDay, location)
	if err != nil {
		return false, err
	}
	hasRecentPeriod, err := service.hasPeriodInRecentDays(userID, dayStart, 3, location)
	if err != nil {
		return false, err
	}
	return !previousEntry.IsPeriod && !hasRecentPeriod, nil
}

func (service *DayService) AutoFillFollowingPeriodDays(userID uint, startDay time.Time, periodLength int, flow string, now time.Time, location *time.Location) error {
	if periodLength <= 1 {
		return nil
	}
	if location == nil {
		location = time.UTC
	}

	today := DateAtLocation(now, location)
	for offset := 1; offset < periodLength; offset++ {
		targetDay := DateAtLocation(startDay.AddDate(0, 0, offset), location)
		if !today.IsZero() && targetDay.After(today) {
			break
		}
		entry, err := service.FetchLogByDate(userID, targetDay, location)
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
			if err := service.logs.Save(&entry); err != nil {
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
			CycleFactorKeys: []string{},
			SymptomIDs:      []uint{},
		}
		if err := service.logs.Create(&newEntry); err != nil {
			return err
		}
	}

	return nil
}

func (service *DayService) hasPeriodInRecentDays(userID uint, day time.Time, lookbackDays int, location *time.Location) (bool, error) {
	if lookbackDays <= 0 {
		return false, nil
	}
	for offset := 1; offset <= lookbackDays; offset++ {
		previousDay := day.AddDate(0, 0, -offset)
		entry, err := service.FetchLogByDate(userID, previousDay, location)
		if err != nil {
			return false, err
		}
		if entry.IsPeriod {
			return true, nil
		}
	}
	return false, nil
}

func (service *DayService) clearCompetingCycleStarts(userID uint, logs []models.DailyLog, selectedEntry models.DailyLog, location *time.Location) error {
	clusterStart, clusterEnd, ok := manualCycleStartClusterBounds(logs, selectedEntry.Date, location)
	if !ok {
		return nil
	}

	selectedDay := DateAtLocation(selectedEntry.Date, location)
	for _, logEntry := range logs {
		if logEntry.UserID != userID || !logEntry.CycleStart {
			continue
		}

		logDay := DateAtLocation(logEntry.Date, location)
		if logDay.Before(clusterStart) || logDay.After(clusterEnd) {
			continue
		}
		if sameCalendarDay(logDay, selectedDay) && logEntry.ID == selectedEntry.ID {
			continue
		}

		logEntry.CycleStart = false
		logEntry.IsUncertain = false
		if err := service.logs.Save(&logEntry); err != nil {
			return err
		}
	}

	return nil
}

func (service *DayService) refreshDerivedCycleSettings(userID uint, location *time.Location) {
	if service == nil || service.users == nil || service.logs == nil {
		return
	}

	logs, err := service.logs.ListByUser(userID)
	if err != nil {
		return
	}

	lutealPhase, ok := InferUserLutealPhase(logs, location)
	if !ok {
		lutealPhase = defaultLutealPhaseDays
	}
	_ = service.users.UpdateByID(userID, map[string]any{
		"luteal_phase": lutealPhase,
	})
}
