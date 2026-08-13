package services

import (
	"math"
	"sort"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Statements are sentences about what the account itself recorded: how its own
// cycle length moved, and which symptom keeps coming back in the same phase.
// They describe history, never a cause and never a projection — nothing here
// reads a predicted date, so an owner whose predictions are suppressed
// (unpredictable mode, a paused pregnancy, an overdue cycle) still sees them.
const (
	StatsStatementKindCycleLengthTrend       = "cycle_length_trend"
	StatsStatementKindSymptomPhaseRecurrence = "symptom_phase_recurrence"
)

const (
	StatsStatementDirectionShorter = "shorter"
	StatsStatementDirectionLonger  = "longer"
	StatsStatementDirectionSteady  = "steady"
)

// statsStatementTrendMaxWindow caps how far back the cycle-length comparison
// reaches. Six completed cycles is roughly half a year of tracking: enough for
// the two halves to mean something, short enough that the statement is about
// the account's recent rhythm rather than its whole history.
const statsStatementTrendMaxWindow = 6

// statsStatementRecurrenceMinimumHits keeps a single coincidence out of the
// section: a symptom that appeared in exactly one phase occurrence is not a
// recurrence, whatever the denominator.
const statsStatementRecurrenceMinimumHits = 2

// statsStatementRecurrenceLimit caps the recurrence family so the section
// stays a short shelf of statements rather than a dump of every symptom.
const statsStatementRecurrenceLimit = 3

// StatsStatement is one rendered sentence plus the numbers it prints.
//
// Count is the number the sentence's own plural form is chosen by for the
// trend family; for the recurrence family the plural form follows Total, the
// upper bound of "in Count of Total phases". DetailKey is the optional muted
// second line, and is empty when the sentence already carries its window.
type StatsStatement struct {
	Kind        string
	Key         string
	Count       int
	Total       int
	DetailKey   string
	DetailCount int
	Direction   string
	Phase       string
	SymptomName string
	SymptomIcon string
}

// buildCycleLengthTrendStatement compares the recent half of the account's
// completed-cycle window against the earlier half.
//
// completedCycleLengths is the slice BuildStatsPageViewData already derives
// from CompletedCycleTrendLengths — the same signal StatsFlags.CompletedCycleCount
// counts — so the gate here is that count and not a second traversal of the
// logs. On an odd window the middle cycle belongs to neither half and is
// dropped, which keeps the two halves the same size and the difference
// symmetric.
func buildCycleLengthTrendStatement(completedCycleLengths []int) (StatsStatement, bool) {
	window := statsStatementTrendWindow(completedCycleLengths)
	if len(window) < minimumPhaseInsightCycles {
		return StatsStatement{}, false
	}

	half := len(window) / 2
	delta := meanOfCycleLengths(window[len(window)-half:]) - meanOfCycleLengths(window[:half])
	days := int(math.Round(math.Abs(delta)))

	switch {
	case days == 0:
		return StatsStatement{
			Kind:      StatsStatementKindCycleLengthTrend,
			Key:       "stats.statement_cycle_trend_steady",
			Count:     len(window),
			Direction: StatsStatementDirectionSteady,
		}, true
	case delta < 0:
		return newCycleLengthTrendStatement(StatsStatementDirectionShorter, days, len(window)), true
	default:
		return newCycleLengthTrendStatement(StatsStatementDirectionLonger, days, len(window)), true
	}
}

func newCycleLengthTrendStatement(direction string, days int, windowSize int) StatsStatement {
	key := "stats.statement_cycle_trend_shorter"
	if direction == StatsStatementDirectionLonger {
		key = "stats.statement_cycle_trend_longer"
	}
	return StatsStatement{
		Kind:        StatsStatementKindCycleLengthTrend,
		Key:         key,
		Count:       days,
		DetailKey:   "stats.statement_cycle_trend_window",
		DetailCount: windowSize,
		Direction:   direction,
	}
}

// statsStatementTrendWindow returns the most recent completed cycle lengths,
// dropping any non-positive value so a merged or malformed span cannot drag a
// mean toward a change that never happened.
func statsStatementTrendWindow(completedCycleLengths []int) []int {
	window := make([]int, 0, len(completedCycleLengths))
	for _, length := range completedCycleLengths {
		if length > 0 {
			window = append(window, length)
		}
	}
	if len(window) > statsStatementTrendMaxWindow {
		window = window[len(window)-statsStatementTrendMaxWindow:]
	}
	return window
}

func meanOfCycleLengths(lengths []int) float64 {
	if len(lengths) == 0 {
		return 0
	}

	total := 0
	for _, length := range lengths {
		total += length
	}
	return float64(total) / float64(len(lengths))
}

// statsPhaseOccurrence identifies one phase of one completed cycle — the unit
// the recurrence family counts. "4 of 5 luteal phases" means four distinct
// occurrences of this key carried the symptom.
type statsPhaseOccurrence struct {
	cycleIndex int
	phase      string
}

// buildSymptomPhaseRecurrenceStatements counts, per symptom and per phase, in
// how many of the account's completed phase occurrences the symptom was
// logged. The denominator is the number of occurrences that hold at least one
// logged day, so a stretch the owner never recorded neither inflates nor
// deflates the ratio.
//
// The phase taxonomy is the product's own — menstrual / follicular / ovulation
// / luteal. Fertility is a separate axis and never appears here.
func buildSymptomPhaseRecurrenceStatements(logs []models.DailyLog, symptomByID map[uint]models.SymptomType, location *time.Location) []StatsStatement {
	cycles := buildCompletedCyclePhaseContexts(logs, location)
	if len(cycles) < minimumPhaseInsightCycles || len(symptomByID) == 0 {
		return nil
	}

	observed, hits := collectStatsPhaseOccurrences(logs, cycles, symptomByID, location)
	statements := statsRecurrenceCandidates(observed, hits, symptomByID)
	sortStatsRecurrenceStatements(statements)
	if len(statements) > statsStatementRecurrenceLimit {
		return statements[:statsStatementRecurrenceLimit]
	}
	return statements
}

func collectStatsPhaseOccurrences(logs []models.DailyLog, cycles []completedCyclePhaseContext, symptomByID map[uint]models.SymptomType, location *time.Location) (map[statsPhaseOccurrence]struct{}, map[uint]map[statsPhaseOccurrence]struct{}) {
	observed := make(map[statsPhaseOccurrence]struct{})
	hits := make(map[uint]map[statsPhaseOccurrence]struct{})

	for _, logEntry := range logs {
		occurrence, ok := statsPhaseOccurrenceForDay(logEntry.Date, cycles, location)
		if !ok {
			continue
		}

		observed[occurrence] = struct{}{}
		for _, symptomID := range uniqueKnownSymptomIDs(logEntry.SymptomIDs, symptomByID) {
			if hits[symptomID] == nil {
				hits[symptomID] = make(map[statsPhaseOccurrence]struct{})
			}
			hits[symptomID][occurrence] = struct{}{}
		}
	}

	return observed, hits
}

// statsPhaseOccurrenceForDay locates the cycle and phase a logged day falls
// in. phaseForCompletedCycleDay already answers "outside this cycle" with an
// empty phase, so it is the single bounds decision here — a second range check
// beside it would be the same predicate written twice, free to drift.
func statsPhaseOccurrenceForDay(day time.Time, cycles []completedCyclePhaseContext, location *time.Location) (statsPhaseOccurrence, bool) {
	for index, cycle := range cycles {
		phase := phaseForCompletedCycleDay(day, cycle, location)
		if phase == "" {
			continue
		}
		return statsPhaseOccurrence{cycleIndex: index, phase: phase}, true
	}
	return statsPhaseOccurrence{}, false
}

func statsRecurrenceCandidates(observed map[statsPhaseOccurrence]struct{}, hits map[uint]map[statsPhaseOccurrence]struct{}, symptomByID map[uint]models.SymptomType) []StatsStatement {
	totals := make(map[string]int, len(phaseInsightOrder))
	for occurrence := range observed {
		totals[occurrence.phase]++
	}

	statements := make([]StatsStatement, 0, len(hits))
	for symptomID, occurrences := range hits {
		counts := make(map[string]int, len(phaseInsightOrder))
		for occurrence := range occurrences {
			counts[occurrence.phase]++
		}

		for _, phase := range phaseInsightOrder {
			if !statsRecurrenceQualifies(counts[phase], totals[phase]) {
				continue
			}
			symptom := symptomByID[symptomID]
			statements = append(statements, StatsStatement{
				Kind:        StatsStatementKindSymptomPhaseRecurrence,
				Key:         "stats.statement_symptom_recurrence",
				Count:       counts[phase],
				Total:       totals[phase],
				Phase:       phase,
				SymptomName: symptom.Name,
				SymptomIcon: symptom.Icon,
			})
		}
	}
	return statements
}

// statsRecurrenceQualifies is the recurrence tier: at least
// minimumPhaseInsightCycles recorded occurrences of the phase, the symptom in
// at least statsStatementRecurrenceMinimumHits of them, and a strict majority.
// Below any of the three the statement does not render — there is no hedged
// wording for a thin sample.
func statsRecurrenceQualifies(count int, total int) bool {
	return total >= minimumPhaseInsightCycles &&
		count >= statsStatementRecurrenceMinimumHits &&
		count*2 > total
}

// sortStatsRecurrenceStatements puts the strongest recurrence first: more
// occurrences, then the tighter denominator (3 of 3 outranks 3 of 5), then the
// product's phase order, then the symptom name so the order is total.
func sortStatsRecurrenceStatements(statements []StatsStatement) {
	phaseRank := make(map[string]int, len(phaseInsightOrder))
	for index, phase := range phaseInsightOrder {
		phaseRank[phase] = index
	}

	sort.Slice(statements, func(i, j int) bool {
		left, right := statements[i], statements[j]
		if left.Count != right.Count {
			return left.Count > right.Count
		}
		if left.Total != right.Total {
			return left.Total < right.Total
		}
		if phaseRank[left.Phase] != phaseRank[right.Phase] {
			return phaseRank[left.Phase] < phaseRank[right.Phase]
		}
		return left.SymptomName < right.SymptomName
	})
}
