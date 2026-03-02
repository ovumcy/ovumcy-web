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
}

type StatsService struct {
	days     StatsDayReader
	symptoms StatsSymptomReader
}

const statsOverviewWindowYears = 2

type StatsFlags struct {
	HasObservedCycleData bool
	HasTrendData         bool
	HasReliableTrend     bool
	CycleDataStale       bool
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
	return lengths, OwnerBaselineCycleLength(user)
}

func (service *StatsService) BuildFlags(user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location, trendPointCount int) StatsFlags {
	observedCycleCount := len(CycleLengths(logs))
	today := DateAtLocation(now, location)
	cycleDayReference := DashboardCycleReferenceLength(user, stats)
	cycleStaleAnchor := DashboardCycleStaleAnchor(user, stats, location)

	return StatsFlags{
		HasObservedCycleData: observedCycleCount > 0,
		HasTrendData:         trendPointCount > 0,
		HasReliableTrend:     trendPointCount >= 3,
		CycleDataStale:       DashboardCycleDataLooksStale(cycleStaleAnchor, today, cycleDayReference),
	}
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
