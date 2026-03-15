package services

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type StatsDayReader interface {
	FetchLogsForUser(userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error)
	FetchAllLogsForUser(userID uint) ([]models.DailyLog, error)
}

type StatsSymptomReader interface {
	CalculateFrequencies(userID uint, logs []models.DailyLog) ([]SymptomFrequency, error)
	FetchSymptoms(userID uint) ([]models.SymptomType, error)
}

type StatsService struct {
	days     StatsDayReader
	symptoms StatsSymptomReader
}

const statsOverviewWindowYears = 2

const (
	statsMinimumInsightsCycles = 2
	statsReliableTrendCycles   = 3
)

type StatsFlags struct {
	HasObservedCycleData bool
	HasTrendData         bool
	HasInsights          bool
	HasReliableTrend     bool
	CycleDataStale       bool
	CompletedCycleCount  int
	InsightProgress      int
}

func NewStatsService(days StatsDayReader, symptoms StatsSymptomReader) *StatsService {
	return &StatsService{
		days:     days,
		symptoms: symptoms,
	}
}

func (service *StatsService) BuildCycleStatsForRange(user *models.User, from time.Time, to time.Time, now time.Time, location *time.Location) (CycleStats, []models.DailyLog, error) {
	logs, err := service.days.FetchLogsForUser(user.ID, from, to, location)
	if err != nil {
		return CycleStats{}, nil, err
	}

	stats := BuildCycleStats(logs, now)
	stats = ApplyUserCycleBaseline(user, logs, stats, now, location)
	return stats, logs, nil
}

func StatsOverviewRange(now time.Time) (time.Time, time.Time) {
	return now.AddDate(-statsOverviewWindowYears, 0, 0), now
}

func (service *StatsService) BuildOverviewStats(user *models.User, now time.Time, location *time.Location) (CycleStats, error) {
	from, to := StatsOverviewRange(now)
	stats, _, err := service.BuildCycleStatsForRange(user, from, to, now, location)
	if err != nil {
		return CycleStats{}, err
	}
	return stats, nil
}

func TrimTrailingCycleTrendLengths(lengths []int, maxPoints int) []int {
	if maxPoints <= 0 || len(lengths) <= maxPoints {
		return lengths
	}
	return lengths[len(lengths)-maxPoints:]
}

func OwnerBaselineCycleLength(user *models.User) int {
	if !IsOwnerUser(user) || !IsValidOnboardingCycleLength(user.CycleLength) {
		return 0
	}
	return user.CycleLength
}

func (service *StatsService) BuildTrend(user *models.User, logs []models.DailyLog, now time.Time, location *time.Location, maxTrendPoints int) ([]int, int) {
	lengths := CompletedCycleTrendLengths(logs, now, location)
	lengths = TrimTrailingCycleTrendLengths(lengths, maxTrendPoints)
	if len(lengths) == 0 {
		return lengths, 0
	}
	return lengths, int(averageInts(lengths) + 0.5)
}

func (service *StatsService) BuildFlags(user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location, trendPointCount int) StatsFlags {
	observedCycleCount := len(CycleLengths(logs))
	completedCycleCount := len(CompletedCycleTrendLengths(logs, now, location))
	today := DateAtLocation(now, location)
	cycleDayReference := DashboardCycleReferenceLength(user, stats)
	cycleStaleAnchor := DashboardCycleStaleAnchor(user, stats, location)

	return StatsFlags{
		HasObservedCycleData: observedCycleCount > 0,
		HasTrendData:         trendPointCount > 0,
		HasInsights:          completedCycleCount >= statsMinimumInsightsCycles,
		HasReliableTrend:     trendPointCount >= statsReliableTrendCycles,
		CycleDataStale:       DashboardCycleDataLooksStale(cycleStaleAnchor, today, cycleDayReference),
		CompletedCycleCount:  completedCycleCount,
		InsightProgress:      statsInsightProgress(completedCycleCount),
	}
}

func statsInsightProgress(completedCycleCount int) int {
	if completedCycleCount <= 0 {
		return 0
	}

	progress := completedCycleCount * 100 / statsMinimumInsightsCycles
	if progress > 100 {
		return 100
	}
	return progress
}

func (service *StatsService) BuildSymptomFrequenciesForUser(user *models.User) ([]SymptomFrequency, error) {
	if !IsOwnerUser(user) {
		return []SymptomFrequency{}, nil
	}

	logs, err := service.days.FetchAllLogsForUser(user.ID)
	if err != nil {
		return nil, err
	}

	return service.symptoms.CalculateFrequencies(user.ID, logs)
}
