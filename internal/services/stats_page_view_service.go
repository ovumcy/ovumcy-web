package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

var (
	ErrStatsPageViewLoadStats    = errors.New("stats page view load stats")
	ErrStatsPageViewLoadSymptoms = errors.New("stats page view load symptoms")
)

type StatsChartViewData struct {
	Labels      []string
	Values      []int
	Baseline    int
	HasBaseline bool
	Kind        string
}

type StatsSymptomCountViewData struct {
	Name             string
	Icon             string
	Count            int
	TotalDays        int
	FrequencySummary string
}

type StatsPageViewData struct {
	Stats                               CycleStats
	ChartData                           StatsChartViewData
	ChartBaseline                       int
	TrendPointCount                     int
	Flags                               StatsFlags
	PredictionSampleCount               int
	PredictionSampleUsesRecentWindow    bool
	PredictionReliabilityLabelKey       string
	PredictionReliabilityHintKey        string
	ShowPredictionReliability           bool
	RecentCycleFactors                  []StatsCycleFactorContextItem
	CycleFactorPatternSummaries         []StatsCycleFactorPatternSummary
	RecentFactorCycles                  []StatsCycleFactorRecentCycleSummary
	PredictionFactorHintKeys            []string
	LastCycleSymptoms                   []StatsSymptomCountViewData
	SymptomPatterns                     []StatsSymptomPatternViewData
	SymptomCounts                       []StatsSymptomCountViewData
	CurrentCycleBBTChart                StatsBBTChartViewData
	PhaseMoodInsights                   []StatsPhaseMoodInsight
	PhaseSymptomInsights                []StatsPhaseSymptomInsight
	HasLastCycleSymptoms                bool
	HasSymptomPatterns                  bool
	HasRecentCycleFactors               bool
	HasCycleFactorPatternSummaries      bool
	HasRecentFactorCycles               bool
	HasPredictionFactorHint             bool
	HasCurrentCycleBBTChart             bool
	HasPhaseMoodInsights                bool
	HasPhaseSymptomInsights             bool
	ShowIrregularityNotice              bool
	ShowIrregularInsufficientDataNotice bool
	ShowIrregularModeRecommendation     bool
	ShowAgeVariabilityHint              bool
	PredictionDisabled                  bool
	IsIrregularMode                     bool
	IsOwner                             bool
}

type statsPageBaseData struct {
	stats           CycleStats
	logs            []models.DailyLog
	chartData       StatsChartViewData
	chartBaseline   int
	trendPointCount int
	flags           StatsFlags
}

type statsOwnerInsightsViewData struct {
	lastCycleSymptoms       []StatsSymptomCountViewData
	symptomPatterns         []StatsSymptomPatternViewData
	currentCycleBBTChart    StatsBBTChartViewData
	phaseSymptomInsights    []StatsPhaseSymptomInsight
	hasPhaseSymptomInsights bool
}

func (service *StatsService) BuildStatsPageViewData(user *models.User, language string, cycleLabelPattern string, now time.Time, location *time.Location, maxTrendPoints int) (StatsPageViewData, error) {
	baseData, err := service.buildStatsPageBaseData(user, cycleLabelPattern, now, location, maxTrendPoints)
	if err != nil {
		return StatsPageViewData{}, err
	}

	frequencies, err := service.BuildSymptomFrequenciesForUser(user)
	if err != nil {
		return StatsPageViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadSymptoms, err)
	}
	symptomCounts := buildStatsSymptomCountViewData(language, frequencies)
	phaseMoodInsights, hasPhaseMoodInsights := service.BuildPhaseMoodInsights(user, baseData.logs, location)
	ownerInsights, err := service.buildOwnerStatsInsights(user, language, baseData.stats, baseData.logs, now, location)
	if err != nil {
		return StatsPageViewData{}, err
	}

	showIrregularityNotice := shouldShowStatsIrregularityNotice(user, baseData.flags, baseData.stats)
	showIrregularInsufficientDataNotice := shouldShowStatsIrregularInsufficientDataNotice(user, baseData.flags)
	showIrregularModeRecommendation := shouldShowStatsIrregularityNotice(user, baseData.flags, baseData.stats)
	showAgeVariabilityHint := shouldShowStatsAgeVariabilityHint(user)
	predictionDisabled := isStatsPredictionDisabled(user)
	isIrregularMode := isStatsIrregularMode(user)
	isOwner := IsOwnerUser(user)
	predictionSampleCount, predictionSampleUsesRecentWindow, predictionReliabilityLabelKey, predictionReliabilityHintKey, showPredictionReliability := buildStatsPredictionReliability(user, baseData.flags, baseData.stats)
	cycleFactorExplanation, hasCycleFactorExplanation := buildStatsCycleFactorExplanation(user, baseData.logs, baseData.stats, now, location)

	return StatsPageViewData{
		Stats:                               baseData.stats,
		ChartData:                           baseData.chartData,
		ChartBaseline:                       baseData.chartBaseline,
		TrendPointCount:                     baseData.trendPointCount,
		Flags:                               baseData.flags,
		PredictionSampleCount:               predictionSampleCount,
		PredictionSampleUsesRecentWindow:    predictionSampleUsesRecentWindow,
		PredictionReliabilityLabelKey:       predictionReliabilityLabelKey,
		PredictionReliabilityHintKey:        predictionReliabilityHintKey,
		ShowPredictionReliability:           showPredictionReliability,
		RecentCycleFactors:                  cycleFactorExplanation.RecentFactors,
		CycleFactorPatternSummaries:         cycleFactorExplanation.PatternSummaries,
		RecentFactorCycles:                  cycleFactorExplanation.RecentCycles,
		PredictionFactorHintKeys:            cycleFactorExplanation.HintFactorKeys,
		LastCycleSymptoms:                   ownerInsights.lastCycleSymptoms,
		SymptomPatterns:                     ownerInsights.symptomPatterns,
		SymptomCounts:                       symptomCounts,
		CurrentCycleBBTChart:                ownerInsights.currentCycleBBTChart,
		PhaseMoodInsights:                   phaseMoodInsights,
		PhaseSymptomInsights:                ownerInsights.phaseSymptomInsights,
		HasLastCycleSymptoms:                len(ownerInsights.lastCycleSymptoms) > 0,
		HasSymptomPatterns:                  len(ownerInsights.symptomPatterns) > 0,
		HasRecentCycleFactors:               hasCycleFactorExplanation && len(cycleFactorExplanation.RecentFactors) > 0,
		HasCycleFactorPatternSummaries:      hasCycleFactorExplanation && len(cycleFactorExplanation.PatternSummaries) > 0,
		HasRecentFactorCycles:               hasCycleFactorExplanation && len(cycleFactorExplanation.RecentCycles) > 0,
		HasPredictionFactorHint:             hasCycleFactorExplanation && len(cycleFactorExplanation.HintFactorKeys) > 0,
		HasCurrentCycleBBTChart:             len(ownerInsights.currentCycleBBTChart.Labels) > 0,
		HasPhaseMoodInsights:                hasPhaseMoodInsights,
		HasPhaseSymptomInsights:             ownerInsights.hasPhaseSymptomInsights,
		ShowIrregularityNotice:              showIrregularityNotice,
		ShowIrregularInsufficientDataNotice: showIrregularInsufficientDataNotice,
		ShowIrregularModeRecommendation:     showIrregularModeRecommendation,
		ShowAgeVariabilityHint:              showAgeVariabilityHint,
		PredictionDisabled:                  predictionDisabled,
		IsIrregularMode:                     isIrregularMode,
		IsOwner:                             isOwner,
	}, nil
}

func (service *StatsService) buildStatsPageBaseData(user *models.User, cycleLabelPattern string, now time.Time, location *time.Location, maxTrendPoints int) (statsPageBaseData, error) {
	stats, logs, err := service.BuildCycleStatsForRange(user, now.AddDate(-2, 0, 0), now, now, location)
	if err != nil {
		return statsPageBaseData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadStats, err)
	}

	lengths, baselineCycleLength := service.BuildTrend(user, logs, now, location, maxTrendPoints)
	trendPointCount := len(lengths)

	return statsPageBaseData{
		stats:           stats,
		logs:            logs,
		chartData:       buildStatsChartViewData(cycleLabelPattern, lengths, baselineCycleLength),
		chartBaseline:   baselineCycleLength,
		trendPointCount: trendPointCount,
		flags:           service.BuildFlags(user, logs, stats, now, location, trendPointCount),
	}, nil
}

func buildStatsChartViewData(cycleLabelPattern string, lengths []int, baselineCycleLength int) StatsChartViewData {
	chartData := StatsChartViewData{
		Labels: BuildCycleTrendLabels(cycleLabelPattern, len(lengths)),
		Values: lengths,
		Kind:   "bar",
	}
	if baselineCycleLength > 0 {
		chartData.Baseline = baselineCycleLength
		chartData.HasBaseline = true
	}
	return chartData
}

func buildStatsSymptomCountViewData(language string, frequencies []SymptomFrequency) []StatsSymptomCountViewData {
	symptomCounts := make([]StatsSymptomCountViewData, 0, len(frequencies))
	for _, item := range frequencies {
		symptomCounts = append(symptomCounts, StatsSymptomCountViewData{
			Name:             item.Name,
			Icon:             item.Icon,
			Count:            item.Count,
			TotalDays:        item.TotalDays,
			FrequencySummary: LocalizedSymptomFrequencySummary(language, item.Count, item.TotalDays),
		})
	}
	return symptomCounts
}

func (service *StatsService) buildOwnerStatsInsights(user *models.User, language string, stats CycleStats, logs []models.DailyLog, now time.Time, location *time.Location) (statsOwnerInsightsViewData, error) {
	insights := statsOwnerInsightsViewData{}
	if !IsOwnerUser(user) {
		return insights, nil
	}

	insights.currentCycleBBTChart = buildCurrentCycleBBTChart(stats, logs, now, location)
	if service.symptoms == nil {
		return insights, nil
	}

	symptomByID, err := service.phaseInsightSymptomMap(user.ID)
	if err != nil {
		return statsOwnerInsightsViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadSymptoms, err)
	}
	completedCycles := buildCompletedCycleSpans(logs, location)
	insights.lastCycleSymptoms = buildLastCycleSymptomCounts(language, logs, completedCycles, symptomByID, location)
	insights.symptomPatterns = buildSymptomPatternInsights(logs, completedCycles, symptomByID, location)
	insights.phaseSymptomInsights, insights.hasPhaseSymptomInsights = buildPhaseSymptomInsightsWithMap(logs, location, symptomByID)
	return insights, nil
}

func shouldShowStatsIrregularityNotice(user *models.User, flags StatsFlags, stats CycleStats) bool {
	return IsOwnerUser(user) && !user.IrregularCycle && flags.CompletedCycleCount >= 3 && IsIrregularCycleSpread(stats)
}

func shouldShowStatsIrregularInsufficientDataNotice(user *models.User, flags StatsFlags) bool {
	return user != nil && user.IrregularCycle && flags.CompletedCycleCount < 3
}

func shouldShowStatsAgeVariabilityHint(user *models.User) bool {
	return user != nil && NormalizeAgeGroup(user.AgeGroup) == models.AgeGroup35Plus
}

func isStatsPredictionDisabled(user *models.User) bool {
	return user != nil && user.UnpredictableCycle
}

func isStatsIrregularMode(user *models.User) bool {
	return user != nil && user.IrregularCycle
}

func buildStatsPredictionReliability(user *models.User, flags StatsFlags, stats CycleStats) (int, bool, string, string, bool) {
	if flags.CompletedCycleCount < statsMinimumInsightsCycles || isStatsPredictionDisabled(user) {
		return 0, false, "", "", false
	}

	sampleCount := flags.CompletedCycleCount
	usesRecentWindow := false
	if sampleCount > cyclePredictionWindow {
		sampleCount = cyclePredictionWindow
		usesRecentWindow = true
	}

	variablePattern := user != nil && (user.IrregularCycle || (flags.CompletedCycleCount >= minimumPhaseInsightCycles && IsIrregularCycleSpread(stats)))
	labelKey := "stats.reliability.early"

	switch {
	case variablePattern && sampleCount >= minimumPhaseInsightCycles:
		labelKey = "stats.reliability.variable"
	case sampleCount >= cyclePredictionWindow:
		labelKey = "stats.reliability.stable"
	case sampleCount >= minimumPhaseInsightCycles:
		labelKey = "stats.reliability.building"
	}

	hintKey := "stats.reliability.hint"
	if variablePattern {
		hintKey = "stats.reliability.hint_variable"
	}

	return sampleCount, usesRecentWindow, labelKey, hintKey, true
}
