package services

import (
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
	AveragePeriodLength  float64   `json:"average_period_length"`
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
)

func BuildCycleStats(logs []models.DailyLog, now time.Time) CycleStats {
	stats := CycleStats{CurrentPhase: "unknown"}
	if len(logs) == 0 {
		return stats
	}

	sorted := sortDailyLogs(logs)
	starts := DetectCycleStarts(sorted)
	if len(starts) == 0 {
		return stats
	}

	cycles := buildCycles(starts, sorted)
	populateObservedCycleStats(&stats, cycleLengths(starts), cycles)
	stats.LastPeriodStart = starts[len(starts)-1]
	applyPredictedCycleStats(&stats)

	today := dateOnly(now)
	stats.CurrentCycleDay = cycleDayAt(stats.LastPeriodStart, today)
	stats.CurrentPhase = detectCyclePhase(stats, sorted, today)
	return stats
}

// PredictCycleWindow returns ovulation date and fertility window for the cycle
// that starts at periodStart.
// Invariants:
// - ovulation is strictly after period end and before next period start
// - fertility window never overlaps period days
// - if the clamped fertility range becomes empty, it is suppressed
func CalcOvulationDay(cycleLen, periodLen int) (int, bool) {
	remaining := cycleLen - periodLen
	if remaining < 8 {
		return 0, false
	}
	if remaining < 15 {
		return periodLen + 1, false
	}

	ovDay := cycleLen - 14
	if ovDay <= periodLen {
		ovDay = periodLen + 1
	}
	if ovDay >= cycleLen {
		ovDay = cycleLen - 1
	}
	return ovDay, true
}

func PredictCycleWindow(periodStart time.Time, cycleLength int, periodLength int) (time.Time, time.Time, time.Time, bool, bool) {
	if periodStart.IsZero() || cycleLength <= 0 {
		return time.Time{}, time.Time{}, time.Time{}, false, false
	}
	if periodLength <= 0 {
		periodLength = models.DefaultPeriodLength
	}
	ovulationDay, ovulationExact := CalcOvulationDay(cycleLength, periodLength)
	if ovulationDay <= 0 {
		return time.Time{}, time.Time{}, time.Time{}, false, false
	}

	nextPeriodStart := dateOnly(periodStart.AddDate(0, 0, cycleLength))
	periodEnd := dateOnly(periodStart.AddDate(0, 0, periodLength-1))
	firstNonPeriodDay := dateOnly(periodEnd.AddDate(0, 0, 1))
	lastPrePeriodDay := dateOnly(nextPeriodStart.AddDate(0, 0, -1))

	ovulationDate := dateOnly(periodStart.AddDate(0, 0, ovulationDay-1))
	if !ovulationDate.Before(nextPeriodStart) {
		ovulationDate = lastPrePeriodDay
	}
	if !ovulationDate.After(periodEnd) {
		return time.Time{}, time.Time{}, time.Time{}, false, false
	}

	fertilityStart := dateOnly(ovulationDate.AddDate(0, 0, -5))
	fertilityEnd := dateOnly(ovulationDate.AddDate(0, 0, 1))
	if !fertilityStart.After(periodEnd) {
		fertilityStart = firstNonPeriodDay
	}
	if !fertilityEnd.Before(nextPeriodStart) {
		fertilityEnd = lastPrePeriodDay
	}
	if fertilityStart.After(fertilityEnd) {
		return ovulationDate, time.Time{}, time.Time{}, ovulationExact, true
	}

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

func sortDailyLogs(logs []models.DailyLog) []models.DailyLog {
	sorted := make([]models.DailyLog, 0, len(logs))
	sorted = append(sorted, logs...)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date.Before(sorted[j].Date)
	})
	return sorted
}

func populateObservedCycleStats(stats *CycleStats, lengths []int, cycles []detectedCycle) {
	recentLengths := tailInts(lengths, cyclePredictionWindow)
	if len(recentLengths) > 0 {
		stats.AverageCycleLength = averageInts(recentLengths)
		stats.MedianCycleLength = medianInt(recentLengths)
	}
	if len(lengths) > 0 {
		stats.MinCycleLength, stats.MaxCycleLength = minMaxInts(lengths)
	}

	periodLengths := recentPositivePeriodLengths(cycles, cyclePredictionWindow)
	if len(periodLengths) > 0 {
		stats.AveragePeriodLength = averageInts(periodLengths)
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
	predictedPeriodLength := predictedPeriodLength(stats.AveragePeriodLength)

	stats.NextPeriodStart = dateOnly(stats.LastPeriodStart.AddDate(0, 0, predictionCycleLength))
	ovulationDate, fertilityWindowStart, fertilityWindowEnd, ovulationExact, ovulationCalculable := PredictCycleWindow(
		stats.LastPeriodStart,
		predictionCycleLength,
		predictedPeriodLength,
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
