package services

import (
	"math"
	"sort"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const (
	statsCycleFactorContextWindowDays = 90
	statsCycleFactorContextLimit      = 3
	statsCycleFactorHintLimit         = 2
	statsCycleFactorPatternItemLimit  = 2
	statsCycleFactorRecentCycleLimit  = 3
	statsCycleFactorComparisonDelta   = 2
)

type StatsCycleFactorContextItem struct {
	Key   string
	Count int
}

type StatsCycleFactorPatternSummary struct {
	Kind  string
	Items []StatsCycleFactorContextItem
}

type StatsCycleFactorRecentCycleSummary struct {
	Start          time.Time
	End            time.Time
	CycleLength    int
	ComparisonKind string
	FactorKeys     []string
}

type StatsCycleFactorExplanation struct {
	RecentFactors    []StatsCycleFactorContextItem
	PatternSummaries []StatsCycleFactorPatternSummary
	RecentCycles     []StatsCycleFactorRecentCycleSummary
	HintFactorKeys   []string
}

type statsCycleFactorCycleSnapshot struct {
	Start          time.Time
	End            time.Time
	CycleLength    int
	ComparisonKind string
	FactorKeys     []string
}

func StatsCycleFactorContextWindowDays() int {
	return statsCycleFactorContextWindowDays
}

func buildStatsCycleFactorExplanation(user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time, location *time.Location) (StatsCycleFactorExplanation, bool) {
	completedCycles := buildCompletedCycleSpans(logs, location)
	if !shouldBuildStatsCycleFactorExplanation(user, logs, completedCycles, stats) {
		return StatsCycleFactorExplanation{}, false
	}

	recentFactors := buildStatsCycleFactorItems(collectStatsCycleFactorCounts(logs, now, location))
	snapshots := buildStatsCycleFactorSnapshots(logs, completedCycles, stats, location)
	explanation := StatsCycleFactorExplanation{
		RecentFactors:    recentFactors,
		PatternSummaries: buildStatsCycleFactorPatternSummaries(snapshots),
		RecentCycles:     buildStatsCycleFactorRecentCycles(snapshots),
		HintFactorKeys:   buildStatsCycleFactorHintKeys(recentFactors),
	}
	return explanation, hasStatsCycleFactorExplanation(explanation)
}

func shouldBuildStatsCycleFactorExplanation(user *models.User, logs []models.DailyLog, completedCycles []completedCycleSpan, stats CycleStats) bool {
	if !IsOwnerUser(user) || len(logs) == 0 || isStatsPredictionDisabled(user) || len(completedCycles) < statsMinimumInsightsCycles {
		return false
	}
	return user.IrregularCycle || (len(completedCycles) >= minimumPhaseInsightCycles && IsIrregularCycleSpread(stats))
}

func collectStatsCycleFactorCounts(logs []models.DailyLog, now time.Time, location *time.Location) map[string]int {
	today := DateAtLocation(now, location)
	windowStart := today.AddDate(0, 0, -(statsCycleFactorContextWindowDays - 1))
	counts := make(map[string]int, len(supportedDayCycleFactorKeys))
	for _, logEntry := range logs {
		logDay := CalendarDay(logEntry.Date, location)
		if logDay.Before(windowStart) || logDay.After(today) {
			continue
		}
		for _, key := range normalizedStatsCycleFactorKeys(logEntry.CycleFactorKeys) {
			counts[key]++
		}
	}
	return counts
}

func buildStatsCycleFactorSnapshots(logs []models.DailyLog, completedCycles []completedCycleSpan, stats CycleStats, location *time.Location) []statsCycleFactorCycleSnapshot {
	snapshots := make([]statsCycleFactorCycleSnapshot, 0, len(completedCycles))
	for _, cycle := range completedCycles {
		factorKeys := collectCycleFactorKeysForSpan(logs, cycle, location)
		if len(factorKeys) == 0 {
			continue
		}
		snapshots = append(snapshots, statsCycleFactorCycleSnapshot{
			Start:          cycle.Start,
			End:            cycle.NextStart.AddDate(0, 0, -1),
			CycleLength:    cycle.CycleLength,
			ComparisonKind: classifyStatsCycleFactorComparison(stats, cycle.CycleLength),
			FactorKeys:     factorKeys,
		})
	}
	return snapshots
}

func collectCycleFactorKeysForSpan(logs []models.DailyLog, cycle completedCycleSpan, location *time.Location) []string {
	counts := make(map[string]int, len(supportedDayCycleFactorKeys))
	for _, logEntry := range logs {
		localDay := CalendarDay(logEntry.Date, location)
		if localDay.Before(cycle.Start) || !localDay.Before(cycle.NextStart) {
			continue
		}
		for _, key := range normalizedStatsCycleFactorKeys(logEntry.CycleFactorKeys) {
			counts[key]++
		}
	}
	return factorKeysByStableOrder(counts)
}

func factorKeysByStableOrder(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for _, key := range supportedDayCycleFactorKeys {
		if counts[key] <= 0 {
			continue
		}
		keys = append(keys, key)
	}
	return keys
}

func classifyStatsCycleFactorComparison(stats CycleStats, cycleLength int) string {
	baseline := stats.MedianCycleLength
	if baseline <= 0 {
		baseline = int(math.Round(stats.AverageCycleLength))
	}
	switch {
	case baseline > 0 && cycleLength <= baseline-statsCycleFactorComparisonDelta:
		return "shorter"
	case baseline > 0 && cycleLength >= baseline+statsCycleFactorComparisonDelta:
		return "longer"
	default:
		return "variable"
	}
}

func normalizedStatsCycleFactorKeys(keys []string) []string {
	normalized, _ := NormalizeDayCycleFactorKeys(keys)
	return normalized
}

func buildStatsCycleFactorPatternSummaries(snapshots []statsCycleFactorCycleSnapshot) []StatsCycleFactorPatternSummary {
	bucketCounts := make(map[string]map[string]int, len(statsCycleFactorPatternOrder))
	for _, snapshot := range snapshots {
		counts := bucketCounts[snapshot.ComparisonKind]
		if counts == nil {
			counts = make(map[string]int, len(snapshot.FactorKeys))
			bucketCounts[snapshot.ComparisonKind] = counts
		}
		for _, key := range snapshot.FactorKeys {
			counts[key]++
		}
	}

	summaries := make([]StatsCycleFactorPatternSummary, 0, len(bucketCounts))
	for _, kind := range statsCycleFactorPatternOrder {
		items := buildStatsCycleFactorItems(bucketCounts[kind])
		if len(items) == 0 {
			continue
		}
		if len(items) > statsCycleFactorPatternItemLimit {
			items = items[:statsCycleFactorPatternItemLimit]
		}
		summaries = append(summaries, StatsCycleFactorPatternSummary{
			Kind:  kind,
			Items: items,
		})
	}
	return summaries
}

var statsCycleFactorPatternOrder = []string{"longer", "shorter", "variable"}

func buildStatsCycleFactorRecentCycles(snapshots []statsCycleFactorCycleSnapshot) []StatsCycleFactorRecentCycleSummary {
	if len(snapshots) == 0 {
		return nil
	}

	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].Start.After(snapshots[j].Start)
	})
	if len(snapshots) > statsCycleFactorRecentCycleLimit {
		snapshots = snapshots[:statsCycleFactorRecentCycleLimit]
	}

	summaries := make([]StatsCycleFactorRecentCycleSummary, 0, len(snapshots))
	for _, snapshot := range snapshots {
		summaries = append(summaries, StatsCycleFactorRecentCycleSummary{
			Start:          snapshot.Start,
			End:            snapshot.End,
			CycleLength:    snapshot.CycleLength,
			ComparisonKind: snapshot.ComparisonKind,
			FactorKeys:     append([]string{}, snapshot.FactorKeys...),
		})
	}
	return summaries
}

func buildStatsCycleFactorHintKeys(items []StatsCycleFactorContextItem) []string {
	limit := statsCycleFactorHintLimit
	if len(items) < limit {
		limit = len(items)
	}
	keys := make([]string, 0, limit)
	for index := 0; index < limit; index++ {
		keys = append(keys, items[index].Key)
	}
	return keys
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

func hasStatsCycleFactorExplanation(explanation StatsCycleFactorExplanation) bool {
	return len(explanation.RecentFactors) > 0 ||
		len(explanation.PatternSummaries) > 0 ||
		len(explanation.RecentCycles) > 0 ||
		len(explanation.HintFactorKeys) > 0
}
