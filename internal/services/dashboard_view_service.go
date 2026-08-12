package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrDashboardViewLoadStats    = errors.New("dashboard view load stats")
	ErrDashboardViewLoadTodayLog = errors.New("dashboard view load today log")
	ErrDashboardViewLoadDayState = errors.New("dashboard view load day state")
	ErrDashboardViewLoadDayLog   = errors.New("dashboard view load day log")
	ErrDashboardViewLoadLogs     = errors.New("dashboard view load logs")
)

type DashboardStatsProvider interface {
	BuildCycleStatsForRange(ctx context.Context, user *models.User, from time.Time, to time.Time, now time.Time, location *time.Location) (CycleStats, []models.DailyLog, error)
	BuildCycleStatsFromLogs(user *models.User, logs []models.DailyLog, now time.Time, location *time.Location) CycleStats
}

type DashboardViewerProvider interface {
	FetchDayLogForViewer(ctx context.Context, user *models.User, day time.Time, location *time.Location) (models.DailyLog, []models.SymptomType, error)
}

type DashboardDayStateProvider interface {
	DayHasDataForDate(ctx context.Context, userID uint, day time.Time, location *time.Location) (bool, error)
	FetchAllLogsForUser(ctx context.Context, userID uint) ([]models.DailyLog, error)
}

type DashboardViewService struct {
	stats  DashboardStatsProvider
	viewer DashboardViewerProvider
	days   DashboardDayStateProvider
}

type DashboardViewData struct {
	Stats                             CycleStats
	CycleContext                      DashboardCycleContext
	CycleHero                         DashboardCycleHero
	ReminderBanner                    DashboardReminderBanner
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
	ShowCycleFactors                  bool
	ShowNotesField                    bool
	MoreFieldsOpen                    bool
	AllowManualCycleStart             bool
	ManualCycleStartPolicy            ManualCycleStartPolicy
	ShowHighFertilityBadge            bool
	ShowMissedDaysLink                bool
	MissedDay                         time.Time
	ShowCycleStartSuggestion          bool
	ShowCycleStartQuestion            bool
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
	ShowCycleFactors           bool
	ShowNotesField             bool
	AllowManualCycleStart      bool
	ManualCycleStartPolicy     ManualCycleStartPolicy
	ShowFutureCycleStartNotice bool
	ShowCycleStartSuggestion   bool
	ShowCycleStartQuestion     bool
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

func (service *DashboardViewService) BuildDashboardViewData(ctx context.Context, user *models.User, language string, now time.Time, location *time.Location) (DashboardViewData, error) {
	today := DateAtLocation(now, location)

	todayLog, symptoms, err := service.viewer.FetchDayLogForViewer(ctx, user, today, location)
	if err != nil {
		return DashboardViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadTodayLog, err)
	}

	stats, logs, err := service.buildDashboardStats(ctx, user, symptoms, today, now, location)
	if err != nil {
		return DashboardViewData{}, err
	}

	cycleContext := BuildDashboardCycleContext(user, stats, today, location)
	cycleFactorExplanation, hasCycleFactorExplanation := buildStatsCycleFactorExplanation(user, logs, stats, now, location)
	selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStart, err := service.buildPickerViewState(
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
	yesterdayHasData, err := service.days.DayHasDataForDate(ctx, user.ID, yesterday, location)
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
	visibility := dashboardOwnerVisibilityState(user, today, now, location)
	showHighFertilityBadge := dashboardHighFertilityBadge(user, todayLog)
	showSpottingCycleWarning := dashboardSpottingCycleWarning(logs, todayLog, today, location)
	reminderBanner := DashboardReminderBanner{}
	if IsOwnerUser(user) {
		reminderBanner = BuildDashboardReminderBanner(cycleContext, today, user.ReminderLeadDays)
	}

	return DashboardViewData{
		Stats:                             stats,
		CycleContext:                      cycleContext,
		CycleHero:                         BuildDashboardCycleHero(user, stats, cycleContext),
		ReminderBanner:                    reminderBanner,
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
		ShowSexChip:                       visibility.ShowSexChip,
		ShowBBTField:                      visibility.ShowBBTField,
		ShowCervicalMucus:                 visibility.ShowCervicalMucus,
		ShowCycleFactors:                  visibility.ShowCycleFactors,
		ShowNotesField:                    visibility.ShowNotesField,
		MoreFieldsOpen:                    dashboardMoreFieldsHoldData(todayLog, visibility),
		AllowManualCycleStart:             visibility.AllowManualCycleStart,
		ManualCycleStartPolicy:            cycleStart.Policy,
		ShowHighFertilityBadge:            showHighFertilityBadge,
		ShowMissedDaysLink:                showMissedDaysLink,
		MissedDay:                         missedDay,
		ShowCycleStartSuggestion:          cycleStart.ShowSuggestion,
		ShowCycleStartQuestion:            cycleStart.AskQuestion,
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

type dashboardOwnerVisibility struct {
	ShowSexChip           bool
	ShowBBTField          bool
	ShowCervicalMucus     bool
	ShowCycleFactors      bool
	ShowNotesField        bool
	AllowManualCycleStart bool
}

// dashboardMoreFieldsHoldData answers whether the journal's "More" disclosure
// must render open: it does exactly when today already holds one of the values
// that live behind it. A field the owner's tracking settings hide is not
// rendered at all, so a value left behind in its column cannot open the
// disclosure over a control that does not exist — the visibility flags gate
// every clause. The pregnancy test has no tracking toggle and is always there.
func dashboardMoreFieldsHoldData(entry models.DailyLog, visibility dashboardOwnerVisibility) bool {
	if visibility.ShowSexChip && NormalizeDaySexActivity(entry.SexActivity) != models.SexActivityNone {
		return true
	}
	if visibility.ShowCervicalMucus && NormalizeDayCervicalMucus(entry.CervicalMucus) != models.CervicalMucusNone {
		return true
	}
	if NormalizeDayPregnancyTest(entry.PregnancyTest) != models.PregnancyTestNone {
		return true
	}
	if visibility.ShowBBTField && entry.BBT != nil && IsValidDayBBT(entry.BBT) {
		return true
	}
	if visibility.ShowCycleFactors && len(DayCycleFactorKeySet(entry.CycleFactorKeys)) > 0 {
		return true
	}
	return visibility.ShowNotesField && strings.TrimSpace(entry.Notes) != ""
}

func dashboardOwnerVisibilityState(user *models.User, today time.Time, now time.Time, location *time.Location) dashboardOwnerVisibility {
	isOwner := IsOwnerUser(user)
	visibility := TrackingVisibilityForUser(user)
	return dashboardOwnerVisibility{
		ShowSexChip:           isOwner && visibility.ShowSexChip,
		ShowBBTField:          isOwner && user.TrackBBT,
		ShowCervicalMucus:     isOwner && user.TrackCervicalMucus,
		ShowCycleFactors:      isOwner && visibility.ShowCycleFactors,
		ShowNotesField:        isOwner && visibility.ShowNotesField,
		AllowManualCycleStart: isOwner && IsAllowedManualCycleStartDate(today, now, location),
	}
}

func dashboardHighFertilityBadge(user *models.User, todayLog models.DailyLog) bool {
	return IsOwnerUser(user) && NormalizeDayCervicalMucus(todayLog.CervicalMucus) == models.CervicalMucusEggWhite
}

func dashboardSpottingCycleWarning(logs []models.DailyLog, todayLog models.DailyLog, today time.Time, location *time.Location) bool {
	return shouldShowSpottingCycleWarning(logs, todayLog, today, location)
}

func (service *DashboardViewService) BuildDayEditorViewData(ctx context.Context, user *models.User, language string, day time.Time, now time.Time, location *time.Location) (DayEditorViewData, error) {
	hasDayData, err := service.days.DayHasDataForDate(ctx, user.ID, day, location)
	if err != nil {
		return DayEditorViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadDayState, err)
	}

	logEntry, symptoms, err := service.viewer.FetchDayLogForViewer(ctx, user, day, location)
	if err != nil {
		return DayEditorViewData{}, fmt.Errorf("%w: %v", ErrDashboardViewLoadDayLog, err)
	}
	logs, err := service.entryContextLogs(ctx, user, symptoms)
	if err != nil {
		return DayEditorViewData{}, err
	}
	selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStart, err := service.buildPickerViewState(
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
	visibility := dashboardOwnerVisibilityState(user, day, now, location)
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
		ShowSexChip:                visibility.ShowSexChip,
		ShowBBTField:               visibility.ShowBBTField,
		ShowCervicalMucus:          visibility.ShowCervicalMucus,
		ShowCycleFactors:           visibility.ShowCycleFactors,
		ShowNotesField:             visibility.ShowNotesField,
		AllowManualCycleStart:      visibility.AllowManualCycleStart,
		ManualCycleStartPolicy:     cycleStart.Policy,
		ShowFutureCycleStartNotice: isFutureDate && visibility.AllowManualCycleStart,
		ShowCycleStartSuggestion:   cycleStart.ShowSuggestion,
		ShowCycleStartQuestion:     cycleStart.AskQuestion,
		ShowSpottingCycleWarning:   shouldShowSpottingCycleWarning(logs, logEntry, day, location),
		IsOwner:                    IsOwnerUser(user),
	}, nil
}

func requiresEntryContextLogs(user *models.User, symptoms []models.SymptomType) bool {
	return len(symptoms) >= 2 || IsOwnerUser(user)
}

func (service *DashboardViewService) entryContextLogs(ctx context.Context, user *models.User, symptoms []models.SymptomType) ([]models.DailyLog, error) {
	if !requiresEntryContextLogs(user, symptoms) {
		return nil, nil
	}

	logs, err := service.days.FetchAllLogsForUser(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDashboardViewLoadLogs, err)
	}
	return logs, nil
}

// buildDashboardStats computes the dashboard's 2-year cycle stats. When entry
// context logs are needed anyway (owner view, or >=2 symptoms — the common
// case), it fetches the full log history once via entryContextLogs and
// derives the 2-year stats window from it in memory, instead of issuing a
// second, near-duplicate daily_logs query for a range that mostly overlaps
// the full history. Otherwise it falls back to the single ranged query.
func (service *DashboardViewService) buildDashboardStats(ctx context.Context, user *models.User, symptoms []models.SymptomType, today time.Time, now time.Time, location *time.Location) (CycleStats, []models.DailyLog, error) {
	statsFrom := today.AddDate(-2, 0, 0)
	if !requiresEntryContextLogs(user, symptoms) {
		stats, _, err := service.stats.BuildCycleStatsForRange(ctx, user, statsFrom, today, now, location)
		if err != nil {
			return CycleStats{}, nil, fmt.Errorf("%w: %v", ErrDashboardViewLoadStats, err)
		}
		return stats, nil, nil
	}

	logs, err := service.entryContextLogs(ctx, user, symptoms)
	if err != nil {
		return CycleStats{}, nil, err
	}
	rangeLogs := FilterLogsByDateRange(logs, statsFrom, today, location)
	stats := service.stats.BuildCycleStatsFromLogs(user, rangeLogs, now, location)
	return stats, logs, nil
}

// dayFormCycleStartState groups the cycle-start flags one day form needs: the
// manual control's policy, the plain suggestion hint, and whether the form asks
// the inline "does a new cycle begin here?" question beside the period toggle.
type dayFormCycleStartState struct {
	Policy         ManualCycleStartPolicy
	ShowSuggestion bool
	AskQuestion    bool
}

func (service *DashboardViewService) buildPickerViewState(user *models.User, day time.Time, now time.Time, logEntry models.DailyLog, symptoms []models.SymptomType, logs []models.DailyLog, location *time.Location) (map[uint]bool, []models.SymptomType, []models.SymptomType, []models.SymptomType, dayFormCycleStartState, error) {
	selectedSymptomID := SymptomIDSet(logEntry.SymptomIDs)
	rankedSymptoms := symptoms
	if len(logs) == 0 {
		primarySymptoms, extraSymptoms := SplitSymptomsForCollapsedPicker(rankedSymptoms, selectedSymptomID, 8)
		return selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, dayFormCycleStartState{}, nil
	}
	if len(symptoms) >= 2 && completedCycleCountFromLogs(logs) >= 2 {
		rankedSymptoms = RankSymptomsForEntryPicker(symptoms, logs)
	}

	primarySymptoms, extraSymptoms := SplitSymptomsForCollapsedPicker(rankedSymptoms, selectedSymptomID, 8)
	cycleStart := dayFormCycleStartState{
		ShowSuggestion: ShouldSuggestManualCycleStart(user, logs, logEntry, day, now, location),
	}
	if IsOwnerUser(user) {
		cycleStart.Policy = ResolveManualCycleStartPolicy(user, logs, day, now, location)
		cycleStart.AskQuestion = ShouldAskCycleStartQuestion(user, logs, logEntry, day, now, location)
	}
	return selectedSymptomID, rankedSymptoms, primarySymptoms, extraSymptoms, cycleStart, nil
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
		logByDay[CalendarDay(logEntry.Date, location).Format("2006-01-02")] = true
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
