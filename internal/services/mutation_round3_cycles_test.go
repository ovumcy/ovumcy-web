package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// mr3cycDay builds a date-only UTC value for the given calendar date.
func mr3cycDay(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// mr3cycPeriodLog builds a period DailyLog on the given day.
func mr3cycPeriodLog(day time.Time, cycleStart bool, uncertain bool) models.DailyLog {
	return models.DailyLog{
		Date:        day,
		IsPeriod:    true,
		CycleStart:  cycleStart,
		IsUncertain: uncertain,
	}
}

// mr3cycCluster appends `days` consecutive period days starting at `start`.
func mr3cycCluster(logs []models.DailyLog, start time.Time, days int, cycleStart bool, uncertain bool) []models.DailyLog {
	for i := 0; i < days; i++ {
		day := start.AddDate(0, 0, i)
		// Only the first day of a cluster carries the cycle-start / uncertain flags.
		logs = append(logs, mr3cycPeriodLog(day, cycleStart && i == 0, uncertain && i == 0))
	}
	return logs
}

// TestMR3Cycles_ObservedStartsNotOverwrittenByDetected targets
// cycles.go:64 `if len(observedStarts) == 0 { observedStarts = detectedStarts }`
// (CONDITIONALS_NEGATION). Three clusters are logged; the middle cluster is an
// explicit-but-uncertain cycle start, which ObservedCycleStarts deliberately
// skips while DetectCycleStarts includes it. The observed list is therefore
// non-empty (2 starts -> 1 completed cycle) and must NOT be overwritten by the
// detected list (3 starts -> 2 completed cycles). Under the `!=` mutation the
// non-empty observed list is wrongly replaced by the detected list and the
// completed-cycle count / median diverge.
func TestMR3Cycles_ObservedStartsNotOverwrittenByDetected(t *testing.T) {
	logs := []models.DailyLog{}
	// Cluster A: regular start on Jan 1.
	logs = mr3cycCluster(logs, mr3cycDay(2026, time.January, 1), 4, false, false)
	// Cluster B: ~30 days later, explicit cycle start but UNCERTAIN -> skipped
	// by ObservedCycleStarts, included by DetectCycleStarts.
	logs = mr3cycCluster(logs, mr3cycDay(2026, time.January, 31), 4, true, true)
	// Cluster C: another ~30 days later, regular start.
	logs = mr3cycCluster(logs, mr3cycDay(2026, time.March, 2), 4, false, false)

	now := mr3cycDay(2026, time.March, 10)
	stats := BuildCycleStats(logs, now)

	// Observed starts = {Jan 1, Mar 2} -> 1 completed cycle of 60 days.
	if stats.CompletedCycleCount != 1 {
		t.Fatalf("expected 1 completed cycle from observed starts, got %d", stats.CompletedCycleCount)
	}
	// Jan 1 -> Mar 2 2026 is 60 days.
	if stats.MedianCycleLength != 60 {
		t.Fatalf("expected median cycle length 60 from observed starts, got %d", stats.MedianCycleLength)
	}
}

// TestMR3Cycles_CalcOvulationDayMinCycleBoundary pins the observable
// CalcOvulationDay contract around the minimum-cycle threshold. NOTE on
// cycles.go:101 `if cycleLen < minLutealPhaseDays+minOvulationCycleDay` (+ -> -):
// that mutant is EQUIVALENT. Line 101's threshold (cycleLen < 15) is identical
// to the redundant downstream guard at line 108 (`maxSupportedLutealPhase <
// minLutealPhaseDays`, i.e. cycleLen-5 < 10 == cycleLen < 15). Under the `-`
// mutation (threshold 5) every cycleLen in 5..14 that slips past line 101 is
// still rejected verbatim by line 108, so the observable (day, ok) output is
// byte-identical across the whole domain (verified by exhaustive probe 1..30).
// This test does not attempt to kill that mutant; it documents the real contract.
func TestMR3Cycles_CalcOvulationDayMinCycleBoundary(t *testing.T) {
	// Below 15 there is no room for both the 5-day minimum follicular floor and
	// the 10-day minimum luteal phase, so ovulation is non-calculable.
	if day, ok := CalcOvulationDay(14, defaultLutealPhaseDays); ok || day != 0 {
		t.Fatalf("cycle length 14 must be non-calculable, got day=%d ok=%v", day, ok)
	}
	// A 28-day cycle with the 14-day default luteal phase ovulates on day 14
	// (exact).
	if day, ok := CalcOvulationDay(28, defaultLutealPhaseDays); !ok || day != 14 {
		t.Fatalf("28-day cycle must ovulate exactly on day 14, got day=%d ok=%v", day, ok)
	}
}

// TestMR3Cycles_PredictCycleWindowZeroCycleLength pins the PredictCycleWindow
// contract: cycleLength==0 must yield calculable=false. NOTE on cycles.go:124
// BOUNDARY `cycleLength <= 0` (-> `< 0`): that mutant is EQUIVALENT. The only
// input it changes is cycleLength==0, which then reaches CalcOvulationDay(0,..)
// -> `0 < 15` -> (0,false), so the downstream guard `ovulationDay <= 0` at
// line 128 rejects it verbatim. Observable output is unchanged. This test still
// pins the user-facing contract regardless.
func TestMR3Cycles_PredictCycleWindowZeroCycleLength(t *testing.T) {
	periodStart := mr3cycDay(2026, time.March, 10)
	_, _, _, _, calculable := PredictCycleWindow(periodStart, 0, defaultLutealPhaseDays)
	if calculable {
		t.Fatalf("cycleLength==0 must yield calculable=false")
	}
}

// TestMR3Cycles_CycleLengthSpreadMaxZero pins the CycleLengthSpread contract:
// MaxCycleLength==0 with a positive Min yields spread 0. NOTE on cycles.go:505
// BOUNDARY `stats.MaxCycleLength <= 0` (-> `< 0`): EQUIVALENT. With Max==0 the
// only way the second clause matters is Min>0, but then the third clause
// `MaxCycleLength < MinCycleLength` (0 < Min) already returns 0; and if Min<=0
// the first clause returns 0. No input isolates the Max guard at 0, so output
// is unchanged. This test still pins the user-facing contract.
func TestMR3Cycles_CycleLengthSpreadMaxZero(t *testing.T) {
	stats := CycleStats{MinCycleLength: 5, MaxCycleLength: 0}
	if got := CycleLengthSpread(stats); got != 0 {
		t.Fatalf("MaxCycleLength==0 must yield spread 0, got %d", got)
	}
}

// TestMR3Cycles_ResolveLutealPhaseBoundaries targets
// cycles.go:87 `case value <= 0` and cycles.go:89 `case value < minLutealPhaseDays`.
// value==0 must return the default (14); value just below the min must clamp to
// the min; value at the min must pass through unchanged. Under the `<` mutation
// at line 87, value==0 would fall through to the clamp branch and return 10
// instead of 14. Under the `<=` mutation at line 89, value==min(10) would be
// clamped (a no-op for 10) — discriminated instead by asserting value==9 clamps
// to 10 while value==10 passes through, and that the default branch differs.
func TestMR3Cycles_ResolveLutealPhaseBoundaries(t *testing.T) {
	if got := ResolveLutealPhase(0); got != defaultLutealPhaseDays {
		t.Fatalf("ResolveLutealPhase(0) must be default %d, got %d", defaultLutealPhaseDays, got)
	}
	// Negative also hits the <=0 default branch, distinct from the clamp value.
	if got := ResolveLutealPhase(-3); got != defaultLutealPhaseDays {
		t.Fatalf("ResolveLutealPhase(-3) must be default %d, got %d", defaultLutealPhaseDays, got)
	}
	// Just below the min clamps up to the min.
	if got := ResolveLutealPhase(minLutealPhaseDays - 1); got != minLutealPhaseDays {
		t.Fatalf("ResolveLutealPhase(%d) must clamp to %d, got %d",
			minLutealPhaseDays-1, minLutealPhaseDays, got)
	}
	// At the min passes through.
	if got := ResolveLutealPhase(minLutealPhaseDays); got != minLutealPhaseDays {
		t.Fatalf("ResolveLutealPhase(%d) must pass through, got %d", minLutealPhaseDays, got)
	}
	// Above the min passes through unchanged.
	if got := ResolveLutealPhase(13); got != 13 {
		t.Fatalf("ResolveLutealPhase(13) must pass through, got %d", got)
	}
}

// TestMR3Cycles_LatestExplicitCycleStartNilLocation targets
// cycle_anchor.go:25 `if location == nil` (NEGATION) in
// latestExplicitCycleStartBeforeOrOn (reached via the exported
// LatestCycleStartAnchorBeforeOrOn). A nil location must not panic and must fall
// back to UTC, returning the explicit cycle start. Under the NEGATION mutation
// (`location != nil`), a nil location is left nil and CalendarDay / DateAtLocation
// downstream would receive nil — exercised here to ensure correct fallback.
func TestMR3Cycles_LatestExplicitCycleStartNilLocation(t *testing.T) {
	logs := []models.DailyLog{
		mr3cycPeriodLog(mr3cycDay(2026, time.March, 1), true, false),
	}
	day := mr3cycDay(2026, time.March, 10)

	var result time.Time
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nil location must not panic: %v", r)
			}
		}()
		// user == nil so the anchor comes purely from the explicit log start.
		result = LatestCycleStartAnchorBeforeOrOn(nil, logs, day, nil)
	}()

	if !sameCalendarDay(result, mr3cycDay(2026, time.March, 1)) {
		t.Fatalf("expected explicit cycle start 2026-03-01, got %v", result)
	}
}

// TestMR3Cycles_LatestExplicitCycleStartHonorsLocation also targets
// cycle_anchor.go:25. The NEGATION mutation (`location != nil`) leaves a
// passed-in non-nil location intact only when it is nil and otherwise clobbers
// it to UTC. With a UTC+2 location and a same-day explicit cycle start anchored
// at that location's calendar day, the explicit start is on-or-before the
// target and must be returned. Clobbering the location to UTC shifts the target
// day one calendar day earlier, pushing the explicit start into the future so
// it is wrongly skipped and a zero anchor is returned.
func TestMR3Cycles_LatestExplicitCycleStartHonorsLocation(t *testing.T) {
	loc := time.FixedZone("UTCplus2", 2*60*60)
	// Explicit cycle start stored as the calendar date 2026-03-11.
	logs := []models.DailyLog{
		mr3cycPeriodLog(mr3cycDay(2026, time.March, 11), true, false),
	}
	// `day` is an instant that, projected into UTC+2, lands on 2026-03-11.
	day := time.Date(2026, time.March, 11, 1, 0, 0, 0, loc)

	result := LatestCycleStartAnchorBeforeOrOn(nil, logs, day, loc)
	if !sameCalendarDay(result, mr3cycDay(2026, time.March, 11)) {
		t.Fatalf("expected explicit cycle start 2026-03-11 honoring UTC+2, got %v", result)
	}
}

// TestMR3Cycles_LatestCycleStartAnchorNilLocation targets
// cycle_anchor.go:10 `if location == nil` (NEGATION) in
// latestCycleStartAnchorBeforeOrOn. Driven through the user anchor path with a
// nil location: the user's LastPeriodStart must be returned without panic.
// Under the NEGATION mutation a nil location stays nil and the location is never
// defaulted to UTC.
func TestMR3Cycles_LatestCycleStartAnchorNilLocation(t *testing.T) {
	lastPeriod := mr3cycDay(2026, time.February, 20)
	user := &models.User{
		Role:            models.RoleOwner,
		LastPeriodStart: &lastPeriod,
	}
	day := mr3cycDay(2026, time.March, 10)

	var result time.Time
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nil location must not panic: %v", r)
			}
		}()
		// No logs -> explicit start is zero; anchor comes from the user value.
		result = LatestCycleStartAnchorBeforeOrOn(user, nil, day, nil)
	}()

	if !sameCalendarDay(result, lastPeriod) {
		t.Fatalf("expected user anchor 2026-02-20, got %v", result)
	}
}

// TestMR3Cycles_LatestCycleStartAnchorHonorsLocation also targets
// cycle_anchor.go:10. The NEGATION mutation (`location != nil`) clobbers any
// non-nil location to UTC. With a UTC+2 location and a user anchor on the same
// calendar day as the localized target, the anchor is on-or-before the target
// and must be returned. Clobbering to UTC moves the target one calendar day
// earlier so the anchor looks like it is in the future and is dropped, yielding
// a zero anchor.
func TestMR3Cycles_LatestCycleStartAnchorHonorsLocation(t *testing.T) {
	loc := time.FixedZone("UTCplus2", 2*60*60)
	lastPeriod := mr3cycDay(2026, time.March, 11)
	user := &models.User{
		Role:            models.RoleOwner,
		LastPeriodStart: &lastPeriod,
	}
	// `day` projected into UTC+2 lands on 2026-03-11.
	day := time.Date(2026, time.March, 11, 1, 0, 0, 0, loc)

	result := LatestCycleStartAnchorBeforeOrOn(user, nil, day, loc)
	if !sameCalendarDay(result, mr3cycDay(2026, time.March, 11)) {
		t.Fatalf("expected user anchor 2026-03-11 honoring UTC+2, got %v", result)
	}
}

// TestMR3Cycles_DetectCurrentPhaseNilLocation targets
// cycle_baseline.go:121 `if location == nil` (NEGATION) in DetectCurrentPhase.
// A nil location must fall back to UTC without panic and still classify the
// phase. With period logged today the phase is "menstrual". Under the NEGATION
// mutation the nil location is left nil and downstream CalendarDay calls would
// be reached with nil.
func TestMR3Cycles_DetectCurrentPhaseNilLocation(t *testing.T) {
	today := mr3cycDay(2026, time.March, 10)
	logs := []models.DailyLog{
		mr3cycPeriodLog(today, true, false),
	}
	stats := CycleStats{
		LastPeriodStart:     today,
		AveragePeriodLength: 5,
	}

	var phase string
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("nil location must not panic: %v", r)
			}
		}()
		phase = DetectCurrentPhase(stats, logs, today, nil)
	}()

	if phase != "menstrual" {
		t.Fatalf("expected menstrual phase with period logged today, got %q", phase)
	}
}

// TestMR3Cycles_DetectCurrentPhaseHonorsLocation also targets
// cycle_baseline.go:121. The NEGATION mutation (`location != nil`) clobbers a
// non-nil location to UTC, which shifts the location-rebuilt period-end and
// fertility-window dates relative to the UTC-midnight `today`. At the
// period-end boundary (LastPeriodStart Mar 1 + 5-day period = Mar 5) under a
// far-east UTC+14 location, Mar 5 (UTC midnight) falls just AFTER the
// location-midnight period end, so the phase is follicular. Clobbering the
// location to UTC pulls the period end back to UTC midnight, making Mar 5 land
// inside the menstrual window — a wrong "menstrual" classification.
func TestMR3Cycles_DetectCurrentPhaseHonorsLocation(t *testing.T) {
	loc := time.FixedZone("UTCplus14", 14*60*60)
	stats := CycleStats{
		LastPeriodStart:      mr3cycDay(2026, time.March, 1),
		AveragePeriodLength:  5,
		OvulationDate:        mr3cycDay(2026, time.March, 14),
		FertilityWindowStart: mr3cycDay(2026, time.March, 9),
		FertilityWindowEnd:   mr3cycDay(2026, time.March, 14),
	}
	// today = Mar 5 (UTC midnight), the day after the 5-day menstrual window
	// ends in a far-east locale; no period is logged on this day.
	today := mr3cycDay(2026, time.March, 5)

	phase := DetectCurrentPhase(stats, nil, today, loc)
	if phase != "follicular" {
		t.Fatalf("expected follicular phase honoring UTC+14, got %q", phase)
	}
}

// TestMR3Cycles_PotentialImplantationZeroCycleLengthFallback pins
// implantation detection for the no-observed-data path. NOTE on
// cycle_start_policy.go:83 BOUNDARY `if cycleLength <= 0` (-> `< 0`): that
// mutant is EQUIVALENT because the guard is effectively dead.
// predictedCycleLength never returns <= 0 — its final fallback returns
// models.DefaultCycleLength (28) — so cycleLength is always positive here and
// the DashboardCycleReferenceLength branch is unreachable in practice. The
// `<= 0` vs `< 0` distinction therefore has no observable effect. This test
// still pins the real behavior: implantation detection fires on the no-data
// path (using the default 28-day cycle that predictedCycleLength supplies).
func TestMR3Cycles_PotentialImplantationZeroCycleLengthFallback(t *testing.T) {
	location := time.UTC
	// Owner with a configured 28-day cycle and an explicit last period start,
	// but NO daily logs -> no observed cycle lengths -> predictedCycleLength==0.
	lastPeriod := mr3cycDay(2026, time.March, 1)
	user := &models.User{
		Role:            models.RoleOwner,
		CycleLength:     28,
		PeriodLength:    5,
		LutealPhase:     14,
		LastPeriodStart: &lastPeriod,
	}

	// Ovulation for a 28-day cycle anchored Mar 1 falls on cycle day 14 ->
	// Mar 14. An implantation candidate sits 6-12 days after ovulation; pick a
	// target day 8 days after ovulation (Mar 22).
	target := mr3cycDay(2026, time.March, 22)
	now := mr3cycDay(2026, time.March, 22)

	policy := ResolveManualCycleStartPolicy(user, nil, target, now, location)
	if !policy.PotentialImplantation {
		t.Fatalf("expected implantation detection via cycle-length fallback, got none (gap=%d)",
			policy.ImplantationGapDays)
	}
}
