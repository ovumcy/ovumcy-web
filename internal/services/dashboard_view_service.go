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

// DashboardViewData is everything the dashboard page renders.
//
// ShowFertilityStatus gates the fertility half of the status header — the
// fertile-window item and the status the header declares — on the decision the
// cycle context already resolved (DashboardCycleContext.FertilitySuppressed,
// which is FertilityProjectionSuppressed). It used to name the first-cycle
// floor alone, one disjunct of the four, so unpredictable-cycle mode, a
// pregnancy pause and an overdue cycle all rendered "Fertile window" — the last
// two beside the very notice saying the account's predictions are off.
//
// Stats is the PUBLISHED copy, cleared by PublishedStats: the gate and
// the data behind it move together, so a partial that forgets the gate renders
// nothing rather than the raw classification. Every builder in
// BuildDashboardViewData still reads the uncleared stats, because each applies
// its own suppression rule to them.
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
	ShowOvulationEstimate             bool
	ShowFirstCycleBridge              bool
	ShowFertilityStatus               bool
	ShowBBTInVisibleTier              bool
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

	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, location)
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
	yesterday := AddCalendarDays(today, -1, location)
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
	timingFrame := resolveDashboardTimingFrame(user, cycleContext, visibility)
	showHighFertilityBadge := dashboardHighFertilityBadge(user, todayLog)
	showSpottingCycleWarning := dashboardSpottingCycleWarning(logs, todayLog, today, location)
	reminderBanner := DashboardReminderBanner{}
	if IsOwnerUser(user) {
		reminderBanner = BuildDashboardReminderBanner(cycleContext, today, user.ReminderLeadDays)
	}

	// A confirmed thermal shift outranks the projection on the whole triple —
	// day, window, status — before either the published copy or the hero ribbon
	// reads it, so the header, the ring and the JSON API cannot name one day and
	// shade another. The resolver carries the same medical gate the ovulation
	// line above already resolves through, so this changes WHICH window is shown
	// and never whether one is shown at all.
	confirmedStats, _ := ResolveConfirmedCycleStats(user, logs, stats, today, location)

	// The same helper the stats page and the JSON API publish through, so the
	// surfaces cannot drift apart on what a suppressed tier is allowed to carry.
	// Every builder above still reads the uncleared stats.
	publishedStats, _ := PublishedStats(user, confirmedStats)

	return DashboardViewData{
		Stats:                             publishedStats,
		CycleContext:                      cycleContext,
		CycleHero:                         BuildDashboardCycleHero(user, confirmedStats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: location}),
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
		MoreFieldsOpen:                    dashboardMoreFieldsHoldData(todayLog, visibility, timingFrame.BBTInVisibleTier),
		ShowOvulationEstimate:             timingFrame.ShowOvulationEstimate,
		ShowFirstCycleBridge:              timingFrame.ShowFirstCycleBridge,
		ShowFertilityStatus:               !cycleContext.FertilitySuppressed,
		ShowBBTInVisibleTier:              timingFrame.BBTInVisibleTier,
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

// dashboardTimingFrame is the goal-aware half of the dashboard. An account
// tracking to conceive is on the page for timing, so the status header carries
// the ovulation estimate beside the next-period one and the morning temperature
// such an account records daily sits in the journal's visible tier instead of
// behind the "More" disclosure. Every other goal reads the page unchanged.
//
// ShowFirstCycleBridge is the one state where the timing item is replaced
// rather than dropped: an account with no completed cycle yet gets a single line
// naming when its fertile window arrives, so the goal it chose still answers it.
type dashboardTimingFrame struct {
	ShowOvulationEstimate bool
	ShowFirstCycleBridge  bool
	BBTInVisibleTier      bool
}

// resolveDashboardTimingFrame decides that frame from the resolved usage goal.
// The ovulation estimate is withheld wherever the next-period window is: an
// unpredictable cycle, a pregnancy pause, and a cycle overdue past its own
// reference length — it is derived from the same projection, so an account
// trying to conceive would otherwise be the one cohort still reading a
// placeholder where the window used to be. Before the first completed cycle
// (AwaitingFirstCycle) the projection has nothing but the onboarding slider to
// project from, so the estimate is manufactured rather than measured; that state
// gets the bridge line in place of the estimate — a promise about data, not a
// date. BBT keeps existing only where the tracking settings grant it, and its
// placement is a property of the goal alone: a late cycle is when a morning
// reading matters most, so nothing about the cycle demotes the field.
//
// The two items read DIFFERENT gates, and the difference is about what each one
// SAYS, not about which signals happen to be at hand.
//
// The ovulation estimate names a date, so it is a fertility claim and reads the
// decision the context already resolved (FertilitySuppressed =
// FertilityProjectionSuppressed) rather than rebuilding it from the disjuncts.
//
// The bridge line names NO date — it says the fertile window arrives once the
// first cycle closes. Suppression exists to withhold a claim, and there is no
// claim here to withhold, so the bridge asks only whether the account has
// predictions at all: PredictionDisabled (unpredictable-cycle mode, pregnancy
// pause), where a line promising a future window would contradict the page. It
// deliberately does NOT read NextPeriodEstimatePaused. That flag means "this
// projection is paused", which is a fact about a date the bridge does not name;
// reading it withdrew the line for an account whose FIRST cycle had run past the
// reference length — the moment the owner most needs to be told when the window
// arrives — and left the status slot empty instead. Nor does it read
// FertilitySuppressed: the bridge is the line shown IN the first-cycle floor, so
// a gate carrying that floor would gate the bridge on its own state.
func resolveDashboardTimingFrame(user *models.User, cycleContext DashboardCycleContext, visibility dashboardOwnerVisibility) dashboardTimingFrame {
	if !IsOwnerUser(user) || NormalizeUsageGoal(user.UsageGoal) != models.UsageGoalTrying {
		return dashboardTimingFrame{}
	}
	return dashboardTimingFrame{
		ShowOvulationEstimate: !cycleContext.FertilitySuppressed,
		ShowFirstCycleBridge:  !cycleContext.PredictionDisabled && cycleContext.AwaitingFirstCycle,
		BBTInVisibleTier:      visibility.ShowBBTField,
	}
}

// dashboardMoreFieldsHoldData answers whether the journal's "More" disclosure
// must render open: it does exactly when today already holds one of the values
// that live behind it. A field the owner's tracking settings hide is not
// rendered at all, so a value left behind in its column cannot open the
// disclosure over a control that does not exist — the visibility flags gate
// every clause. The pregnancy test has no tracking toggle and is always there.
// BBT is counted only while it is behind the disclosure: for a goal that lifts
// it into the visible tier a recorded temperature is already in the open, and
// opening the disclosure over it would fold nothing.
func dashboardMoreFieldsHoldData(entry models.DailyLog, visibility dashboardOwnerVisibility, bbtInVisibleTier bool) bool {
	if visibility.ShowSexChip && NormalizeDaySexActivity(entry.SexActivity) != models.SexActivityNone {
		return true
	}
	if visibility.ShowCervicalMucus && NormalizeDayCervicalMucus(entry.CervicalMucus) != models.CervicalMucusNone {
		return true
	}
	if NormalizeDayPregnancyTest(entry.PregnancyTest) != models.PregnancyTestNone {
		return true
	}
	if visibility.ShowBBTField && !bbtInVisibleTier && entry.BBT != nil && IsValidDayBBT(entry.BBT) {
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
		ShowBBTField:          isOwner && visibility.ShowBBTField,
		ShowCervicalMucus:     isOwner && visibility.ShowCervicalMucus,
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

	startDay := AddCalendarDays(today, -lookbackDays, location)
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
	for day := startDay; day.Before(today); day = AddCalendarDays(day, 1, location) {
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
