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
	Stats                             CycleStats
	CycleContext                      DashboardCycleContext
	Today                             time.Time
	Yesterday                         time.Time
	YesterdayMonth                    string
	FormattedDate                     string
	TodayLog                          models.DailyLog
	TodayHasData                      bool
	TodayEntryExists                  bool
	Symptoms                          []models.SymptomType
	PrimarySymptoms                   []models.SymptomType
	ExtraSymptoms                     []models.SymptomType
	HasExtraSymptoms                  bool
	SelectedSymptomID                 map[uint]bool
	ShowYesterdayJump                 bool
	ShowSexChip                       bool
	ShowBBTField                      bool
	ShowCervicalMucus                 bool
	AllowManualCycleStart             bool
	ManualCycleStartPolicy            ManualCycleStartPolicy
	ShowHighFertilityBadge            bool
	ShowMissedDaysLink                bool
	MissedDay                         time.Time
	ShowCycleStartSuggestion          bool
	ShowSpottingCycleWarning          bool
	PredictionExplanationPrimaryKey   string
	PredictionExplanationSecondaryKey string
	HasPredictionExplanationPrimary   bool
	HasPredictionExplanationSecondary bool
	PredictionFactorHintKeys          []string
	HasPredictionFactorHint           bool
	IsOwner                           bool
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
	ShowSpottingCycleWarning   bool
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
	logs, err := service.entryContextLogs(user, symptoms)
	if err != nil {
		return DashboardViewData{}, err
	}

	cycleContext := BuildDashboardCycleContext(user, stats, today, location)
	cycleFactorExplanation, hasCycleFactorExplanation := buildStatsCycleFactorExplanation(user, logs, stats, now, location)
	predictionExplanation := BuildOwnerPredictionExplanation(user, cycleContext, hasCycleFactorExplanation && len(cycleFactorExplanation.HintFactorKeys) > 0)
	selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStartPolicy, showCycleStartSuggestion, err := service.buildPickerViewState(
		user,
		today,
		now,
		todayLog,
		symptoms,
		logs,
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
	missedDay, showMissedDaysLink := firstMissingTrackedDay(logs, today, 14, user.CreatedAt, location)
	predictionExplanation, factorHintKeys, hasPredictionFactorHint := dashboardPredictionExplanationState(
		user,
		cycleContext,
		cycleFactorExplanation,
		hasCycleFactorExplanation,
	)
	showSexChip, showBBTField, showCervicalMucus, allowManualCycleStart := dashboardOwnerVisibilityState(user, today, now, location)
	showHighFertilityBadge := dashboardHighFertilityBadge(user, todayLog)
	showSpottingCycleWarning := dashboardSpottingCycleWarning(stats, todayLog, today, location)

	return DashboardViewData{
		Stats:                             stats,
		CycleContext:                      cycleContext,
		Today:                             today,
		Yesterday:                         yesterday,
		YesterdayMonth:                    yesterday.Format("2006-01"),
		FormattedDate:                     LocalizedDashboardDate(language, today),
		TodayLog:                          todayLog,
		TodayHasData:                      DayHasData(todayLog),
		TodayEntryExists:                  todayLog.ID != 0,
		Symptoms:                          rankedSymptoms,
		PrimarySymptoms:                   primarySymptoms,
		ExtraSymptoms:                     extraSymptoms,
		HasExtraSymptoms:                  len(extraSymptoms) > 0,
		SelectedSymptomID:                 selectedSymptomID,
		ShowYesterdayJump:                 !yesterdayHasData,
		ShowSexChip:                       showSexChip,
		ShowBBTField:                      showBBTField,
		ShowCervicalMucus:                 showCervicalMucus,
		AllowManualCycleStart:             allowManualCycleStart,
		ManualCycleStartPolicy:            cycleStartPolicy,
		ShowHighFertilityBadge:            showHighFertilityBadge,
		ShowMissedDaysLink:                showMissedDaysLink,
		MissedDay:                         missedDay,
		ShowCycleStartSuggestion:          showCycleStartSuggestion,
		ShowSpottingCycleWarning:          showSpottingCycleWarning,
		PredictionExplanationPrimaryKey:   predictionExplanation.PrimaryKey,
		PredictionExplanationSecondaryKey: predictionExplanation.SecondaryKey,
		HasPredictionExplanationPrimary:   predictionExplanation.PrimaryKey != "",
		HasPredictionExplanationSecondary: predictionExplanation.SecondaryKey != "",
		PredictionFactorHintKeys:          factorHintKeys,
		HasPredictionFactorHint:           hasPredictionFactorHint,
		IsOwner:                           IsOwnerUser(user),
	}, nil
}

func dashboardPredictionExplanationState(user *models.User, cycleContext DashboardCycleContext, cycleFactorExplanation StatsCycleFactorExplanation, hasCycleFactorExplanation bool) (PredictionExplanation, []string, bool) {
	factorHintKeys := cycleFactorExplanation.HintFactorKeys
	hasPredictionFactorHint := hasCycleFactorExplanation && len(factorHintKeys) > 0
	predictionExplanation := BuildOwnerPredictionExplanation(user, cycleContext, hasPredictionFactorHint)
	return predictionExplanation, factorHintKeys, hasPredictionFactorHint
}

func dashboardOwnerVisibilityState(user *models.User, today time.Time, now time.Time, location *time.Location) (bool, bool, bool, bool) {
	isOwner := IsOwnerUser(user)
	return isOwner && !user.HideSexChip,
		isOwner && user.TrackBBT,
		isOwner && user.TrackCervicalMucus,
		isOwner && IsAllowedManualCycleStartDate(today, now, location)
}

func dashboardHighFertilityBadge(user *models.User, todayLog models.DailyLog) bool {
	return IsOwnerUser(user) && NormalizeDayCervicalMucus(todayLog.CervicalMucus) == models.CervicalMucusEggWhite
}

func dashboardSpottingCycleWarning(stats CycleStats, todayLog models.DailyLog, today time.Time, location *time.Location) bool {
	return todayLog.IsPeriod &&
		NormalizeDayFlow(todayLog.Flow) == models.FlowSpotting &&
		!stats.LastPeriodStart.IsZero() &&
		sameCalendarDay(DateAtLocation(stats.LastPeriodStart, location), today)
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
	logs, err := service.entryContextLogs(user, symptoms)
	if err != nil {
		return DayEditorViewData{}, err
	}
	selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStartPolicy, showCycleStartSuggestion, err := service.buildPickerViewState(
		user,
		day,
		now,
		logEntry,
		symptoms,
		logs,
		location,
	)
	if err != nil {
		return DayEditorViewData{}, err
	}
	isFutureDate := day.After(DateAtLocation(now.In(location), location))
	allowManualCycleStart := IsOwnerUser(user) && IsAllowedManualCycleStartDate(day, now, location)
	stats, _, err := service.stats.BuildCycleStatsForRange(user, day.AddDate(-2, 0, 0), day, now, location)
	if err != nil {
		return DayEditorViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadStats, err)
	}

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
		ShowSpottingCycleWarning:   logEntry.IsPeriod && NormalizeDayFlow(logEntry.Flow) == models.FlowSpotting && !stats.LastPeriodStart.IsZero() && sameCalendarDay(DateAtLocation(stats.LastPeriodStart, location), day),
		IsOwner:                    IsOwnerUser(user),
	}, nil
}

func (service *DashboardViewService) entryContextLogs(user *models.User, symptoms []models.SymptomType) ([]models.DailyLog, error) {
	requiresLogs := len(symptoms) >= 2 || IsOwnerUser(user)
	if !requiresLogs {
		return nil, nil
	}

	logs, err := service.days.FetchAllLogsForUser(user.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDashboardViewLoadLogs, err)
	}
	return logs, nil
}

func (service *DashboardViewService) buildPickerViewState(user *models.User, day time.Time, now time.Time, logEntry models.DailyLog, symptoms []models.SymptomType, logs []models.DailyLog, location *time.Location) (map[uint]bool, []models.SymptomType, []models.SymptomType, []models.SymptomType, ManualCycleStartPolicy, bool, error) {
	selectedSymptomID := SymptomIDSet(logEntry.SymptomIDs)
	rankedSymptoms := symptoms
	if len(logs) == 0 {
		primarySymptoms, extraSymptoms := SplitSymptomsForCollapsedPicker(rankedSymptoms, selectedSymptomID, 8)
		return selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, ManualCycleStartPolicy{}, false, nil
	}
	if len(symptoms) >= 2 && completedCycleCountFromLogs(logs) >= 2 {
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

func completedCycleCountFromLogs(logs []models.DailyLog) int {
	starts := ObservedCycleStarts(logs)
	if len(starts) < 2 {
		return 0
	}
	return len(starts) - 1
}

func firstMissingTrackedDay(logs []models.DailyLog, today time.Time, lookbackDays int, trackingStart time.Time, location *time.Location) (time.Time, bool) {
	if lookbackDays < 3 {
		lookbackDays = 3
	}
	logByDay := make(map[string]bool, len(logs))
	for _, logEntry := range logs {
		logByDay[DateAtLocation(logEntry.Date, location).Format("2006-01-02")] = true
	}

	startDay := today.AddDate(0, 0, -lookbackDays)
	if !trackingStart.IsZero() {
		trackingStartDay := DateAtLocation(trackingStart, location)
		if trackingStartDay.After(startDay) {
			startDay = trackingStartDay
		}
	}
	if !startDay.Before(today) {
		return time.Time{}, false
	}
	missedCount := 0
	firstMissing := time.Time{}
	for day := startDay; day.Before(today); day = day.AddDate(0, 0, 1) {
		if logByDay[day.Format("2006-01-02")] {
			continue
		}
		missedCount++
		if firstMissing.IsZero() {
			firstMissing = day
		}
	}
	if missedCount < 3 || firstMissing.IsZero() {
		return time.Time{}, false
	}
	return firstMissing, true
}
