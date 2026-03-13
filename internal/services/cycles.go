package services

import (
	"math"
	"sort"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type CycleStats struct {
	CurrentCycleDay      int       `json:"current_cycle_day"`
	CurrentPhase         string    `json:"current_phase"`
	AverageCycleLength   float64   `json:"average_cycle_length"`
	MedianCycleLength    int       `json:"median_cycle_length"`
	MinCycleLength       int       `json:"min_cycle_length"`
	MaxCycleLength       int       `json:"max_cycle_length"`
	CycleLengthStdDev    float64   `json:"cycle_length_std_dev"`
	CompletedCycleCount  int       `json:"completed_cycle_count"`
	AveragePeriodLength  float64   `json:"average_period_length"`
	LastCycleLength      int       `json:"last_cycle_length"`
	LastPeriodLength     int       `json:"last_period_length"`
	LutealPhase          int       `json:"luteal_phase"`
	LastPeriodStart      time.Time `json:"last_period_start"`
	NextPeriodStart      time.Time `json:"next_period_start"`
	OvulationDate        time.Time `json:"ovulation_date"`
	OvulationExact       bool      `json:"ovulation_exact"`
	OvulationImpossible  bool      `json:"ovulation_impossible"`
	FertilityWindowStart time.Time `json:"fertility_window_start"`
	FertilityWindowEnd   time.Time `json:"fertility_window_end"`
}

type detectedCycle struct {
	Start        time.Time
	End          time.Time
	PeriodLength int
}

const (
	cyclePredictionWindow      = 6
	irregularCycleSpreadDays   = 7
	irregularCycleFallbackSpan = 7
	defaultLutealPhaseDays     = 14
	minLutealPhaseDays         = 10
	minCycleReserveDays        = 10
)

func BuildCycleStats(logs []models.DailyLog, now time.Time) CycleStats {
	stats := CycleStats{CurrentPhase: "unknown"}
	today := dateOnly(now)
	sorted := sortDailyLogs(filterLogsNotAfter(logs, today))
	if len(sorted) == 0 {
		return stats
	}

	detectedStarts := DetectCycleStarts(sorted)
	if len(detectedStarts) == 0 {
		return stats
	}

	observedStarts := ObservedCycleStarts(sorted)
	if len(observedStarts) == 0 {
		observedStarts = detectedStarts
	}

	cycles := buildCycles(observedStarts, sorted)
	populateObservedCycleStats(&stats, cycleLengths(observedStarts), cycles)
	stats.LastPeriodStart = detectedStarts[len(detectedStarts)-1]
	stats.LutealPhase = defaultLutealPhaseDays
	applyPredictedCycleStats(&stats)

	stats.CurrentCycleDay = cycleDayAt(stats.LastPeriodStart, today)
	stats.CurrentPhase = detectCyclePhase(stats, sorted, today)
	return stats
}

// PredictCycleWindow returns ovulation date and fertility window for the cycle
// that starts at periodStart.
// Invariants:
// - ovulation is strictly before next period start
// - fertility window is the 6-day range [ovulation-5, ovulation]
// - fertility window may overlap menstruation on short cycles
func ResolveLutealPhase(value int) int {
	switch {
	case value <= 0:
		return defaultLutealPhaseDays
	case value < minLutealPhaseDays:
		return minLutealPhaseDays
	default:
		return value
	}
}

func CalcOvulationDay(cycleLen, lutealPhase int) (int, bool) {
	resolvedLutealPhase := ResolveLutealPhase(lutealPhase)
	if cycleLen <= resolvedLutealPhase {
		return 0, false
	}

	ovDay := cycleLen - resolvedLutealPhase
	if ovDay < 1 {
		return 0, false
	}
	return ovDay, true
}

func PredictCycleWindow(periodStart time.Time, cycleLength int, lutealPhase int) (time.Time, time.Time, time.Time, bool, bool) {
	if periodStart.IsZero() || cycleLength <= 0 {
		return time.Time{}, time.Time{}, time.Time{}, false, false
	}
	ovulationDay, ovulationExact := CalcOvulationDay(cycleLength, lutealPhase)
	if ovulationDay <= 0 {
		return time.Time{}, time.Time{}, time.Time{}, false, false
	}

	nextPeriodStart := dateOnly(periodStart.AddDate(0, 0, cycleLength))
	ovulationDate := dateOnly(periodStart.AddDate(0, 0, ovulationDay-1))
	if !ovulationDate.Before(nextPeriodStart) {
		return time.Time{}, time.Time{}, time.Time{}, false, false
	}

	fertilityStart := dateOnly(ovulationDate.AddDate(0, 0, -5))
	if fertilityStart.Before(periodStart) {
		fertilityStart = dateOnly(periodStart)
	}
	fertilityEnd := ovulationDate

	return ovulationDate, fertilityStart, fertilityEnd, ovulationExact, true
}

func DetectCycleStarts(logs []models.DailyLog) []time.Time {
	if len(logs) == 0 {
		return nil
	}

	sorted := sortDailyLogs(logs)
	starts := make([]time.Time, 0)
	var previousPeriodDay time.Time

	for _, log := range sorted {
		day := dateOnly(log.Date)
		if !log.IsPeriod {
			continue
		}

		if previousPeriodDay.IsZero() {
			starts = append(starts, day)
			previousPeriodDay = day
			continue
		}

		gapDays := int(day.Sub(previousPeriodDay).Hours()/24) - 1
		if gapDays >= 5 {
			starts = append(starts, day)
		}
		previousPeriodDay = day
	}

	return starts
}

type periodCluster struct {
	Start                time.Time
	End                  time.Time
	ExplicitStart        time.Time
	HasUncertainExplicit bool
}

func ObservedCycleStarts(logs []models.DailyLog) []time.Time {
	clusters := buildPeriodClusters(logs)
	if len(clusters) == 0 {
		return nil
	}

	starts := make([]time.Time, 0, len(clusters))
	for _, cluster := range clusters {
		switch {
		case !cluster.ExplicitStart.IsZero():
			starts = append(starts, cluster.ExplicitStart)
		case cluster.HasUncertainExplicit:
			continue
		default:
			starts = append(starts, cluster.Start)
		}
	}
	return starts
}

func DetectExplicitCycleStarts(logs []models.DailyLog) []time.Time {
	if len(logs) == 0 {
		return nil
	}

	sorted := sortDailyLogs(logs)
	starts := make([]time.Time, 0)
	seen := make(map[string]struct{}, len(sorted))
	for _, logEntry := range sorted {
		if !logEntry.IsPeriod || !logEntry.CycleStart {
			continue
		}

		day := dateOnly(logEntry.Date)
		key := day.Format("2006-01-02")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		starts = append(starts, day)
	}
	return starts
}

func buildPeriodClusters(logs []models.DailyLog) []periodCluster {
	if len(logs) == 0 {
		return nil
	}

	sorted := sortDailyLogs(logs)
	clusters := make([]periodCluster, 0)
	for _, log := range sorted {
		if !log.IsPeriod {
			continue
		}

		day := dateOnly(log.Date)
		if len(clusters) == 0 {
			clusters = append(clusters, periodCluster{Start: day, End: day})
		} else {
			lastIndex := len(clusters) - 1
			gapDays := int(day.Sub(clusters[lastIndex].End).Hours()/24) - 1
			if gapDays >= 5 {
				clusters = append(clusters, periodCluster{Start: day, End: day})
			} else if day.After(clusters[lastIndex].End) {
				clusters[lastIndex].End = day
			}
		}

		cluster := &clusters[len(clusters)-1]
		if !log.CycleStart {
			continue
		}
		if log.IsUncertain {
			cluster.HasUncertainExplicit = true
			continue
		}
		if cluster.ExplicitStart.IsZero() || day.Before(cluster.ExplicitStart) {
			cluster.ExplicitStart = day
		}
	}

	return clusters
}

func sortDailyLogs(logs []models.DailyLog) []models.DailyLog {
	sorted := make([]models.DailyLog, 0, len(logs))
	sorted = append(sorted, logs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date.Before(sorted[j].Date)
	})
	return sorted
}

func populateObservedCycleStats(stats *CycleStats, lengths []int, cycles []detectedCycle) {
	stats.CompletedCycleCount = len(lengths)
	recentLengths := tailInts(lengths, cyclePredictionWindow)
	if len(recentLengths) > 0 {
		stats.AverageCycleLength = averageInts(recentLengths)
		stats.MedianCycleLength = medianInt(recentLengths)
	}
	if len(lengths) > 0 {
		stats.MinCycleLength, stats.MaxCycleLength = minMaxInts(lengths)
		stats.CycleLengthStdDev = stddevInts(lengths)
		stats.LastCycleLength = lengths[len(lengths)-1]
	}

	periodLengths := recentPositivePeriodLengths(cycles, cyclePredictionWindow)
	if len(periodLengths) > 0 {
		stats.AveragePeriodLength = averageInts(periodLengths)
	}
	completedCycleCount := len(lengths)
	if completedCycleCount > 0 && len(cycles) >= completedCycleCount {
		stats.LastPeriodLength = cycles[completedCycleCount-1].PeriodLength
	}
}

func recentPositivePeriodLengths(cycles []detectedCycle, limit int) []int {
	periodLengths := make([]int, 0, len(cycles))
	for _, cycle := range tailCycles(cycles, limit) {
		if cycle.PeriodLength > 0 {
			periodLengths = append(periodLengths, cycle.PeriodLength)
		}
	}
	return periodLengths
}

func applyPredictedCycleStats(stats *CycleStats) {
	predictionCycleLength := predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	if stats.LutealPhase <= 0 {
		stats.LutealPhase = defaultLutealPhaseDays
	}

	stats.NextPeriodStart = dateOnly(stats.LastPeriodStart.AddDate(0, 0, predictionCycleLength))
	ovulationDate, fertilityWindowStart, fertilityWindowEnd, ovulationExact, ovulationCalculable := PredictCycleWindow(
		stats.LastPeriodStart,
		predictionCycleLength,
		stats.LutealPhase,
	)
	if !ovulationCalculable {
		clearPredictedCycleWindow(stats)
		return
	}

	stats.OvulationDate = ovulationDate
	stats.OvulationExact = ovulationExact
	stats.OvulationImpossible = false
	stats.FertilityWindowStart = fertilityWindowStart
	stats.FertilityWindowEnd = fertilityWindowEnd
}

func predictedCycleLength(median int, average float64) int {
	if average > 0 {
		return int(average + 0.5)
	}
	if median > 0 {
		return median
	}
	return models.DefaultCycleLength
}

func predictedPeriodLength(average float64) int {
	length := int(average + 0.5)
	if length > 0 {
		return length
	}
	return models.DefaultPeriodLength
}

func clearPredictedCycleWindow(stats *CycleStats) {
	stats.OvulationDate = time.Time{}
	stats.OvulationExact = false
	stats.OvulationImpossible = true
	stats.FertilityWindowStart = time.Time{}
	stats.FertilityWindowEnd = time.Time{}
}

func cycleDayAt(lastPeriodStart time.Time, today time.Time) int {
	if today.Before(lastPeriodStart) {
		return 0
	}
	return int(today.Sub(lastPeriodStart).Hours()/24) + 1
}

func detectCyclePhase(stats CycleStats, logs []models.DailyLog, today time.Time) string {
	if periodLoggedOnDay(logs, today) {
		return "menstrual"
	}
	if stats.OvulationImpossible || stats.OvulationDate.IsZero() {
		return "unknown"
	}
	if betweenInclusive(today, stats.FertilityWindowStart, stats.FertilityWindowEnd) {
		if sameDay(today, stats.OvulationDate) {
			return "ovulation"
		}
		return "fertile"
	}
	if today.Before(stats.OvulationDate) {
		return "follicular"
	}
	return "luteal"
}

func periodLoggedOnDay(logs []models.DailyLog, day time.Time) bool {
	dayKey := day.Format("2006-01-02")
	for _, log := range logs {
		if log.IsPeriod && dateOnly(log.Date).Format("2006-01-02") == dayKey {
			return true
		}
	}
	return false
}

func CycleLengths(logs []models.DailyLog) []int {
	starts := DetectCycleStarts(logs)
	return cycleLengths(starts)
}

func buildCycles(starts []time.Time, logs []models.DailyLog) []detectedCycle {
	if len(starts) == 0 {
		return nil
	}

	isPeriodByDate := make(map[string]bool, len(logs))
	for _, log := range logs {
		day := dateOnly(log.Date).Format("2006-01-02")
		isPeriodByDate[day] = log.IsPeriod
	}

	cycles := make([]detectedCycle, 0, len(starts))
	for i, start := range starts {
		end := start
		if i+1 < len(starts) {
			end = starts[i+1].AddDate(0, 0, -1)
		}

		periodLength := 0
		for day := start; !day.After(start.AddDate(0, 0, 10)); day = day.AddDate(0, 0, 1) {
			if !isPeriodByDate[day.Format("2006-01-02")] {
				break
			}
			periodLength++
		}

		cycles = append(cycles, detectedCycle{
			Start:        start,
			End:          end,
			PeriodLength: periodLength,
		})
	}

	return cycles
}

func cycleLengths(starts []time.Time) []int {
	if len(starts) < 2 {
		return nil
	}

	lengths := make([]int, 0, len(starts)-1)
	for i := 1; i < len(starts); i++ {
		lengths = append(lengths, int(starts[i].Sub(starts[i-1]).Hours()/24))
	}
	return lengths
}

func tailInts(values []int, n int) []int {
	if len(values) <= n {
		return values
	}
	return values[len(values)-n:]
}

func tailCycles(values []detectedCycle, n int) []detectedCycle {
	if len(values) <= n {
		return values
	}
	return values[len(values)-n:]
}

func averageInts(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	var total int
	for _, value := range values {
		total += value
	}
	return float64(total) / float64(len(values))
}

func minMaxInts(values []int) (int, int) {
	if len(values) == 0 {
		return 0, 0
	}

	minValue := values[0]
	maxValue := values[0]
	for _, value := range values[1:] {
		if value < minValue {
			minValue = value
		}
		if value > maxValue {
			maxValue = value
		}
	}
	return minValue, maxValue
}

func CycleLengthSpread(stats CycleStats) int {
	if stats.MinCycleLength <= 0 || stats.MaxCycleLength <= 0 || stats.MaxCycleLength < stats.MinCycleLength {
		return 0
	}
	return stats.MaxCycleLength - stats.MinCycleLength
}

func IsIrregularCycleSpread(stats CycleStats) bool {
	return CycleLengthSpread(stats) > irregularCycleSpreadDays
}

func medianInt(values []int) int {
	if len(values) == 0 {
		return 0
	}

	sorted := make([]int, 0, len(values))
	sorted = append(sorted, values...)
	sort.Ints(sorted)

	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}

	left := sorted[mid-1]
	right := sorted[mid]
	return int(float64(left+right)/2 + 0.5)
}

func betweenInclusive(day, start, end time.Time) bool {
	if start.IsZero() || end.IsZero() {
		return false
	}
	return (day.Equal(start) || day.After(start)) && (day.Equal(end) || day.Before(end))
}

func sameDay(a, b time.Time) bool {
	return a.Format("2006-01-02") == b.Format("2006-01-02")
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func filterLogsNotAfter(logs []models.DailyLog, cutoff time.Time) []models.DailyLog {
	if len(logs) == 0 || cutoff.IsZero() {
		return logs
	}

	filtered := make([]models.DailyLog, 0, len(logs))
	for _, log := range logs {
		if dateOnly(log.Date).After(cutoff) {
			continue
		}
		filtered = append(filtered, log)
	}
	return filtered
}

func stddevInts(values []int) float64 {
	if len(values) == 0 {
		return 0
	}

	mean := averageInts(values)
	var squared float64
	for _, value := range values {
		diff := float64(value) - mean
		squared += diff * diff
	}
	return math.Sqrt(squared / float64(len(values)))
}
