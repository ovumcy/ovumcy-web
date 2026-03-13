package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var (
	ErrDashboardViewLoadStats    = errors.New("dashboard view load stats")
	ErrDashboardViewLoadTodayLog = errors.New("dashboard view load today log")
	ErrDashboardViewLoadDayState = errors.New("dashboard view load day state")
	ErrDashboardViewLoadDayLog   = errors.New("dashboard view load day log")
	ErrDashboardViewLoadLogs     = errors.New("dashboard view load logs")
)

type DashboardStatsProvider interface {
	BuildCycleStatsForRange(user *models.User, from time.Time, to time.Time, now time.Time, location *time.Location) (CycleStats, []models.DailyLog, error)
}

type DashboardViewerProvider interface {
	FetchDayLogForViewer(user *models.User, day time.Time, location *time.Location) (models.DailyLog, []models.SymptomType, error)
}

type DashboardDayStateProvider interface {
	DayHasDataForDate(userID uint, day time.Time, location *time.Location) (bool, error)
	FetchAllLogsForUser(userID uint) ([]models.DailyLog, error)
}

type DashboardViewService struct {
	stats  DashboardStatsProvider
	viewer DashboardViewerProvider
	days   DashboardDayStateProvider
}

type DashboardViewData struct {
	Stats                    CycleStats
	CycleContext             DashboardCycleContext
	Today                    time.Time
	Yesterday                time.Time
	YesterdayMonth           string
	FormattedDate            string
	TodayLog                 models.DailyLog
	TodayHasData             bool
	TodayEntryExists         bool
	Symptoms                 []models.SymptomType
	PrimarySymptoms          []models.SymptomType
	ExtraSymptoms            []models.SymptomType
	HasExtraSymptoms         bool
	SelectedSymptomID        map[uint]bool
	ShowYesterdayJump        bool
	ShowSexChip              bool
	ShowBBTField             bool
	ShowCervicalMucus        bool
	AllowManualCycleStart    bool
	ManualCycleStartPolicy   ManualCycleStartPolicy
	ShowHighFertilityBadge   bool
	ShowCycleStartSuggestion bool
	IsOwner                  bool
}

type DayEditorViewData struct {
	Date                       time.Time
	DateString                 string
	DateLabel                  string
	IsFutureDate               bool
	Log                        models.DailyLog
	Symptoms                   []models.SymptomType
	PrimarySymptoms            []models.SymptomType
	ExtraSymptoms              []models.SymptomType
	HasExtraSymptoms           bool
	SelectedSymptomID          map[uint]bool
	HasDayData                 bool
	ShowSexChip                bool
	ShowBBTField               bool
	ShowCervicalMucus          bool
	AllowManualCycleStart      bool
	ManualCycleStartPolicy     ManualCycleStartPolicy
	ShowFutureCycleStartNotice bool
	ShowCycleStartSuggestion   bool
	IsOwner                    bool
}

func NewDashboardViewService(stats DashboardStatsProvider, viewer DashboardViewerProvider, days DashboardDayStateProvider) *DashboardViewService {
	return &DashboardViewService{
		stats:  stats,
		viewer: viewer,
		days:   days,
	}
}

func (service *DashboardViewService) BuildDashboardViewData(user *models.User, language string, now time.Time, location *time.Location) (DashboardViewData, error) {
	today := DateAtLocation(now, location)

	stats, _, err := service.stats.BuildCycleStatsForRange(user, today.AddDate(-2, 0, 0), today, now, location)
	if err != nil {
		return DashboardViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadStats, err)
	}

	todayLog, symptoms, err := service.viewer.FetchDayLogForViewer(user, today, location)
	if err != nil {
		return DashboardViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadTodayLog, err)
	}

	cycleContext := BuildDashboardCycleContext(user, stats, today, location)
	selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStartPolicy, showCycleStartSuggestion, err := service.buildPickerViewState(
		user,
		today,
		now,
		todayLog,
		symptoms,
		location,
	)
	if err != nil {
		return DashboardViewData{}, err
	}
	yesterday := today.AddDate(0, 0, -1)
	yesterdayHasData, err := service.days.DayHasDataForDate(user.ID, yesterday, location)
	if err != nil {
		return DashboardViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadDayState, err)
	}
	allowManualCycleStart := IsOwnerUser(user) && IsAllowedManualCycleStartDate(today, now, location)

	return DashboardViewData{
		Stats:                    stats,
		CycleContext:             cycleContext,
		Today:                    today,
		Yesterday:                yesterday,
		YesterdayMonth:           yesterday.Format("2006-01"),
		FormattedDate:            LocalizedDashboardDate(language, today),
		TodayLog:                 todayLog,
		TodayHasData:             DayHasData(todayLog),
		TodayEntryExists:         todayLog.ID != 0,
		Symptoms:                 rankedSymptoms,
		PrimarySymptoms:          primarySymptoms,
		ExtraSymptoms:            extraSymptoms,
		HasExtraSymptoms:         len(extraSymptoms) > 0,
		SelectedSymptomID:        selectedSymptomID,
		ShowYesterdayJump:        !yesterdayHasData,
		ShowSexChip:              IsOwnerUser(user) && !user.HideSexChip,
		ShowBBTField:             IsOwnerUser(user) && user.TrackBBT,
		ShowCervicalMucus:        IsOwnerUser(user) && user.TrackCervicalMucus,
		AllowManualCycleStart:    allowManualCycleStart,
		ManualCycleStartPolicy:   cycleStartPolicy,
		ShowHighFertilityBadge:   IsOwnerUser(user) && NormalizeDayCervicalMucus(todayLog.CervicalMucus) == models.CervicalMucusEggWhite,
		ShowCycleStartSuggestion: showCycleStartSuggestion,
		IsOwner:                  IsOwnerUser(user),
	}, nil
}

func (service *DashboardViewService) BuildDayEditorViewData(user *models.User, language string, day time.Time, now time.Time, location *time.Location) (DayEditorViewData, error) {
	hasDayData, err := service.days.DayHasDataForDate(user.ID, day, location)
	if err != nil {
		return DayEditorViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadDayState, err)
	}

	logEntry, symptoms, err := service.viewer.FetchDayLogForViewer(user, day, location)
	if err != nil {
		return DayEditorViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadDayLog, err)
	}
	selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStartPolicy, showCycleStartSuggestion, err := service.buildPickerViewState(
		user,
		day,
		now,
		logEntry,
		symptoms,
		location,
	)
	if err != nil {
		return DayEditorViewData{}, err
	}
	isFutureDate := day.After(DateAtLocation(now.In(location), location))
	allowManualCycleStart := IsOwnerUser(user) && IsAllowedManualCycleStartDate(day, now, location)

	return DayEditorViewData{
		Date:                       day,
		DateString:                 day.Format("2006-01-02"),
		DateLabel:                  LocalizedDateLabel(language, day),
		IsFutureDate:               isFutureDate,
		Log:                        logEntry,
		Symptoms:                   rankedSymptoms,
		PrimarySymptoms:            primarySymptoms,
		ExtraSymptoms:              extraSymptoms,
		HasExtraSymptoms:           len(extraSymptoms) > 0,
		SelectedSymptomID:          selectedSymptomID,
		HasDayData:                 hasDayData,
		ShowSexChip:                IsOwnerUser(user) && !user.HideSexChip,
		ShowBBTField:               IsOwnerUser(user) && user.TrackBBT,
		ShowCervicalMucus:          IsOwnerUser(user) && user.TrackCervicalMucus,
		AllowManualCycleStart:      allowManualCycleStart,
		ManualCycleStartPolicy:     cycleStartPolicy,
		ShowFutureCycleStartNotice: isFutureDate && allowManualCycleStart,
		ShowCycleStartSuggestion:   showCycleStartSuggestion,
		IsOwner:                    IsOwnerUser(user),
	}, nil
}

func (service *DashboardViewService) buildPickerViewState(user *models.User, day time.Time, now time.Time, logEntry models.DailyLog, symptoms []models.SymptomType, location *time.Location) (map[uint]bool, []models.SymptomType, []models.SymptomType, []models.SymptomType, ManualCycleStartPolicy, bool, error) {
	selectedSymptomID := SymptomIDSet(logEntry.SymptomIDs)
	rankedSymptoms := symptoms
	requiresLogs := len(symptoms) >= 2 || IsOwnerUser(user)
	if !requiresLogs {
		primarySymptoms, extraSymptoms := SplitSymptomsForCollapsedPicker(rankedSymptoms, selectedSymptomID, 8)
		return selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, ManualCycleStartPolicy{}, false, nil
	}

	logs, err := service.days.FetchAllLogsForUser(user.ID)
	if err != nil {
		return nil, nil, nil, nil, ManualCycleStartPolicy{}, false, fmt.Errorf("%w: %v", ErrDashboardViewLoadLogs, err)
	}
	if len(symptoms) >= 2 {
		rankedSymptoms = RankSymptomsForEntryPicker(symptoms, logs)
	}

	primarySymptoms, extraSymptoms := SplitSymptomsForCollapsedPicker(rankedSymptoms, selectedSymptomID, 8)
	showCycleStartSuggestion := ShouldSuggestManualCycleStart(user, logs, logEntry, day, now, location)
	cycleStartPolicy := ManualCycleStartPolicy{}
	if IsOwnerUser(user) {
		cycleStartPolicy = ResolveManualCycleStartPolicy(user, logs, day, now, location)
	}
	return selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStartPolicy, showCycleStartSuggestion, nil
}
