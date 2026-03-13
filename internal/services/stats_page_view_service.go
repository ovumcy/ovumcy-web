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
	SymptomCounts                       []StatsSymptomCountViewData
	PhaseMoodInsights                   []StatsPhaseMoodInsight
	PhaseSymptomInsights                []StatsPhaseSymptomInsight
	HasPhaseMoodInsights                bool
	HasPhaseSymptomInsights             bool
	ShowIrregularityNotice              bool
	ShowIrregularInsufficientDataNotice bool
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
	phaseSymptomInsights, hasPhaseSymptomInsights, err := service.BuildPhaseSymptomInsights(user, logs, location)
	if err != nil {
		return StatsPageViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadSymptoms, err)
	}

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

	return StatsPageViewData{
		Stats:                               stats,
		ChartData:                           chartData,
		ChartBaseline:                       baselineCycleLength,
		TrendPointCount:                     trendPointCount,
		Flags:                               flags,
		SymptomCounts:                       symptomCounts,
		PhaseMoodInsights:                   phaseMoodInsights,
		PhaseSymptomInsights:                phaseSymptomInsights,
		HasPhaseMoodInsights:                hasPhaseMoodInsights,
		HasPhaseSymptomInsights:             hasPhaseSymptomInsights,
		ShowIrregularityNotice:              IsOwnerUser(user) && !user.IrregularCycle && IsIrregularCycleSpread(stats),
		ShowIrregularInsufficientDataNotice: user != nil && user.IrregularCycle && flags.CompletedCycleCount < 2,
		IsIrregularMode:                     user != nil && user.IrregularCycle,
		IsOwner:                             IsOwnerUser(user),
	}, nil
}
