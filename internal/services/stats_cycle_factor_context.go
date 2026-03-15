package services

import (
	"sort"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

const (
	statsCycleFactorContextWindowDays = 90
	statsCycleFactorContextLimit      = 3
)

type StatsCycleFactorContextItem struct {
	Key   string
	Count int
}

func StatsCycleFactorContextWindowDays() int {
	return statsCycleFactorContextWindowDays
}

func buildStatsCycleFactorContext(user *models.User, logs []models.DailyLog, stats CycleStats, flags StatsFlags, now time.Time, location *time.Location) ([]StatsCycleFactorContextItem, bool) {
	if !shouldBuildStatsCycleFactorContext(user, logs, stats, flags) {
		return nil, false
	}
	items := buildStatsCycleFactorItems(collectStatsCycleFactorCounts(logs, now, location))
	return items, len(items) > 0
}

func shouldBuildStatsCycleFactorContext(user *models.User, logs []models.DailyLog, stats CycleStats, flags StatsFlags) bool {
	if !IsOwnerUser(user) || len(logs) == 0 || isStatsPredictionDisabled(user) || flags.CompletedCycleCount < statsMinimumInsightsCycles {
		return false
	}
	return user.IrregularCycle || (flags.CompletedCycleCount >= minimumPhaseInsightCycles && IsIrregularCycleSpread(stats))
}

func collectStatsCycleFactorCounts(logs []models.DailyLog, now time.Time, location *time.Location) map[string]int {
	today := DateAtLocation(now, location)
	windowStart := today.AddDate(0, 0, -(statsCycleFactorContextWindowDays - 1))
	counts := make(map[string]int, len(supportedDayCycleFactorKeys))
	for _, logEntry := range logs {
		logDay := DateAtLocation(logEntry.Date, location)
		if logDay.Before(windowStart) || logDay.After(today) {
			continue
		}
		for _, key := range normalizedStatsCycleFactorKeys(logEntry.CycleFactorKeys) {
			counts[key]++
		}
	}
	return counts
}

func normalizedStatsCycleFactorKeys(keys []string) []string {
	normalized, _ := NormalizeDayCycleFactorKeys(keys)
	return normalized
}

func buildStatsCycleFactorItems(counts map[string]int) []StatsCycleFactorContextItem {
	items := nonZeroStatsCycleFactorItems(counts)
	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return statsCycleFactorOrderIndex(items[i].Key) < statsCycleFactorOrderIndex(items[j].Key)
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > statsCycleFactorContextLimit {
		return items[:statsCycleFactorContextLimit]
	}
	return items
}

func nonZeroStatsCycleFactorItems(counts map[string]int) []StatsCycleFactorContextItem {
	items := make([]StatsCycleFactorContextItem, 0, len(counts))
	for key, count := range counts {
		if count <= 0 {
			continue
		}
		items = append(items, StatsCycleFactorContextItem{Key: key, Count: count})
	}
	return items
}

func statsCycleFactorOrderIndex(key string) int {
	for index, supportedKey := range supportedDayCycleFactorKeys {
		if supportedKey == key {
			return index
		}
	}
	return len(supportedDayCycleFactorKeys)
}
