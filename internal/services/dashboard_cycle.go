package services

import (
	"math"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// DashboardCycleContext is the dashboard's cycle state.
//
// NextPeriodEstimatePaused reports that the running cycle is long enough
// (DashboardCycleDayLooksLong: past the reference length by more than a week)
// that no next-period window is shown at all: every Display* field it would have
// filled is cleared, and the surfaces derived from them — the status header slot,
// the reminder banner — say so instead of naming a date. The cycle day and the
// late-cycle notice carry the state on their own.
//
// AwaitingFirstCycle reports the earliest data tier — no completed cycle yet —
// which decides how much detail the header may show (DashboardAwaitingFirstCycle).
//
// FertilitySuppressed is that policy already applied, resolved once here from
// FertilityProjectionSuppressed(user, stats) — the same predicate the calendar
// grid, the .ics feed and the webhook pass call. Surfaces built from this
// context read the field rather than recombining AwaitingFirstCycle with the
// suppression signals themselves: a floor re-derived per surface diverges the
// moment the shared predicate gains a disjunct or is narrowed to let a recorded
// observation through.
type DashboardCycleContext struct {
	CycleDayReference           int
	CycleDayWarning             bool
	LateCycle                   LateCycleNotice
	CycleDataStale              bool
	PredictionDisabled          bool
	PregnancyPaused             bool
	DisplayNextPeriodStart      time.Time
	DisplayNextPeriodEnd        time.Time
	DisplayNextPeriodRangeStart time.Time
	DisplayNextPeriodRangeEnd   time.Time
	DisplayNextPeriodUseRange   bool
	DisplayNextPeriodPrompt     bool
	DisplayNextPeriodNeedsData  bool
	DisplayOvulationDate        time.Time
	DisplayOvulationRangeStart  time.Time
	DisplayOvulationRangeEnd    time.Time
	DisplayOvulationUseRange    bool
	DisplayOvulationNeedsData   bool
	// DisplayOvulationConfirmed marks DisplayOvulationDate as a day the owner's
	// own thermal shift CONFIRMS — inferred from the temperature signal, never a
	// measurement of the ovulation itself.
	// The dashboard reads it before the "needs more cycles" branch: that caption
	// is about a projection built on thin history, and the calendar grid — gated
	// on FertilityProjectionSuppressed alone — already marks the detector's day
	// for this same cohort. The hero ring and the reminder banner deliberately
	// keep gating on DisplayOvulationNeedsData: the ring is projection
	// arithmetic, and the banner counts down to a day still ahead, neither of
	// which a past confirmation answers.
	DisplayOvulationConfirmed  bool
	DisplayOvulationExact      bool
	DisplayOvulationImpossible bool
	NextPeriodEstimatePaused   bool
	AwaitingFirstCycle         bool
	FertilitySuppressed        bool
	NextPeriodInPast           bool
	OvulationInPast            bool
}

type dashboardPredictionDisplay struct {
	nextPeriodStart      time.Time
	nextPeriodEnd        time.Time
	nextPeriodRangeStart time.Time
	nextPeriodRangeEnd   time.Time
	nextPeriodUseRange   bool
	nextPeriodPrompt     bool
	nextPeriodNeedsData  bool
	ovulationDate        time.Time
	ovulationRangeStart  time.Time
	ovulationRangeEnd    time.Time
	ovulationUseRange    bool
	// ovulationConfirmed marks ovulationDate as a day a detected thermal shift
	// CONFIRMS — an inference from the owner's own signal, not a measurement of
	// the ovulation — rather than a projection. The two representations that
	// exist to express projection uncertainty — the irregular-cycle range and
	// the thin-history "needs more cycles" withholding — read it and stand
	// down, because neither is about a day the temperatures already named.
	ovulationConfirmed  bool
	ovulationNeedsData  bool
	ovulationExact      bool
	ovulationImpossible bool
	estimatePaused      bool
}

func DashboardPredictionDisabled(user *models.User) bool {
	return user != nil && user.UnpredictableCycle
}

// DashboardCycleReferenceLength is the AVERAGE-first cycle length used only as a
// REFERENCE for the cycle-day-long and data-stale warnings (and the hero phase
// ring), where "how far past the owner's typical run are we" is best captured by
// the mean. It must NOT feed a displayed next-period/ovulation DATE — use
// DashboardProjectionCycleLength for that.
func DashboardCycleReferenceLength(user *models.User, stats CycleStats) int {
	if stats.AverageCycleLength > 0 {
		return int(stats.AverageCycleLength + 0.5)
	}
	if stats.MedianCycleLength > 0 {
		return stats.MedianCycleLength
	}
	if user != nil && IsValidOnboardingCycleLength(user.CycleLength) {
		return user.CycleLength
	}
	return models.DefaultCycleLength
}

// DashboardProjectionCycleLength is the MEDIAN-first cycle length used to PROJECT
// future next-period and ovulation DATES on the dashboard hero, the webhook
// reminder decision, and the .ics feed. It delegates to predictedCycleLength —
// the exact statistic stats.NextPeriodStart and the calendar grid already use —
// so every next-period surface agrees on the date.
//
// The median is robust to a single outlier cycle: a missed period log merges two
// real cycles into one ~60-90 day gap that drags the mean by ~10 days (pushing a
// mean-based projection late) but leaves the median unmoved. That is why the
// average-first DashboardCycleReferenceLength is deliberately NOT used here.
//
// The user's configured cycle length is the zero-fallback (mirroring
// applyProjectedBaseline), used only in the degenerate case with no observed
// statistic.
func DashboardProjectionCycleLength(user *models.User, stats CycleStats) int {
	if stats.MedianCycleLength > 0 || stats.AverageCycleLength > 0 {
		return predictedCycleLength(stats.MedianCycleLength, stats.AverageCycleLength)
	}
	if user != nil && IsValidOnboardingCycleLength(user.CycleLength) {
		return user.CycleLength
	}
	return models.DefaultCycleLength
}

// DashboardCycleOverdue reports that the running cycle has passed the account's
// own reference length by more than a week. It is the reference+7 rule of
// DashboardCycleDayLooksLong — the one the late-cycle notice already states —
// resolved against DashboardCycleReferenceLength, so the threshold keeps living
// in exactly one place and no surface may re-derive it.
//
// This is the third medical-safety suppression signal, beside
// DashboardPredictionDisabled(user) and stats.PregnancyPaused: past this point a
// projection can only roll a whole cycle forward at a time (ProjectCycleStart),
// so every date it yields is manufactured rather than estimated, and presenting
// one as a window is the estimate-presented-as-fact the medical-safety invariant
// forbids. Every surface that shows a projected window gates on all three —
// through PredictionsSuppressed, which is where the three now live together.
func DashboardCycleOverdue(user *models.User, stats CycleStats) bool {
	return DashboardCycleDayLooksLong(stats.CurrentCycleDay, DashboardCycleReferenceLength(user, stats))
}

// PredictionsSuppressed is the whole-projection suppression gate: unpredictable-
// cycle mode, a pregnancy pause, or a cycle overdue past its own reference
// length. Any one of them withholds every projected date, on every surface.
//
// It exists as one predicate because the three disjuncts had been written out
// once per surface — the calendar grid, the .ics feed and the webhook pass each
// carried their own copy — so a fourth suppression signal had to be found at
// four sites, and the one that was missed (the completed-cycle floor below)
// stayed missing silently. A new signal belongs here, never in a caller.
func PredictionsSuppressed(user *models.User, stats CycleStats) bool {
	return DashboardPredictionDisabled(user) || stats.PregnancyPaused || DashboardCycleOverdue(user, stats)
}

// FertilityProjectionSuppressed adds the zero-completed-cycle floor to the three
// signals above, and is the gate for the fertility half of the projection: the
// fertile window, the peak band and the ovulation date, wherever they are shown
// or sent — calendar grid, .ics feed, webhook reminder, dashboard banner.
//
// The two predicates are deliberately not one. PredictionsSuppressed withholds
// everything; the first-cycle floor withholds only what has nothing but the
// onboarding slider behind it, which is exactly the fertility half (see
// DashboardAwaitingFirstCycle). The next-period estimate keeps its own path: it
// is anchored on a day the owner recorded and already carries an estimate
// qualifier, and the dashboard header shows it in this tier too.
func FertilityProjectionSuppressed(user *models.User, stats CycleStats) bool {
	return PredictionsSuppressed(user, stats) || DashboardAwaitingFirstCycle(stats)
}

// DashboardAwaitingFirstCycle reports that the account has not completed a
// single cycle yet, which is the earliest tier of the same reliability signal
// the stats page already counts on: CompletedCycleCount, the "based on N
// completed cycles" number behind buildStatsPredictionReliability and
// HasPersonalCycleRange. It is read here rather than recounted — a second count
// of the same thing is a second answer waiting to disagree.
//
// Until that first cycle closes, every fertility surface on the dashboard is
// derived from the onboarding cycle-length slider rather than from anything the
// account recorded: the fertile window and the ovulation date are the settings
// default projected forward. Showing them at the same confidence as a measured
// window is the estimate-presented-as-fact the medical-safety invariant forbids,
// so the header withholds them until one cycle has been observed. The phase and
// the next-period estimate stay: the phase is the axis orthogonal to fertility
// (#416) rather than a claim this tier withholds, and the next-period item
// already carries its own estimate qualifier.
func DashboardAwaitingFirstCycle(stats CycleStats) bool {
	return stats.CompletedCycleCount < 1
}

// SuppressionReason names ONE medical-safety signal that withheld a projection,
// in a stable spelling a client outside the instance may branch on. The strings
// are wire values: rename one and every consumer's branch goes quiet, so treat
// them as a published contract, not as labels.
type SuppressionReason string

const (
	SuppressionReasonUnpredictableCycle SuppressionReason = "unpredictable_cycle"
	SuppressionReasonPregnancyPause     SuppressionReason = "pregnancy_pause"
	SuppressionReasonCycleOverdue       SuppressionReason = "cycle_overdue"
	SuppressionReasonAwaitingFirstCycle SuppressionReason = "awaiting_first_cycle"
)

// PredictionSuppression is the resolved verdict of the two predicates above plus
// the reasons behind it. The two booleans are the DECISION and are read off the
// predicates, never rebuilt from the signals; Reasons only EXPLAINS a decision
// already made, which is why a surface gates on the booleans and publishes the
// reasons.
//
// The fields carry the predicates' own names on purpose: the recombination sweep
// recognises a signal by the identifier that names it
// (prediction_suppression_recombination_barrier_test.go), so a caller that ORs
// these two back together is flagged exactly as one spelling the disjuncts out
// would be.
type PredictionSuppression struct {
	PredictionsSuppressed bool
	FertilitySuppressed   bool
	Reasons               []SuppressionReason
}

// ResolvePredictionSuppression answers what a surface may publish and why. It
// lives in this file because it is the only place the four signals may be named
// together: everywhere else they are read through the two predicates.
//
// Reasons is ordered by the predicate the signal belongs to — the three
// whole-projection signals first, the fertility-only floor last — so a payload
// diffed between two releases moves only when the state does. A verdict may
// carry no reason at all: neither predicate is suppressing, which is the
// ordinary case.
//
// A fifth signal added to either predicate MUST get its reason here, or the
// payload says "suppressed" with nothing naming why.
// TestEverySuppressionSignalHasAPublishedReason fails until it does.
func ResolvePredictionSuppression(user *models.User, stats CycleStats) PredictionSuppression {
	verdict := PredictionSuppression{
		PredictionsSuppressed: PredictionsSuppressed(user, stats),
		FertilitySuppressed:   FertilityProjectionSuppressed(user, stats),
	}

	if DashboardPredictionDisabled(user) {
		verdict.Reasons = append(verdict.Reasons, SuppressionReasonUnpredictableCycle)
	}
	if stats.PregnancyPaused {
		verdict.Reasons = append(verdict.Reasons, SuppressionReasonPregnancyPause)
	}
	if DashboardCycleOverdue(user, stats) {
		verdict.Reasons = append(verdict.Reasons, SuppressionReasonCycleOverdue)
	}
	if DashboardAwaitingFirstCycle(stats) {
		verdict.Reasons = append(verdict.Reasons, SuppressionReasonAwaitingFirstCycle)
	}
	return verdict
}

func DashboardCycleDayLooksLong(currentDay int, referenceLength int) bool {
	if currentDay <= 0 || referenceLength <= 0 {
		return false
	}
	return currentDay > referenceLength+7
}

func DashboardCycleDataLooksStale(lastPeriodStart time.Time, today time.Time, referenceLength int) bool {
	if lastPeriodStart.IsZero() || referenceLength <= 0 || today.Before(lastPeriodStart) {
		return false
	}
	rawCycleDay := CalendarDaysBetween(lastPeriodStart, today) + 1
	return rawCycleDay > referenceLength
}

func DashboardCycleStaleAnchor(user *models.User, stats CycleStats, location *time.Location) time.Time {
	if !stats.LastPeriodStart.IsZero() {
		return CalendarDay(stats.LastPeriodStart, location)
	}
	if user == nil || user.LastPeriodStart == nil || user.LastPeriodStart.IsZero() {
		return time.Time{}
	}
	return CalendarDay(*user.LastPeriodStart, location)
}

// dashboardPredictionRegularSpan returns the half-width, in days, of the
// next-period prediction range for users without irregular-cycle mode.
// Returns 0 when the user has too few completed cycles for the standard
// deviation to be meaningful, signalling the caller to show a single date.
//
// The span is round(StdDev) clamped to [1, 5]. The upper bound keeps the
// UI readable for high-variability cohorts (per-user SD ≈ 5–11 days in
// participants aged 45+ in Gibson et al., npj Digital Medicine 2023,
// Apple Women's Health Study, n=12,608).
func dashboardPredictionRegularSpan(stats CycleStats) int {
	if stats.CompletedCycleCount < 3 || stats.CycleLengthStdDev <= 0 {
		return 0
	}
	span := int(math.Round(stats.CycleLengthStdDev))
	if span < 1 {
		span = 1
	}
	if span > 5 {
		span = 5
	}
	return span
}

func dashboardIrregularPredictionRangeEnabled(user *models.User, stats CycleStats) bool {
	return user != nil && user.IrregularCycle && stats.CompletedCycleCount >= 3 && stats.MinCycleLength > 0 && stats.MaxCycleLength >= stats.MinCycleLength
}

func DashboardPredictionRange(user *models.User, stats CycleStats, predictedStart time.Time, location *time.Location) (time.Time, time.Time, bool) {
	if predictedStart.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	if dashboardIrregularPredictionRangeEnabled(user, stats) {
		return AddCalendarDays(stats.LastPeriodStart, stats.MinCycleLength, location),
			AddCalendarDays(stats.LastPeriodStart, stats.MaxCycleLength, location),
			true
	}

	spanDays := dashboardPredictionRegularSpan(stats)
	if spanDays <= 0 {
		return time.Time{}, time.Time{}, false
	}
	return AddCalendarDays(predictedStart, -spanDays, location),
		AddCalendarDays(predictedStart, spanDays, location),
		true
}

func DashboardOvulationRange(nextPeriodRangeStart time.Time, nextPeriodRangeEnd time.Time, lutealPhase int, location *time.Location) (time.Time, time.Time, bool) {
	if nextPeriodRangeStart.IsZero() || nextPeriodRangeEnd.IsZero() {
		return time.Time{}, time.Time{}, false
	}

	resolvedLutealPhase := ResolveLutealPhase(lutealPhase)
	rangeStart := AddCalendarDays(nextPeriodRangeStart, -resolvedLutealPhase, location)
	rangeEnd := AddCalendarDays(nextPeriodRangeEnd, -resolvedLutealPhase, location)
	if rangeEnd.Before(rangeStart) {
		return time.Time{}, time.Time{}, false
	}

	return rangeStart, rangeEnd, true
}

// DashboardUpcomingPrediction is the named-field result of
// DashboardUpcomingPredictions: the next-period / ovulation pair the dashboard
// displays. OvulationImpossible mirrors CycleStats.OvulationImpossible — true
// when no ovulation date can be predicted for the projected cycle.
type DashboardUpcomingPrediction struct {
	NextPeriodStart     time.Time
	OvulationDate       time.Time
	OvulationExact      bool
	OvulationImpossible bool
}

func DashboardUpcomingPredictions(stats CycleStats, user *models.User, today time.Time, cycleLength int) DashboardUpcomingPrediction {
	prediction := DashboardUpcomingPrediction{
		NextPeriodStart:     stats.NextPeriodStart,
		OvulationDate:       stats.OvulationDate,
		OvulationExact:      stats.OvulationExact,
		OvulationImpossible: stats.OvulationImpossible,
	}

	if stats.LastPeriodStart.IsZero() || cycleLength <= 0 {
		return prediction
	}

	cycleStart, _, projectionOK := ProjectCycleStart(stats.LastPeriodStart, cycleLength, today)
	if !projectionOK {
		// codecov:ignore -- defensive: ProjectCycleStart only reports !ok for a zero
		// LastPeriodStart or non-positive cycleLength, both already returned above.
		return prediction
	}

	prediction.NextPeriodStart = AddCalendarDays(cycleStart, cycleLength, today.Location())
	window := PredictCycleWindow(cycleStart, cycleLength, stats.LutealPhase)
	// window.OvulationDate is a UTC-midnight date-only value while today is a
	// location-midnight working value, so the two are compared as calendar days
	// rather than as instants: local midnight in a UTC-minus zone falls hours
	// after UTC midnight of the same date, which read today's ovulation as past
	// and rolled the anchor a full cycle forward (issue #48 class).
	if window.Calculable && CalendarDaysBetween(window.OvulationDate, today) > 0 {
		cycleStart = ShiftCycleStartToFutureOvulation(cycleStart, window.OvulationDate, cycleLength, today)
		window = PredictCycleWindow(cycleStart, cycleLength, stats.LutealPhase)
	}
	if !window.Calculable {
		prediction.OvulationDate = time.Time{}
		prediction.OvulationExact = false
		prediction.OvulationImpossible = true
		return prediction
	}
	prediction.OvulationDate = window.OvulationDate
	prediction.OvulationExact = window.OvulationExact
	prediction.OvulationImpossible = false
	return prediction
}

func BuildDashboardCycleContext(user *models.User, logs []models.DailyLog, stats CycleStats, today time.Time, location *time.Location) DashboardCycleContext {
	// The tier is a property of the account's history, so it is resolved before
	// the suppression branches and carried by every one of them: a context that
	// reported "not awaiting" merely because predictions are off would disable
	// the gate for exactly the accounts with the least data.
	awaitingFirstCycle := DashboardAwaitingFirstCycle(stats)
	// Resolved beside the tier and carried by every branch below, for the same
	// reason: a context that answered "not suppressed" on a suppression branch
	// would hand the banner the very rule it is meant to be gated by.
	fertilitySuppressed := FertilityProjectionSuppressed(user, stats)
	if stats.PregnancyPaused {
		return DashboardCycleContext{
			CycleDayReference:   DashboardCycleReferenceLength(user, stats),
			PredictionDisabled:  true,
			PregnancyPaused:     true,
			AwaitingFirstCycle:  awaitingFirstCycle,
			FertilitySuppressed: fertilitySuppressed,
		}
	}
	if DashboardPredictionDisabled(user) {
		return DashboardCycleContext{
			CycleDayReference:   DashboardCycleReferenceLength(user, stats),
			CycleDayWarning:     false,
			CycleDataStale:      false,
			PredictionDisabled:  true,
			AwaitingFirstCycle:  awaitingFirstCycle,
			FertilitySuppressed: fertilitySuppressed,
		}
	}

	cycleDayReference := DashboardCycleReferenceLength(user, stats)
	cycleDayWarning := DashboardCycleDayLooksLong(stats.CurrentCycleDay, cycleDayReference)
	cycleStaleAnchor := DashboardCycleStaleAnchor(user, stats, location)
	cycleDataStale := DashboardCycleDataLooksStale(cycleStaleAnchor, today, cycleDayReference)
	display := buildDashboardPredictionDisplay(user, logs, stats, today, location)

	return DashboardCycleContext{
		CycleDayReference:           cycleDayReference,
		CycleDayWarning:             cycleDayWarning,
		LateCycle:                   BuildLateCycleNotice(user, stats, cycleDayWarning),
		CycleDataStale:              cycleDataStale,
		PredictionDisabled:          false,
		DisplayNextPeriodStart:      display.nextPeriodStart,
		DisplayNextPeriodEnd:        display.nextPeriodEnd,
		DisplayNextPeriodRangeStart: display.nextPeriodRangeStart,
		DisplayNextPeriodRangeEnd:   display.nextPeriodRangeEnd,
		DisplayNextPeriodUseRange:   display.nextPeriodUseRange,
		DisplayNextPeriodPrompt:     display.nextPeriodPrompt,
		DisplayNextPeriodNeedsData:  display.nextPeriodNeedsData,
		DisplayOvulationDate:        display.ovulationDate,
		DisplayOvulationRangeStart:  display.ovulationRangeStart,
		DisplayOvulationRangeEnd:    display.ovulationRangeEnd,
		DisplayOvulationUseRange:    display.ovulationUseRange,
		DisplayOvulationNeedsData:   display.ovulationNeedsData,
		DisplayOvulationConfirmed:   display.ovulationConfirmed,
		DisplayOvulationExact:       display.ovulationExact,
		DisplayOvulationImpossible:  display.ovulationImpossible,
		NextPeriodEstimatePaused:    display.estimatePaused,
		AwaitingFirstCycle:          awaitingFirstCycle,
		FertilitySuppressed:         fertilitySuppressed,
		NextPeriodInPast:            dashboardNextPeriodInPast(display, today),
		OvulationInPast:             dashboardOvulationInPast(display, today),
	}
}

// buildDashboardPredictionDisplay turns the projected cycle into the fields the
// dashboard renders, withholding the whole projected window once
// DashboardCycleOverdue reports the cycle is past its reference length by more
// than a week.
func buildDashboardPredictionDisplay(user *models.User, logs []models.DailyLog, stats CycleStats, today time.Time, location *time.Location) dashboardPredictionDisplay {
	prediction := DashboardUpcomingPredictions(
		stats,
		user,
		today,
		DashboardProjectionCycleLength(user, stats),
	)

	display := dashboardPredictionDisplay{
		nextPeriodStart:     prediction.NextPeriodStart,
		nextPeriodEnd:       dashboardNextPeriodEnd(prediction.NextPeriodStart, stats, location),
		nextPeriodPrompt:    stats.LastPeriodStart.IsZero(),
		nextPeriodNeedsData: dashboardNeedsNextPeriodData(user, stats, prediction.NextPeriodStart),
		ovulationDate:       prediction.OvulationDate,
		ovulationNeedsData:  dashboardNeedsOvulationData(user, stats),
		ovulationExact:      prediction.OvulationExact,
		ovulationImpossible: prediction.OvulationImpossible,
	}

	// A detected thermal shift CONFIRMS an ovulation that has already happened,
	// so the line names the day the temperatures point at — the same day the
	// calendar's solid marker and the stats chart name — rather than a projection
	// that observation has superseded. Both surfaces resolve it through
	// ConfirmedCurrentCycleOvulation so they cannot disagree.
	//
	// This substitution deliberately sits ABOVE the suppression branches and
	// changes only WHICH day is named, never whether a day is named at all:
	// whether a window may render at all belongs to the suppression gates, and a
	// confirmed observation must not become a way around one.
	// dashboardOvulationInPast then reads the substituted date, so a confirmed
	// ovulation already behind the owner is rendered as past instead of
	// announced as upcoming — which is the whole defect: on the projected day
	// itself the difference to today was zero, the anchor never shifted, and the
	// line declared an ovulation the temperatures had placed several days
	// earlier.
	if confirmed, ok := ConfirmedCurrentCycleOvulation(user, logs, stats, today, location); ok {
		display.ovulationDate = confirmed
		display.ovulationConfirmed = true
		// ovulationImpossible is the projection's claim that the account's
		// median cycle leaves no room for an ovulation, and the shift the owner
		// recorded is the observation that answers it (ResolveConfirmedCycleStats).
		display.ovulationImpossible = false
	}
	// The prompt is not a projection: with no recorded start there is no date
	// to withhold and nothing for the overdue signal to be about, so it answers
	// first — and DashboardCycleOverdue reads false there anyway, the cycle day
	// being derived from the same absent anchor.
	if display.nextPeriodPrompt {
		return finalizeDashboardPredictionDisplay(display)
	}
	// Suppression is the floor, so overdue outranks the thin-history branch
	// below rather than following it. Both describe a weak estimate, but they
	// answer with different strengths: nextPeriodNeedsData still NAMES the
	// projected date and captions it "needs more cycles", which is exactly the
	// qualifier the medical-safety invariant refuses to accept in place of
	// withholding. Ordered the other way round, the one cohort that met both —
	// an irregular account with fewer than three completed cycles, overdue —
	// was the only one still reading a date past its own reference length.
	if DashboardCycleOverdue(user, stats) {
		return pauseDashboardPredictionDisplay(display)
	}
	if display.nextPeriodNeedsData {
		return finalizeDashboardPredictionDisplay(display)
	}
	return finalizeDashboardPredictionDisplay(applyDashboardPredictionRanges(display, user, stats, location))
}

// pauseDashboardPredictionDisplay withholds the projected window once the
// running cycle is past the reference length by more than a week.
//
// DashboardUpcomingPredictions rolls the projection forward one whole cycle at a
// time (ProjectCycleStart), so it always yields a strictly future date: at cycle
// day 45 with a 28-day reference the header used to name the anchor plus 56 days
// as confidently as it names tomorrow. A cycle that is already overdue carries no
// evidence about when the next one starts, and presenting the roll-forward as a
// window is exactly the estimate-presented-as-fact the medical-safety invariant
// forbids. Both halves of the phantom projection go — the window and the
// ovulation date derived from it — while ovulationNeedsData and
// ovulationImpossible survive: they describe the account's data, not this
// projection, and other surfaces gate on them.
func pauseDashboardPredictionDisplay(display dashboardPredictionDisplay) dashboardPredictionDisplay {
	return dashboardPredictionDisplay{
		ovulationNeedsData:  display.ovulationNeedsData,
		ovulationImpossible: display.ovulationImpossible,
		estimatePaused:      true,
	}
}

func dashboardNeedsNextPeriodData(user *models.User, stats CycleStats, nextPeriodStart time.Time) bool {
	return user != nil && user.IrregularCycle && stats.CompletedCycleCount < 3 && !nextPeriodStart.IsZero()
}

func dashboardNeedsOvulationData(user *models.User, stats CycleStats) bool {
	return user != nil && user.IrregularCycle && stats.CompletedCycleCount < 3 && !stats.LastPeriodStart.IsZero()
}

func applyDashboardPredictionRanges(display dashboardPredictionDisplay, user *models.User, stats CycleStats, location *time.Location) dashboardPredictionDisplay {
	display.nextPeriodRangeStart, display.nextPeriodRangeEnd, display.nextPeriodUseRange = DashboardPredictionRange(
		user,
		stats,
		display.nextPeriodStart,
		location,
	)
	if !dashboardIrregularPredictionRangeEnabled(user, stats) {
		return display
	}
	// A confirmed ovulation outranks the range. The range expresses the SPREAD
	// of a projection, and there is no projection left to express once the
	// temperatures have named the day — it is built from cycle-length spread and
	// need not even contain that day. Discarding a measurement for it would
	// leave the dashboard and the calendar naming different things again, for
	// the cohort whose model is weakest. The next-period range above is
	// untouched: that projection is still a projection.
	if display.ovulationConfirmed {
		return display
	}
	display.ovulationRangeStart, display.ovulationRangeEnd, display.ovulationUseRange = DashboardOvulationRange(
		display.nextPeriodRangeStart,
		display.nextPeriodRangeEnd,
		stats.LutealPhase,
		location,
	)
	if display.ovulationUseRange {
		display.ovulationDate = time.Time{}
		display.ovulationExact = false
	}
	return display
}

func finalizeDashboardPredictionDisplay(display dashboardPredictionDisplay) dashboardPredictionDisplay {
	if !display.ovulationNeedsData || display.ovulationConfirmed {
		// "Needs more cycles" is about a projection built on thin history. A
		// detected thermal shift is not that projection, and the calendar gates
		// the same signal on FertilityProjectionSuppressed alone — which this
		// cohort (irregular, one or two completed cycles) does not meet, so
		// withholding here while the grid marks the day is the same divergence
		// this pair of surfaces was just brought into agreement over.
		//
		// Keeping the date is only half of it: the dashboard template tests
		// DisplayOvulationNeedsData BEFORE the branch that names a date, so the
		// caption wins over any date this function leaves behind. The template
		// therefore reads DisplayOvulationConfirmed alongside it. Regression:
		// TestDashboardNamesTheConfirmedDayForTheThinHistoryCohort renders the
		// page rather than reading the context, which is what a context-level
		// assertion could not tell apart.
		return display
	}
	display.ovulationDate = time.Time{}
	display.ovulationExact = false
	return display
}

func dashboardNextPeriodEnd(nextPeriodStart time.Time, stats CycleStats, location *time.Location) time.Time {
	if nextPeriodStart.IsZero() {
		return time.Time{}
	}

	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	if periodLength <= 0 {
		return time.Time{}
	}

	return AddCalendarDays(nextPeriodStart, periodLength-1, location)
}

func dashboardNextPeriodInPast(display dashboardPredictionDisplay, today time.Time) bool {
	return display.nextPeriodUseRange && !display.nextPeriodRangeEnd.IsZero() && display.nextPeriodRangeEnd.Before(today)
}

func dashboardOvulationInPast(display dashboardPredictionDisplay, today time.Time) bool {
	// The amber notice is about a PROJECTION the model still points at after the
	// day has gone by. An ovulation inferred from the temperature shift being
	// behind the owner is the normal
	// state of every cycle from the shift until the next period, so reading it as
	// that notice would raise a standing false alarm — for about a fortnight per
	// cycle, on exactly the accounts whose data is best. DashboardUpcomingPredictions
	// rolls a projected ovulation forward the moment it is past, which is why this
	// branch had no other way to be true before the confirmed day reached it.
	if display.ovulationConfirmed {
		return false
	}
	if display.ovulationUseRange {
		// Both bounds come from DashboardOvulationRange, which builds them with
		// CalendarDay in the request location, so this pair already shares
		// today's midnight shape and compares directly.
		return !display.ovulationRangeEnd.IsZero() && display.ovulationRangeEnd.Before(today)
	}
	// ovulationDate is the PredictCycleWindow output — a UTC-midnight date-only
	// value — while today is a location midnight, so the two are compared as
	// calendar days, exactly as the shift guard in DashboardUpcomingPredictions
	// that decides this same date already does. As instants, local midnight in a
	// UTC-minus zone falls hours after UTC midnight of the same date, which read
	// the ovulation day itself as past and printed the amber "date is already in
	// the past" notice beside the date the header had just named (issue #48
	// class).
	return !display.ovulationImpossible && !display.ovulationDate.IsZero() && CalendarDaysBetween(display.ovulationDate, today) > 0
}

func CompletedCycleTrendLengths(logs []models.DailyLog, now time.Time, location *time.Location) []int {
	starts := DetectCycleStarts(logs)
	if len(starts) < 2 {
		return nil
	}

	today := DateAtLocation(now, location)
	lengths := make([]int, 0, len(starts)-1)
	for index := 1; index < len(starts); index++ {
		previousStart := CalendarDay(starts[index-1], location)
		currentStart := CalendarDay(starts[index], location)
		if !currentStart.Before(today) {
			break
		}
		lengths = append(lengths, CalendarDaysBetween(previousStart, currentStart))
	}
	return lengths
}
