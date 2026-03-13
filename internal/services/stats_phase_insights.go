package services

import (
	"sort"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

const minimumPhaseInsightCycles = 3

var phaseInsightOrder = []string{"menstrual", "follicular", "ovulation", "luteal"}

type StatsPhaseMoodInsight struct {
	Phase       string
	AverageMood float64
	Percentage  float64
	EntryCount  int
	HasData     bool
}

type StatsPhaseSymptomInsightItem struct {
	Name       string
	Icon       string
	Count      int
	TotalDays  int
	Percentage float64
}

type StatsPhaseSymptomInsight struct {
	Phase     string
	TotalDays int
	Items     []StatsPhaseSymptomInsightItem
	HasData   bool
}

type phaseSymptomCounter struct {
	totalDays int
	counts    map[uint]int
}

type completedCyclePhaseContext struct {
	Start        time.Time
	NextStart    time.Time
	CycleLength  int
	PeriodLength int
	OvulationDay int
}

func buildCompletedCyclePhaseContexts(logs []models.DailyLog, location *time.Location) []completedCyclePhaseContext {
	starts := DetectCycleStarts(logs)
	if len(starts) < 2 {
		return nil
	}

	sorted := sortDailyLogs(logs)
	cycles := buildCycles(starts, sorted)
	contexts := make([]completedCyclePhaseContext, 0, len(starts)-1)
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

		ovulationDay, _ := CalcOvulationDay(cycleLength, defaultLutealPhaseDays)
		if ovulationDay <= 0 {
			continue
		}

		contexts = append(contexts, completedCyclePhaseContext{
			Start:        start,
			NextStart:    nextStart,
			CycleLength:  cycleLength,
			PeriodLength: periodLength,
			OvulationDay: ovulationDay,
		})
	}

	return contexts
}

func phaseForCompletedCycleDay(day time.Time, cycle completedCyclePhaseContext, location *time.Location) string {
	localDay := DateAtLocation(day, location)
	if localDay.Before(cycle.Start) || !localDay.Before(cycle.NextStart) {
		return ""
	}

	dayNumber := int(localDay.Sub(cycle.Start).Hours()/24) + 1
	switch {
	case dayNumber <= cycle.PeriodLength:
		return "menstrual"
	case dayNumber == cycle.OvulationDay:
		return "ovulation"
	case dayNumber < cycle.OvulationDay:
		return "follicular"
	default:
		return "luteal"
	}
}

func findCompletedCycleForDay(day time.Time, cycles []completedCyclePhaseContext, location *time.Location) (completedCyclePhaseContext, bool) {
	localDay := DateAtLocation(day, location)
	for _, cycle := range cycles {
		if !localDay.Before(cycle.Start) && localDay.Before(cycle.NextStart) {
			return cycle, true
		}
	}
	return completedCyclePhaseContext{}, false
}

func (service *StatsService) BuildPhaseMoodInsights(user *models.User, logs []models.DailyLog, location *time.Location) ([]StatsPhaseMoodInsight, bool) {
	if !IsOwnerUser(user) {
		return nil, false
	}

	cycles := buildCompletedCyclePhaseContexts(logs, location)
	if len(cycles) < minimumPhaseInsightCycles {
		return nil, false
	}

	type phaseTotals struct {
		total int
		count int
	}

	totals := map[string]phaseTotals{
		"menstrual":  {},
		"follicular": {},
		"ovulation":  {},
		"luteal":     {},
	}

	for _, logEntry := range logs {
		if logEntry.Mood < MinDayMood || logEntry.Mood > MaxDayMood {
			continue
		}
		cycle, ok := findCompletedCycleForDay(logEntry.Date, cycles, location)
		if !ok {
			continue
		}
		phase := phaseForCompletedCycleDay(logEntry.Date, cycle, location)
		if phase == "" {
			continue
		}
		current := totals[phase]
		current.total += logEntry.Mood
		current.count++
		totals[phase] = current
	}

	insights := make([]StatsPhaseMoodInsight, 0, 4)
	hasData := false
	for _, phase := range phaseInsightOrder {
		current := totals[phase]
		insight := StatsPhaseMoodInsight{Phase: phase, EntryCount: current.count}
		if current.count > 0 {
			insight.HasData = true
			insight.AverageMood = float64(current.total) / float64(current.count)
			insight.Percentage = insight.AverageMood * 20
			hasData = true
		}
		insights = append(insights, insight)
	}

	return insights, hasData
}

func (service *StatsService) BuildPhaseSymptomInsights(user *models.User, logs []models.DailyLog, location *time.Location) ([]StatsPhaseSymptomInsight, bool, error) {
	if !canBuildPhaseSymptomInsights(service, user) {
		return nil, false, nil
	}

	symptomByID, err := service.phaseInsightSymptomMap(user.ID)
	if err != nil {
		return nil, false, err
	}

	insights, hasData := buildPhaseSymptomInsightsWithMap(logs, location, symptomByID)
	return insights, hasData, nil
}

func canBuildPhaseSymptomInsights(service *StatsService, user *models.User) bool {
	return IsOwnerUser(user) && service != nil && service.symptoms != nil
}

func buildPhaseSymptomInsightsWithMap(logs []models.DailyLog, location *time.Location, symptomByID map[uint]models.SymptomType) ([]StatsPhaseSymptomInsight, bool) {
	cycles := buildCompletedCyclePhaseContexts(logs, location)
	if len(cycles) < minimumPhaseInsightCycles || len(symptomByID) == 0 {
		return nil, false
	}

	counters := buildPhaseSymptomCounters(logs, cycles, location, symptomByID)
	return buildPhaseSymptomInsightResults(counters, symptomByID), hasPhaseSymptomInsightData(counters)
}

func (service *StatsService) phaseInsightSymptomMap(userID uint) (map[uint]models.SymptomType, error) {
	symptoms, err := service.symptoms.FetchSymptoms(userID)
	if err != nil {
		return nil, err
	}

	symptomByID := make(map[uint]models.SymptomType, len(symptoms))
	for _, symptom := range symptoms {
		symptomByID[symptom.ID] = symptom
	}
	return symptomByID, nil
}

func buildPhaseSymptomCounters(logs []models.DailyLog, cycles []completedCyclePhaseContext, location *time.Location, symptomByID map[uint]models.SymptomType) map[string]*phaseSymptomCounter {
	counters := newPhaseSymptomCounters()
	for _, logEntry := range logs {
		phase, ok := phaseForCompletedLogEntry(logEntry.Date, cycles, location)
		if !ok {
			continue
		}
		appendPhaseSymptomCounts(counters[phase], logEntry.SymptomIDs, symptomByID)
		counters[phase].totalDays++
	}
	return counters
}

func newPhaseSymptomCounters() map[string]*phaseSymptomCounter {
	counters := make(map[string]*phaseSymptomCounter, len(phaseInsightOrder))
	for _, phase := range phaseInsightOrder {
		counters[phase] = &phaseSymptomCounter{counts: make(map[uint]int)}
	}
	return counters
}

func phaseForCompletedLogEntry(day time.Time, cycles []completedCyclePhaseContext, location *time.Location) (string, bool) {
	cycle, ok := findCompletedCycleForDay(day, cycles, location)
	if !ok {
		return "", false
	}
	phase := phaseForCompletedCycleDay(day, cycle, location)
	if phase == "" {
		return "", false
	}
	return phase, true
}

func appendPhaseSymptomCounts(counter *phaseSymptomCounter, symptomIDs []uint, symptomByID map[uint]models.SymptomType) {
	for _, symptomID := range symptomIDs {
		if _, exists := symptomByID[symptomID]; !exists {
			continue
		}
		counter.counts[symptomID]++
	}
}

func buildPhaseSymptomInsightResults(counters map[string]*phaseSymptomCounter, symptomByID map[uint]models.SymptomType) []StatsPhaseSymptomInsight {
	insights := make([]StatsPhaseSymptomInsight, 0, len(phaseInsightOrder))
	for _, phase := range phaseInsightOrder {
		current := counters[phase]
		items := phaseSymptomInsightItems(current, symptomByID)
		insights = append(insights, StatsPhaseSymptomInsight{
			Phase:     phase,
			TotalDays: current.totalDays,
			Items:     items,
			HasData:   len(items) > 0,
		})
	}
	return insights
}

func phaseSymptomInsightItems(counter *phaseSymptomCounter, symptomByID map[uint]models.SymptomType) []StatsPhaseSymptomInsightItem {
	if counter == nil || counter.totalDays == 0 || len(counter.counts) == 0 {
		return nil
	}

	items := make([]StatsPhaseSymptomInsightItem, 0, len(counter.counts))
	for symptomID, count := range counter.counts {
		symptom := symptomByID[symptomID]
		items = append(items, StatsPhaseSymptomInsightItem{
			Name:       symptom.Name,
			Icon:       symptom.Icon,
			Count:      count,
			TotalDays:  counter.totalDays,
			Percentage: float64(count) * 100 / float64(counter.totalDays),
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

func hasPhaseSymptomInsightData(counters map[string]*phaseSymptomCounter) bool {
	for _, phase := range phaseInsightOrder {
		current := counters[phase]
		if current != nil && current.totalDays > 0 && len(current.counts) > 0 {
			return true
		}
	}
	return false
}
