package services

import (
	"sort"
	"strconv"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type StatsBBTChartViewData struct {
	Labels         []string
	Values         []*float64
	Baseline       float64
	HasBaseline    bool
	Kind           string
	MarkerIndex    int
	MarkerLabelKey string
	HasMarker      bool
}

type StatsSymptomPatternViewData struct {
	Name     string
	Icon     string
	Count    int
	DayStart int
	DayEnd   int
}

type completedCycleSpan struct {
	Start        time.Time
	NextStart    time.Time
	CycleLength  int
	PeriodLength int
}

func buildCompletedCycleSpans(logs []models.DailyLog, location *time.Location) []completedCycleSpan {
	starts := ObservedCycleStarts(logs)
	if len(starts) < 2 {
		return nil
	}

	sorted := sortDailyLogs(logs)
	cycles := buildCycles(starts, sorted)
	spans := make([]completedCycleSpan, 0, len(starts)-1)
	for index := 0; index+1 < len(starts) && index < len(cycles); index++ {
		start := DateAtLocation(starts[index], location)
		nextStart := DateAtLocation(starts[index+1], location)
		cycleLength := int(nextStart.Sub(start).Hours() / 24)
		if cycleLength <= 0 {
			continue
		}

		periodLength := cycles[index].PeriodLength
		if periodLength <= 0 {
			periodLength = models.DefaultPeriodLength
		}

		spans = append(spans, completedCycleSpan{
			Start:        start,
			NextStart:    nextStart,
			CycleLength:  cycleLength,
			PeriodLength: periodLength,
		})
	}

	return spans
}

func buildLastCycleSymptomCounts(language string, logs []models.DailyLog, completedCycles []completedCycleSpan, symptomByID map[uint]models.SymptomType, location *time.Location) []StatsSymptomCountViewData {
	if len(completedCycles) == 0 || len(symptomByID) == 0 {
		return nil
	}

	lastCycle := completedCycles[len(completedCycles)-1]
	counts := make(map[uint]int, len(symptomByID))
	totalLoggedDays := 0
	for _, logEntry := range logs {
		localDay := DateAtLocation(logEntry.Date, location)
		if localDay.Before(lastCycle.Start) || !localDay.Before(lastCycle.NextStart) {
			continue
		}

		totalLoggedDays++
		for _, symptomID := range uniqueKnownSymptomIDs(logEntry.SymptomIDs, symptomByID) {
			counts[symptomID]++
		}
	}

	if totalLoggedDays == 0 || len(counts) == 0 {
		return nil
	}

	items := make([]StatsSymptomCountViewData, 0, len(counts))
	for symptomID, count := range counts {
		symptom := symptomByID[symptomID]
		items = append(items, StatsSymptomCountViewData{
			Name:             symptom.Name,
			Icon:             symptom.Icon,
			Count:            count,
			TotalDays:        totalLoggedDays,
			FrequencySummary: LocalizedSymptomFrequencySummary(language, count, totalLoggedDays),
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Name < items[j].Name
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > 3 {
		return items[:3]
	}
	return items
}

func buildSymptomPatternInsights(logs []models.DailyLog, completedCycles []completedCycleSpan, symptomByID map[uint]models.SymptomType, location *time.Location) []StatsSymptomPatternViewData {
	if len(completedCycles) < minimumPhaseInsightCycles || len(symptomByID) == 0 {
		return nil
	}

	type patternCounter struct {
		count    int
		dayStart int
		dayEnd   int
	}

	counters := make(map[uint]*patternCounter, len(symptomByID))
	for _, logEntry := range logs {
		dayNumber, ok := completedCycleDayNumber(logEntry.Date, completedCycles, location)
		if !ok {
			continue
		}

		for _, symptomID := range uniqueKnownSymptomIDs(logEntry.SymptomIDs, symptomByID) {
			counter, exists := counters[symptomID]
			if !exists {
				counters[symptomID] = &patternCounter{
					count:    1,
					dayStart: dayNumber,
					dayEnd:   dayNumber,
				}
				continue
			}

			counter.count++
			if dayNumber < counter.dayStart {
				counter.dayStart = dayNumber
			}
			if dayNumber > counter.dayEnd {
				counter.dayEnd = dayNumber
			}
		}
	}

	items := make([]StatsSymptomPatternViewData, 0, len(counters))
	for symptomID, counter := range counters {
		symptom := symptomByID[symptomID]
		items = append(items, StatsSymptomPatternViewData{
			Name:     symptom.Name,
			Icon:     symptom.Icon,
			Count:    counter.count,
			DayStart: counter.dayStart,
			DayEnd:   counter.dayEnd,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Count == items[j].Count {
			return items[i].Name < items[j].Name
		}
		return items[i].Count > items[j].Count
	})
	if len(items) > 2 {
		return items[:2]
	}
	return items
}

func buildCurrentCycleBBTChart(stats CycleStats, logs []models.DailyLog, now time.Time, location *time.Location) StatsBBTChartViewData {
	if stats.LastPeriodStart.IsZero() {
		return StatsBBTChartViewData{}
	}
	if location == nil {
		location = time.UTC
	}

	cycleStart := DateAtLocation(stats.LastPeriodStart, location)
	today := DateAtLocation(now.In(location), location)
	if today.Before(cycleStart) {
		return StatsBBTChartViewData{}
	}

	dayValues := make(map[int]float64)
	recordedDays := make([]int, 0)
	for _, logEntry := range sortDailyLogs(filterLogsNotAfter(logs, today)) {
		localDay := DateAtLocation(logEntry.Date, location)
		if localDay.Before(cycleStart) || localDay.After(today) {
			continue
		}
		if !IsValidDayBBT(logEntry.BBT) || logEntry.BBT <= 0 {
			continue
		}

		dayNumber := int(localDay.Sub(cycleStart).Hours()/24) + 1
		if dayNumber <= 0 {
			continue
		}
		if _, exists := dayValues[dayNumber]; !exists {
			recordedDays = append(recordedDays, dayNumber)
		}
		dayValues[dayNumber] = logEntry.BBT
	}

	sort.Ints(recordedDays)
	if len(recordedDays) < 5 {
		return StatsBBTChartViewData{}
	}

	maxDay := recordedDays[len(recordedDays)-1]
	labels := make([]string, maxDay)
	values := make([]*float64, maxDay)
	baselineWindow := make([]float64, 0, 5)
	for dayNumber := 1; dayNumber <= maxDay; dayNumber++ {
		labels[dayNumber-1] = strconv.Itoa(dayNumber)
		value, exists := dayValues[dayNumber]
		if !exists {
			continue
		}

		pointValue := value
		values[dayNumber-1] = &pointValue
		if len(baselineWindow) < 5 {
			baselineWindow = append(baselineWindow, value)
		}
	}

	if len(baselineWindow) < 5 {
		return StatsBBTChartViewData{}
	}

	var baseline float64
	for _, value := range baselineWindow {
		baseline += value
	}
	baseline /= float64(len(baselineWindow))

	markerIndex, hasMarker := detectProbableOvulationMarker(recordedDays, dayValues, baseline)

	return StatsBBTChartViewData{
		Labels:         labels,
		Values:         values,
		Baseline:       baseline,
		HasBaseline:    true,
		Kind:           "line",
		MarkerIndex:    markerIndex,
		MarkerLabelKey: "stats.bbt_probable_ovulation",
		HasMarker:      hasMarker,
	}
}

func detectProbableOvulationMarker(recordedDays []int, dayValues map[int]float64, baseline float64) (int, bool) {
	if len(recordedDays) < 8 {
		return 0, false
	}

	threshold := baseline + 0.2
	for index := 5; index+2 < len(recordedDays); index++ {
		dayOne := recordedDays[index]
		dayTwo := recordedDays[index+1]
		dayThree := recordedDays[index+2]
		if dayTwo != dayOne+1 || dayThree != dayTwo+1 {
			continue
		}
		if dayValues[dayOne] < threshold || dayValues[dayTwo] < threshold || dayValues[dayThree] < threshold {
			continue
		}

		markerDay := dayOne - 1
		if markerDay < 1 {
			markerDay = dayOne
		}
		return markerDay - 1, true
	}

	return 0, false
}

func completedCycleDayNumber(day time.Time, completedCycles []completedCycleSpan, location *time.Location) (int, bool) {
	localDay := DateAtLocation(day, location)
	for _, cycle := range completedCycles {
		if localDay.Before(cycle.Start) || !localDay.Before(cycle.NextStart) {
			continue
		}
		return int(localDay.Sub(cycle.Start).Hours()/24) + 1, true
	}
	return 0, false
}

func uniqueKnownSymptomIDs(symptomIDs []uint, symptomByID map[uint]models.SymptomType) []uint {
	if len(symptomIDs) == 0 || len(symptomByID) == 0 {
		return nil
	}

	unique := make([]uint, 0, len(symptomIDs))
	seen := make(map[uint]struct{}, len(symptomIDs))
	for _, symptomID := range symptomIDs {
		if _, exists := symptomByID[symptomID]; !exists {
			continue
		}
		if _, duplicate := seen[symptomID]; duplicate {
			continue
		}
		seen[symptomID] = struct{}{}
		unique = append(unique, symptomID)
	}
	return unique
}
