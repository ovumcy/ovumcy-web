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
	LastCycleSymptoms                   []StatsSymptomCountViewData
	SymptomPatterns                     []StatsSymptomPatternViewData
	SymptomCounts                       []StatsSymptomCountViewData
	CurrentCycleBBTChart                StatsBBTChartViewData
	PhaseMoodInsights                   []StatsPhaseMoodInsight
	PhaseSymptomInsights                []StatsPhaseSymptomInsight
	HasLastCycleSymptoms                bool
	HasSymptomPatterns                  bool
	HasCurrentCycleBBTChart             bool
	HasPhaseMoodInsights                bool
	HasPhaseSymptomInsights             bool
	ShowIrregularityNotice              bool
	ShowIrregularInsufficientDataNotice bool
	ShowIrregularModeRecommendation     bool
	IsIrregularMode                     bool
	IsOwner                             bool
}

func (service *StatsService) BuildStatsPageViewData(user *models.User, language string, cycleLabelPattern string, now time.Time, location *time.Location, maxTrendPoints int) (StatsPageViewData, error) {
	stats, logs, err := service.BuildCycleStatsForRange(user, now.AddDate(-2, 0, 0), now, now, location)
	if err != nil {
		return StatsPageViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadStats, err)
	}

	lengths, baselineCycleLength := service.BuildTrend(user, logs, now, location, maxTrendPoints)
	trendPointCount := len(lengths)
	flags := service.BuildFlags(user, logs, stats, now, location, trendPointCount)

	frequencies, err := service.BuildSymptomFrequenciesForUser(user)
	if err != nil {
		return StatsPageViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadSymptoms, err)
	}
	phaseMoodInsights, hasPhaseMoodInsights := service.BuildPhaseMoodInsights(user, logs, location)

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

	chartData := StatsChartViewData{
		Labels: BuildCycleTrendLabels(cycleLabelPattern, trendPointCount),
		Values: lengths,
		Kind:   "bar",
	}
	if baselineCycleLength > 0 {
		chartData.Baseline = baselineCycleLength
		chartData.HasBaseline = true
	}

	completedCycles := buildCompletedCycleSpans(logs, location)
	lastCycleSymptoms := []StatsSymptomCountViewData{}
	symptomPatterns := []StatsSymptomPatternViewData{}
	currentCycleBBTChart := StatsBBTChartViewData{}
	phaseSymptomInsights := []StatsPhaseSymptomInsight{}
	hasPhaseSymptomInsights := false
	if IsOwnerUser(user) {
		currentCycleBBTChart = buildCurrentCycleBBTChart(stats, logs, now, location)
	}
	if IsOwnerUser(user) && service.symptoms != nil {
		symptomByID, err := service.phaseInsightSymptomMap(user.ID)
		if err != nil {
			return StatsPageViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadSymptoms, err)
		}
		lastCycleSymptoms = buildLastCycleSymptomCounts(language, logs, completedCycles, symptomByID, location)
		symptomPatterns = buildSymptomPatternInsights(logs, completedCycles, symptomByID, location)
		phaseSymptomInsights, hasPhaseSymptomInsights = buildPhaseSymptomInsightsWithMap(logs, location, symptomByID)
	}

	return StatsPageViewData{
		Stats:                               stats,
		ChartData:                           chartData,
		ChartBaseline:                       baselineCycleLength,
		TrendPointCount:                     trendPointCount,
		Flags:                               flags,
		LastCycleSymptoms:                   lastCycleSymptoms,
		SymptomPatterns:                     symptomPatterns,
		SymptomCounts:                       symptomCounts,
		CurrentCycleBBTChart:                currentCycleBBTChart,
		PhaseMoodInsights:                   phaseMoodInsights,
		PhaseSymptomInsights:                phaseSymptomInsights,
		HasLastCycleSymptoms:                len(lastCycleSymptoms) > 0,
		HasSymptomPatterns:                  len(symptomPatterns) > 0,
		HasCurrentCycleBBTChart:             len(currentCycleBBTChart.Labels) > 0,
		HasPhaseMoodInsights:                hasPhaseMoodInsights,
		HasPhaseSymptomInsights:             hasPhaseSymptomInsights,
		ShowIrregularityNotice:              IsOwnerUser(user) && !user.IrregularCycle && flags.CompletedCycleCount >= 3 && IsIrregularCycleSpread(stats),
		ShowIrregularInsufficientDataNotice: user != nil && user.IrregularCycle && flags.CompletedCycleCount < 3,
		ShowIrregularModeRecommendation:     IsOwnerUser(user) && !user.IrregularCycle && flags.CompletedCycleCount >= 3 && IsIrregularCycleSpread(stats),
		IsIrregularMode:                     user != nil && user.IrregularCycle,
		IsOwner:                             IsOwnerUser(user),
	}, nil
}
