package services

import (
	"math"
	"sort"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
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
	PregnancyPaused      bool      `json:"pregnancy_paused"`
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
	minOvulationCycleDay       = 5
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

// ResolveLutealPhase clamps a raw luteal phase length to the supported range,
// falling back to defaultLutealPhaseDays when value is unset or non-positive.
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

// CalcOvulationDay returns the one-based ovulation day within the cycle where
// periodStart is cycle day 1. Example: a 28-day cycle with a 14-day luteal
// phase predicts ovulation on cycle day 14, so a cycle that starts on
// March 10, 2026 maps to March 23, 2026.
func CalcOvulationDay(cycleLen, lutealPhase int) (int, bool) {
	if cycleLen < minLutealPhaseDays+minOvulationCycleDay {
		return 0, false
	}

	resolvedLutealPhase := ResolveLutealPhase(lutealPhase)
	ovulationExact := true
	maxSupportedLutealPhase := cycleLen - minOvulationCycleDay
	if maxSupportedLutealPhase < minLutealPhaseDays {
		return 0, false
	}
	if resolvedLutealPhase > maxSupportedLutealPhase {
		resolvedLutealPhase = maxSupportedLutealPhase
		ovulationExact = false
	}

	ovDay := cycleLen - resolvedLutealPhase
	if ovDay < minOvulationCycleDay {
		return 0, false
	}
	return ovDay, ovulationExact
}

// CycleWindowPrediction is the named-field result of PredictCycleWindow.
// Calculable reports whether a window could be predicted at all; when it is
// false every other field holds its zero value. OvulationExact distinguishes
// an exact luteal-phase fit from a clamped estimate.
type CycleWindowPrediction struct {
	OvulationDate        time.Time
	FertilityWindowStart time.Time
	FertilityWindowEnd   time.Time
	OvulationExact       bool
	Calculable           bool
}

// PredictCycleWindow returns ovulation date and fertility window for the cycle
// that starts at periodStart.
// Invariants:
// - ovulation is strictly before next period start
// - fertility window is the 6-day range [ovulation-5, ovulation]
// - fertility window may overlap menstruation on short cycles
func PredictCycleWindow(periodStart time.Time, cycleLength int, lutealPhase int) CycleWindowPrediction {
	if periodStart.IsZero() || cycleLength <= 0 {
		return CycleWindowPrediction{}
	}
	ovulationDay, ovulationExact := CalcOvulationDay(cycleLength, lutealPhase)
	if ovulationDay <= 0 {
		return CycleWindowPrediction{}
	}

	nextPeriodStart := dateOnly(periodStart.AddDate(0, 0, cycleLength))
	// ovulationDay is one-based relative to periodStart (cycle day 1).
	ovulationDate := dateOnly(periodStart.AddDate(0, 0, ovulationDay-1))
	if !ovulationDate.Before(nextPeriodStart) {
		// codecov:ignore -- defensive invariant: CalcOvulationDay caps ovulationDay at
		// cycleLen-minLutealPhaseDays, so ovulationDate is always strictly before nextPeriodStart.
		return CycleWindowPrediction{}
	}

	fertilityStart := dateOnly(ovulationDate.AddDate(0, 0, -5))
	if fertilityStart.Before(periodStart) {
		fertilityStart = dateOnly(periodStart)
	}

	return CycleWindowPrediction{
		OvulationDate:        ovulationDate,
		FertilityWindowStart: fertilityStart,
		FertilityWindowEnd:   ovulationDate,
		OvulationExact:       ovulationExact,
		Calculable:           true,
	}
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
	seen := make(map[time.Time]struct{}, len(sorted))
	for _, logEntry := range sorted {
		if !logEntry.IsPeriod || !logEntry.CycleStart {
			continue
		}

		day := dateOnly(logEntry.Date)
		if _, exists := seen[day]; exists {
			continue
		}
		seen[day] = struct{}{}
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
		// Range and spread statistics describe the same recent window the
		// median prediction uses: an outlier cycle that has aged out of the
		// window must not keep widening irregular prediction ranges or the
		// variability spread indefinitely.
		stats.MinCycleLength, stats.MaxCycleLength = minMaxInts(recentLengths)
		stats.CycleLengthStdDev = stddevInts(recentLengths)
		stats.LastCycleLength = recentLengths[len(recentLengths)-1]
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
	window := PredictCycleWindow(
		stats.LastPeriodStart,
		predictionCycleLength,
		stats.LutealPhase,
	)
	if !window.Calculable {
		clearPredictedCycleWindow(stats)
		return
	}

	stats.OvulationDate = window.OvulationDate
	stats.OvulationExact = window.OvulationExact
	stats.OvulationImpossible = false
	stats.FertilityWindowStart = window.FertilityWindowStart
	stats.FertilityWindowEnd = window.FertilityWindowEnd
}

func predictedCycleLength(median int, average float64) int {
	// Prefer the median (the statistic documented in docs/cycle-prediction.md):
	// it is robust to a single outlier cycle. A missed period log merges two
	// real cycles into one ~60-90 day gap that would drag the mean by ~10 days
	// and push every downstream prediction late, but leaves the median unmoved.
	// The mean is only a fallback for the degenerate case where no median is
	// available (it never is when at least one cycle length exists).
	if median > 0 {
		return median
	}
	if average > 0 {
		return int(average + 0.5)
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
	days := CalendarDaysBetween(lastPeriodStart, today)
	if days < 0 {
		return 0
	}
	return days + 1
}

// cyclePhaseOptions parameterizes resolveCyclePhase over the one point where
// the no-baseline and baseline-applied callers genuinely disagree: whether a
// day with no logged period entry can still count as menstrual because it
// falls inside a *projected* period window (LastPeriodStart +
// AveragePeriodLength). detectCyclePhase (no owner baseline) requires an
// actual logged entry; DetectCurrentPhase (after ApplyUserCycleBaseline)
// additionally honors the projection. Both callers rely on their current
// behavior (day_feedback_policy.go / cycle_start_policy.go read BuildCycleStats
// without a baseline; every owner-facing surface goes through
// ApplyUserCycleBaseline), so this stays an explicit option rather than being
// collapsed to one semantic.
type cyclePhaseOptions struct {
	location               *time.Location
	includeProjectedPeriod bool
}

func detectCyclePhase(stats CycleStats, logs []models.DailyLog, today time.Time) string {
	return resolveCyclePhase(stats, logs, today, cyclePhaseOptions{location: time.UTC})
}

func resolveCyclePhase(stats CycleStats, logs []models.DailyLog, today time.Time, opts cyclePhaseOptions) string {
	if periodLoggedOnDay(logs, today) {
		return "menstrual"
	}
	if opts.includeProjectedPeriod && !stats.LastPeriodStart.IsZero() {
		periodLength := int(stats.AveragePeriodLength + 0.5)
		if periodLength <= 0 {
			periodLength = models.DefaultPeriodLength
		}
		periodEnd := CalendarDay(stats.LastPeriodStart.AddDate(0, 0, periodLength-1), opts.location)
		if betweenInclusive(today, stats.LastPeriodStart, periodEnd) {
			return "menstrual"
		}
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
	dayKey := dateOnly(day)
	for _, log := range logs {
		if log.IsPeriod && dateOnly(log.Date).Equal(dayKey) {
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

	isPeriodByDate := make(map[time.Time]bool, len(logs))
	for _, log := range logs {
		isPeriodByDate[dateOnly(log.Date)] = log.IsPeriod
	}

	cycles := make([]detectedCycle, 0, len(starts))
	for i, start := range starts {
		end := start
		if i+1 < len(starts) {
			end = starts[i+1].AddDate(0, 0, -1)
		}

		periodLength := 0
		for day := start; !day.After(start.AddDate(0, 0, 10)); day = day.AddDate(0, 0, 1) {
			if !isPeriodByDate[dateOnly(day)] {
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

// sameDay reports whether a and b fall on the same calendar day, each read in
// its own location — exactly the comparison the former string-key form
// (Format("2006-01-02") equality) expressed, without the two allocations.
func sameDay(a, b time.Time) bool {
	return dateOnly(a).Equal(dateOnly(b))
}

// dateOnly reduces an instant to the midnight of its calendar day, rebuilt at
// UTC. Stored date-only values (DailyLog.Date) are persisted at UTC-midnight,
// and derived stats dates inherit that. Anchoring `now` to UTC-midnight of its
// displayed calendar day keeps "today" comparable with those stored dates;
// using t.Location() instead skews cross-timezone comparisons by up to a day
// (today's log dropped on UTC+ servers, off-by-one cycle day).
func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
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

// stddevInts returns the sample standard deviation (n-1 denominator) of
// values; with fewer than two values the spread is undefined and 0 is
// returned. The observed cycle lengths are a small sample (at most the
// recent prediction window) of the owner's ongoing cycle process, and the
// population formula systematically understates variability at such n.
func stddevInts(values []int) float64 {
	if len(values) < 2 {
		return 0
	}

	mean := averageInts(values)
	var squared float64
	for _, value := range values {
		diff := float64(value) - mean
		squared += diff * diff
	}
	return math.Sqrt(squared / float64(len(values)-1))
}
