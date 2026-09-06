package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrStatsPageViewLoadStats    = errors.New("stats page view load stats")
	ErrStatsPageViewLoadSymptoms = errors.New("stats page view load symptoms")
)

type StatsChartViewData struct {
	Labels      []string
	Values      []int
	Baseline    int
	HasBaseline bool
	Kind        string
}

type StatsSymptomCountViewData struct {
	Name             string
	Icon             string
	Count            int
	TotalDays        int
	FrequencySummary string
}

type StatsPageViewData struct {
	Stats                             CycleStats
	ChartData                         StatsChartViewData
	ChartBaseline                     int
	TrendPointCount                   int
	Flags                             StatsFlags
	PredictionSampleCount             int
	PredictionSampleUsesRecentWindow  bool
	PredictionReliabilityLabelKey     string
	PredictionReliabilityHintKey      string
	ShowPredictionReliability         bool
	PredictionExplanationPrimaryKey   string
	PredictionExplanationSecondaryKey string
	HasPredictionExplanationPrimary   bool
	HasPredictionExplanationSecondary bool
	RecentCycleFactors                []StatsCycleFactorContextItem
	CycleFactorPatternSummaries       []StatsCycleFactorPatternSummary
	RecentFactorCycles                []StatsCycleFactorRecentCycleSummary
	PredictionFactorHintKeys          []string
	LastCycleSymptoms                 []StatsSymptomCountViewData
	SymptomPatterns                   []StatsSymptomPatternViewData
	SymptomCounts                     []StatsSymptomCountViewData
	CurrentCycleBBTChart              StatsBBTChartViewData
	CycleRibbon                       StatsCycleRibbon
	PhaseMoodInsights                 []StatsPhaseMoodInsight
	PhaseSymptomInsights              []StatsPhaseSymptomInsight
	Statements                        []StatsStatement
	HasStatements                     bool
	HasLastCycleSymptoms              bool
	HasSymptomPatterns                bool
	HasRecentCycleFactors             bool
	HasCycleFactorPatternSummaries    bool
	HasRecentFactorCycles             bool
	HasPredictionFactorHint           bool
	HasCurrentCycleBBTChart           bool
	HasPhaseMoodInsights              bool
	HasPhaseSymptomInsights           bool
	// ShowIrregularityNotice gates both the irregularity notice and the
	// irregular-mode recommendation in the stats template: they are two
	// strings shown under one condition (was two always-equal fields).
	ShowIrregularityNotice              bool
	ShowIrregularInsufficientDataNotice bool
	// ShowShortCycleNotice / ShowLongCycleNotice surface a soft, pattern-gated
	// note when several recent completed cycles are clinically short (< 24
	// days) or long (> 45 days). Both gate at 3+ occurrences so a single
	// short or merged-log cycle never triggers medical wording —
	// deliberately anti-anxiety, framed as a pattern, not a single event.
	ShowShortCycleNotice  bool
	ShowLongCycleNotice   bool
	ShowPerimenopauseHint bool
	PredictionDisabled    bool
	IsIrregularMode       bool
	IsOwner               bool
}

type statsPageBaseData struct {
	stats           CycleStats
	logs            []models.DailyLog
	chartData       StatsChartViewData
	chartBaseline   int
	trendPointCount int
	flags           StatsFlags
}

type statsOwnerInsightsViewData struct {
	lastCycleSymptoms       []StatsSymptomCountViewData
	symptomPatterns         []StatsSymptomPatternViewData
	currentCycleBBTChart    StatsBBTChartViewData
	cycleRibbon             StatsCycleRibbon
	phaseSymptomInsights    []StatsPhaseSymptomInsight
	statements              []StatsStatement
	hasPhaseSymptomInsights bool
}

func (service *StatsService) BuildStatsPageViewData(ctx context.Context, user *models.User, language string, cycleLabelPattern string, now time.Time, location *time.Location, maxTrendPoints int) (StatsPageViewData, error) {
	baseData, err := service.buildStatsPageBaseData(ctx, user, cycleLabelPattern, now, location, maxTrendPoints)
	if err != nil {
		return StatsPageViewData{}, err
	}

	frequencies, err := service.BuildSymptomFrequenciesForUser(ctx, user)
	if err != nil {
		return StatsPageViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadSymptoms, err)
	}
	symptomCounts := buildStatsSymptomCountViewData(language, frequencies)
	phaseMoodInsights, hasPhaseMoodInsights := service.BuildPhaseMoodInsights(user, baseData.logs, location)
	// The one traversal that yields the completed-cycle lengths feeds every
	// consumer below, including the statements section, so the tiers there are
	// read off StatsFlags.CompletedCycleCount's own source rather than a
	// second count of the same thing.
	completedCycleLengths := CompletedCycleTrendLengths(baseData.logs, now, location)
	ownerInsights, err := service.buildOwnerStatsInsights(ctx, user, language, baseData.stats, baseData.logs, completedCycleLengths, now, location)
	if err != nil {
		return StatsPageViewData{}, err
	}

	showIrregularityNotice := shouldShowStatsIrregularityNotice(user, baseData.flags, baseData.stats)
	showIrregularInsufficientDataNotice := shouldShowStatsIrregularInsufficientDataNotice(user, baseData.flags)
	showShortCycleNotice := shouldShowStatsShortCycleNotice(user, completedCycleLengths)
	showLongCycleNotice := shouldShowStatsLongCycleNotice(user, completedCycleLengths)
	showPerimenopauseHint := shouldShowStatsPerimenopauseHint(user)
	cycleContext := BuildDashboardCycleContext(user, baseData.logs, baseData.stats, DateAtLocation(now, location), location)
	// The stats phase card reads the suppression the cycle context already
	// resolved, not DashboardPredictionDisabled(user) on its own: the
	// unpredictable-cycle setting is one signal among several, and a pregnancy
	// pause left this surface — the last prediction surface without the gate the
	// calendar grid, the .ics feed and the webhook pass all carry — still
	// claiming a fertile window whose only source is the onboarding
	// cycle-length slider. Reading the resolved decision rather than
	// recombining the signals here keeps /stats moving with the dashboard on
	// every later reliability change. Regression:
	// TestStatsFertileWindowCardIsSuppressedByAPregnancyPause.
	predictionDisabled := cycleContext.PredictionDisabled
	isIrregularMode := isStatsIrregularMode(user)
	isOwner := IsOwnerUser(user)
	predictionSampleCount, predictionSampleUsesRecentWindow, predictionReliabilityLabelKey, predictionReliabilityHintKey, showPredictionReliability := buildStatsPredictionReliability(user, baseData.flags, baseData.stats)
	cycleFactorExplanation, hasCycleFactorExplanation := buildStatsCycleFactorExplanation(user, baseData.logs, baseData.stats, now, location)
	predictionExplanation := BuildOwnerPredictionExplanation(user, cycleContext, hasCycleFactorExplanation && len(cycleFactorExplanation.HintFactorKeys) > 0)
	// The one adapter every projection surface publishes through, so /stats and
	// the JSON API cannot drift apart on what a suppressed tier may carry — and,
	// ahead of it, the one resolver that moves the ovulation day, the fertile
	// window and the fertility status onto a shift the owner's temperatures
	// confirm, so this page's fertile-window card cannot name the projection
	// while the grid and the chart name the confirmed day.
	confirmedStats, _ := ResolveConfirmedCycleStats(user, baseData.logs, baseData.stats, DateAtLocation(now, location), location)
	publishedStats, _ := PublishedStats(user, confirmedStats)

	return StatsPageViewData{
		Stats:                               publishedStats,
		ChartData:                           baseData.chartData,
		ChartBaseline:                       baseData.chartBaseline,
		TrendPointCount:                     baseData.trendPointCount,
		Flags:                               baseData.flags,
		PredictionSampleCount:               predictionSampleCount,
		PredictionSampleUsesRecentWindow:    predictionSampleUsesRecentWindow,
		PredictionReliabilityLabelKey:       predictionReliabilityLabelKey,
		PredictionReliabilityHintKey:        predictionReliabilityHintKey,
		ShowPredictionReliability:           showPredictionReliability,
		PredictionExplanationPrimaryKey:     predictionExplanation.PrimaryKey,
		PredictionExplanationSecondaryKey:   predictionExplanation.SecondaryKey,
		HasPredictionExplanationPrimary:     predictionExplanation.PrimaryKey != "",
		HasPredictionExplanationSecondary:   predictionExplanation.SecondaryKey != "",
		RecentCycleFactors:                  cycleFactorExplanation.RecentFactors,
		CycleFactorPatternSummaries:         cycleFactorExplanation.PatternSummaries,
		RecentFactorCycles:                  cycleFactorExplanation.RecentCycles,
		PredictionFactorHintKeys:            cycleFactorExplanation.HintFactorKeys,
		LastCycleSymptoms:                   ownerInsights.lastCycleSymptoms,
		SymptomPatterns:                     ownerInsights.symptomPatterns,
		SymptomCounts:                       symptomCounts,
		CurrentCycleBBTChart:                ownerInsights.currentCycleBBTChart,
		CycleRibbon:                         ownerInsights.cycleRibbon,
		PhaseMoodInsights:                   phaseMoodInsights,
		PhaseSymptomInsights:                ownerInsights.phaseSymptomInsights,
		Statements:                          ownerInsights.statements,
		HasStatements:                       len(ownerInsights.statements) > 0,
		HasLastCycleSymptoms:                len(ownerInsights.lastCycleSymptoms) > 0,
		HasSymptomPatterns:                  len(ownerInsights.symptomPatterns) > 0,
		HasRecentCycleFactors:               hasCycleFactorExplanation && len(cycleFactorExplanation.RecentFactors) > 0,
		HasCycleFactorPatternSummaries:      hasCycleFactorExplanation && len(cycleFactorExplanation.PatternSummaries) > 0,
		HasRecentFactorCycles:               hasCycleFactorExplanation && len(cycleFactorExplanation.RecentCycles) > 0,
		HasPredictionFactorHint:             hasCycleFactorExplanation && len(cycleFactorExplanation.HintFactorKeys) > 0,
		HasCurrentCycleBBTChart:             len(ownerInsights.currentCycleBBTChart.Labels) > 0,
		HasPhaseMoodInsights:                hasPhaseMoodInsights,
		HasPhaseSymptomInsights:             ownerInsights.hasPhaseSymptomInsights,
		ShowIrregularityNotice:              showIrregularityNotice,
		ShowIrregularInsufficientDataNotice: showIrregularInsufficientDataNotice,
		ShowShortCycleNotice:                showShortCycleNotice,
		ShowLongCycleNotice:                 showLongCycleNotice,
		ShowPerimenopauseHint:               showPerimenopauseHint,
		PredictionDisabled:                  predictionDisabled,
		IsIrregularMode:                     isIrregularMode,
		IsOwner:                             isOwner,
	}, nil
}

func (service *StatsService) buildStatsPageBaseData(ctx context.Context, user *models.User, cycleLabelPattern string, now time.Time, location *time.Location, maxTrendPoints int) (statsPageBaseData, error) {
	stats, logs, err := service.BuildCycleStatsForRange(ctx, user, now.AddDate(-2, 0, 0), now, now, location)
	if err != nil {
		return statsPageBaseData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadStats, err)
	}

	lengths, baselineCycleLength := service.BuildTrend(user, logs, now, location, maxTrendPoints)
	trendPointCount := len(lengths)

	return statsPageBaseData{
		stats:           stats,
		logs:            logs,
		chartData:       buildStatsChartViewData(cycleLabelPattern, lengths, baselineCycleLength),
		chartBaseline:   baselineCycleLength,
		trendPointCount: trendPointCount,
		flags:           service.BuildFlags(user, logs, stats, now, location, trendPointCount),
	}, nil
}

func buildStatsChartViewData(cycleLabelPattern string, lengths []int, baselineCycleLength int) StatsChartViewData {
	chartData := StatsChartViewData{
		Labels: BuildCycleTrendLabels(cycleLabelPattern, len(lengths)),
		Values: lengths,
		Kind:   "bar",
	}
	if baselineCycleLength > 0 {
		chartData.Baseline = baselineCycleLength
		chartData.HasBaseline = true
	}
	return chartData
}

func buildStatsSymptomCountViewData(language string, frequencies []SymptomFrequency) []StatsSymptomCountViewData {
	symptomCounts := make([]StatsSymptomCountViewData, 0, len(frequencies))
	for _, item := range frequencies {
		symptomCounts = append(symptomCounts, StatsSymptomCountViewData{
			Name:             item.Name,
			Icon:             item.Icon,
			Count:            item.Count,
			TotalDays:        item.TotalDays,
			FrequencySummary: LocalizedSymptomFrequencySummary(language, item.Count, item.TotalDays),
		})
	}
	return symptomCounts
}

func (service *StatsService) buildOwnerStatsInsights(ctx context.Context, user *models.User, language string, stats CycleStats, logs []models.DailyLog, completedCycleLengths []int, now time.Time, location *time.Location) (statsOwnerInsightsViewData, error) {
	insights := statsOwnerInsightsViewData{}
	if !IsOwnerUser(user) {
		return insights, nil
	}

	insights.currentCycleBBTChart = buildCurrentCycleBBTChart(language, stats, logs, now, location)
	// The cycle-length trend needs no symptom catalogue, so it is built before
	// the reader check below: an owner with no symptom repository still gets
	// the statement about their own cycle lengths.
	if trend, ok := buildCycleLengthTrendStatement(completedCycleLengths); ok {
		insights.statements = append(insights.statements, trend)
	}
	if service.symptoms == nil {
		return insights, nil
	}

	symptomByID, err := service.phaseInsightSymptomMap(ctx, user.ID)
	if err != nil {
		return statsOwnerInsightsViewData{}, fmt.Errorf("%w: %v", ErrStatsPageViewLoadSymptoms, err)
	}
	completedCycles := buildCompletedCycleSpans(logs, location)
	insights.cycleRibbon = buildStatsCycleRibbon(user, stats, logs, completedCycles)
	insights.lastCycleSymptoms = buildLastCycleSymptomCounts(language, logs, completedCycles, symptomByID, location)
	insights.symptomPatterns = buildSymptomPatternInsights(logs, completedCycles, symptomByID, location)
	insights.phaseSymptomInsights, insights.hasPhaseSymptomInsights = buildPhaseSymptomInsightsWithMap(logs, location, symptomByID)
	insights.statements = append(insights.statements, buildSymptomPhaseRecurrenceStatements(logs, symptomByID, location)...)
	return insights, nil
}

func shouldShowStatsIrregularityNotice(user *models.User, flags StatsFlags, stats CycleStats) bool {
	return IsOwnerUser(user) && !user.IrregularCycle && flags.CompletedCycleCount >= 3 && IsIrregularCycleSpread(stats)
}

func shouldShowStatsIrregularInsufficientDataNotice(user *models.User, flags StatsFlags) bool {
	return user != nil && user.IrregularCycle && flags.CompletedCycleCount < 3
}

// shortCycleNoticeThresholdDays matches the app's existing "less common"
// boundary (the settings/onboarding info_cycle_short advisory fires below
// the same value), so the logged-cycle note and the cycle-length setting
// stay consistent.
const shortCycleNoticeThresholdDays = 24

// shortCycleNoticeMinimumOccurrences requires a repeated pattern before the
// note shows, so a one-off short cycle (or a missed-log artifact) never
// surfaces medical wording.
const shortCycleNoticeMinimumOccurrences = 3

// shouldShowStatsShortCycleNotice surfaces a soft "several recent cycles are
// short" note once the owner has at least shortCycleNoticeMinimumOccurrences
// completed cycles below shortCycleNoticeThresholdDays. Pattern-gated on
// purpose: a single short or merged-log cycle must not trigger medical copy.
func shouldShowStatsShortCycleNotice(user *models.User, completedCycleLengths []int) bool {
	if !IsOwnerUser(user) {
		return false
	}
	short := 0
	for _, length := range completedCycleLengths {
		if length > 0 && length < shortCycleNoticeThresholdDays {
			short++
		}
	}
	return short >= shortCycleNoticeMinimumOccurrences
}

// longCycleNoticeThresholdDays is set deliberately high (above the clinical
// oligomenorrhea boundary of ~35 days) so the pattern-gated note is
// conservative: a single missed log that merges two cycles into a 60-90 day
// span is common, so only a repeated genuinely-long pattern should surface
// medical wording. Mirrors the >45 value the cycle-length setting once used.
const longCycleNoticeThresholdDays = 45

// shouldShowStatsLongCycleNotice mirrors shouldShowStatsShortCycleNotice for
// the long end: a soft note once the owner has at least
// shortCycleNoticeMinimumOccurrences completed cycles longer than
// longCycleNoticeThresholdDays. Pattern-gated so a one-off missed-log merge
// (which the median prediction already absorbs) never triggers it.
func shouldShowStatsLongCycleNotice(user *models.User, completedCycleLengths []int) bool {
	if !IsOwnerUser(user) {
		return false
	}
	long := 0
	for _, length := range completedCycleLengths {
		if length > longCycleNoticeThresholdDays {
			long++
		}
	}
	return long >= shortCycleNoticeMinimumOccurrences
}

// shouldShowStatsPerimenopauseHint surfaces a STRAW+10-aligned educational note
// for users aged 45+, where within-individual cycle variability rises sharply
// (Gibson et al., npj Digital Medicine 2023, Apple Women's Health Study,
// n=12,608).
//
// THE AGE BRACKET IS THE WHOLE RULE — no cycle data is consulted, and that is
// deliberate, not an omission. The copy is an invitation to look, not a verdict:
// it tells the owner that persistent ≥7-day differences between consecutive
// cycles are what marks entry into the menopausal transition (Harlow et al., the
// ReSTAGE collaboration, median entry age 45.5 years) and asks them to notice
// it. Gating the hint on that criterion would turn the invitation into a claim
// the app makes about the account, and would withhold it from exactly the owners
// with too little history to measure variability at all. It is why this function
// is unlike the two siblings above it, which DO make pattern statements and are
// therefore gated at 3 occurrences. Regression:
// TestStatsPerimenopauseHintFollowsTheAgeBracketAlone.
func shouldShowStatsPerimenopauseHint(user *models.User) bool {
	return user != nil && NormalizeAgeGroup(user.AgeGroup) == models.AgeGroup45Plus
}

func isStatsIrregularMode(user *models.User) bool {
	return user != nil && user.IrregularCycle
}

func buildStatsPredictionReliability(user *models.User, flags StatsFlags, stats CycleStats) (int, bool, string, string, bool) {
	if !HasPersonalCycleRange(user, flags.CompletedCycleCount) {
		return 0, false, "", "", false
	}

	sampleCount := flags.CompletedCycleCount
	usesRecentWindow := false
	if sampleCount > cyclePredictionWindow {
		sampleCount = cyclePredictionWindow
		usesRecentWindow = true
	}

	variablePattern := user != nil && (user.IrregularCycle || (flags.CompletedCycleCount >= minimumPhaseInsightCycles && IsIrregularCycleSpread(stats)))
	tier := resolveStatsReliabilityTier(variablePattern, sampleCount)

	return sampleCount, usesRecentWindow, tier.labelKey, tier.hintKey, true
}

// statsReliabilityTier is the single reliability verdict the card renders. The
// heading and the hint are two sentences about ONE state of the account, so
// they are read off one tier rather than derived from two independent
// expressions: the label was gated at minimumPhaseInsightCycles and the hint was
// not, which paired "the pattern is still early" with the hint written for a
// variable pattern for an irregular-cycle owner at exactly two completed cycles
// — the case HasPersonalCycleRange admits and both statements describe
// differently.
type statsReliabilityTier struct {
	labelKey string
	hintKey  string
}

var (
	// statsReliabilityEarly is the floor: enough cycles for basic insights, not
	// enough to characterize a pattern. It says nothing about variability in
	// either sentence, which is what "conservative with sparse data" means here.
	statsReliabilityEarly    = statsReliabilityTier{labelKey: "stats.reliability.early", hintKey: "stats.reliability.hint"}
	statsReliabilityBuilding = statsReliabilityTier{labelKey: "stats.reliability.building", hintKey: "stats.reliability.hint"}
	statsReliabilityStable   = statsReliabilityTier{labelKey: "stats.reliability.stable", hintKey: "stats.reliability.hint"}
	statsReliabilityVariable = statsReliabilityTier{labelKey: "stats.reliability.variable", hintKey: "stats.reliability.hint_variable"}
)

// resolveStatsReliabilityTier picks the tier from the sample the card is about.
// A variable pattern needs minimumPhaseInsightCycles behind it exactly as every
// other pattern claim on this page does; below that the account is early,
// whichever mode it is in.
func resolveStatsReliabilityTier(variablePattern bool, sampleCount int) statsReliabilityTier {
	switch {
	case variablePattern && sampleCount >= minimumPhaseInsightCycles:
		return statsReliabilityVariable
	case sampleCount >= cyclePredictionWindow:
		return statsReliabilityStable
	case sampleCount >= minimumPhaseInsightCycles:
		return statsReliabilityBuilding
	default:
		return statsReliabilityEarly
	}
}
