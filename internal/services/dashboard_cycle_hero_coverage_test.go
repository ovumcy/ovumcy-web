package services

// dashboard_cycle_hero_coverage_test.go — behavior tests for the dashboard
// hero ribbon geometry and render gating in dashboard_cycle_hero.go (per-day
// cells, the axis, phase fallbacks, invisible-render guards). Written to kill
// surviving mutants (gremlins). The "dashboardcycleheroCov" prefix guards this
// file's helpers/types against package-wide collisions.

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func dashboardcycleheroCovUser28() *models.User {
	return &models.User{Role: models.RoleOwner, CycleLength: 28}
}

func dashboardcycleheroCovBaseStats() CycleStats {
	return CycleStats{
		CurrentCycleDay:     7,
		CurrentPhase:        "follicular",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
}

func dashboardcycleheroCovExactContext() DashboardCycleContext {
	return DashboardCycleContext{DisplayOvulationExact: true}
}

// ---------------------------------------------------------------------------
// SURVIVING — line 55: periodLength >= cycleLength guard
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroPeriodLengthEqualsCycleLengthReturnsInvisible tests
// the exact-equality boundary: if period == cycle, the hero must be invisible.
// A mutant that weakens >= to > would expose the hero when they are equal.
func TestDashboardCycleHeroPeriodLengthEqualsCycleLengthReturnsInvisible(t *testing.T) {
	// AveragePeriodLength 20.5 rounds to 21 (predictedPeriodLength adds 0.5 before truncating).
	// cycleLength == 21 (AverageCycleLength 21.0 rounds to 21 via DashboardCycleReferenceLength).
	user := &models.User{Role: models.RoleOwner, CycleLength: 21}
	stats := CycleStats{
		CurrentCycleDay:     5,
		AveragePeriodLength: 20.5, // predictedPeriodLength → 21
		AverageCycleLength:  21,   // reference length → 21
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if hero.Visible {
		t.Fatal("expected invisible hero when predictedPeriodLength == cycleLength")
	}
}

// TestDashboardcycleheroCovPeriodLengthOneLessThanCycleLengthAllowsRender tests
// that periodLength == cycleLength-1 still blocks rendering (ovulation guard
// will reject it), while a slightly smaller period does succeed.
// The key assertion: the hero DOES NOT render when period+1 == cycleLength
// because no room for an ovulation day plus a luteal phase.
func TestDashboardCycleHeroPeriodLengthOneLessThanCycleLengthIsGated(t *testing.T) {
	// cycleLength=17, periodLength=16 → period < cycle (passes guard at line 55)
	// ovulationDay for cycle=17, luteal=14 would be 3, which is ≤ periodLength+1=17,
	// so it will be rejected at the ovulation guard. Hero invisible is correct.
	user := &models.User{Role: models.RoleOwner, CycleLength: 17}
	stats := CycleStats{
		CurrentCycleDay:     3,
		AveragePeriodLength: 15.5, // predictedPeriodLength → 16
		AverageCycleLength:  17,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if hero.Visible {
		t.Fatal("expected invisible hero when period nearly fills cycle leaving no room for valid ovulation")
	}
}

// ---------------------------------------------------------------------------
// SURVIVING — line 60: ovulationDay <= periodLength+1 || ovulationDay > cycleLength
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroOvulationDayEqualsPerioLengthPlusOneReturnsInvisible
// pins the exact-equality boundary on the left side of the OR.
// Mutant: change <= to < makes the equality case slip through.
func TestDashboardCycleHeroOvulationDayEqualsPerioLengthPlusOneReturnsInvisible(t *testing.T) {
	// cycle=28, luteal=22 → ovDay = 28-22 = 6; periodLength=5 → 6 == 5+1 → invisible
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	stats := CycleStats{
		CurrentCycleDay:     3,
		AveragePeriodLength: 5,
		AverageCycleLength:  28,
		LutealPhase:         22, // ovulationDay = 28-22 = 6; periodLength+1 = 6
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if hero.Visible {
		t.Fatalf("expected invisible hero when ovulationDay == periodLength+1 (day %d)", 6)
	}
}

// TestDashboardCycleHeroOvulationDayOneMoreThanPeriodPlusOneRendersOK pins
// the other side of the boundary: ovDay = periodLength+2 must allow rendering.
func TestDashboardCycleHeroOvulationDayOneMoreThanPeriodPlusOneRendersOK(t *testing.T) {
	// cycle=28, luteal=21 → ovDay = 28-21 = 7; periodLength=5 → 7 > 5+1 → allowed
	user := &models.User{Role: models.RoleOwner, CycleLength: 28}
	stats := CycleStats{
		CurrentCycleDay:     4,
		AveragePeriodLength: 5,
		AverageCycleLength:  28,
		LutealPhase:         21, // ovulationDay = 7; periodLength+1 = 6 → 7 > 6 OK
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("expected visible hero when ovulationDay is just past the period+1 boundary")
	}
}

// The right-hand side of the same OR — `ovulationDay > cycleLength` in
// dashboardCycleHero's visibility guard — is deliberately left without a test.
// It is unreachable: CalcOvulationDay returns `cycleLen - resolvedLutealPhase`,
// and ResolveLutealPhase never yields less than minLutealPhaseDays, so the
// ovulation day is at most `cycleLength - minLutealPhaseDays` — always well below
// cycleLength. (The failure returns of CalcOvulationDay give 0, which the
// left-hand side of the OR catches.) Manufacturing the state the guard defends
// against would take a negative luteal phase, which no input path produces, so a
// test for it would assert an impossible world. A test named for this boundary
// used to stand here and check nothing — a bare skip under a name claiming an
// asserted invariant, which is worse than its absence because it read as
// coverage. The guard stays as defence in depth; its boundary mutant is an
// equivalent mutant and is documented here instead of killed.

// ---------------------------------------------------------------------------
// Ribbon: today splits recorded from projected
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroTodayCellSplitsRecordedFromProjected pins the one
// boundary the whole ribbon rests on: exactly one cell is today, every earlier
// cell is recorded and every later one is projected. A mutant flipping the
// comparison (day > currentDay → day >= currentDay) paints today itself as an
// estimate, which is the fact-vs-estimate line the calendar textures encode.
func TestDashboardCycleHeroTodayCellSplitsRecordedFromProjected(t *testing.T) {
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     7,
		CurrentPhase:        "follicular",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}

	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("expected visible hero for setup")
	}

	todayCells := 0
	for _, day := range hero.Days {
		if day.IsToday {
			todayCells++
			if day.Day != 7 {
				t.Fatalf("expected today on cycle day 7, got %d", day.Day)
			}
			if day.IsProjected {
				t.Fatal("today is a recorded day, never a projected one")
			}
			continue
		}
		if wantProjected := day.Day > 7; day.IsProjected != wantProjected {
			t.Fatalf("day %d: expected projected=%v, got %v", day.Day, wantProjected, day.IsProjected)
		}
	}
	if todayCells != 1 {
		t.Fatalf("expected exactly one today cell, got %d", todayCells)
	}
}

// ---------------------------------------------------------------------------
// SURVIVING — lines 83-85: canRenderDashboardCycleHero boundary conditions
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroCycleLengthZeroBlocksRender pins line 83: cycleLength > 0.
// DashboardCycleReferenceLength always returns a positive value (defaults to 28),
// so line 83 is only exercisable by calling canRenderDashboardCycleHero directly.
// A mutant changing > to >= would make cycleLength==0 pass, but also cycleLength==1 fail.
// We cover the contract directly.
func TestDashboardCycleHeroCycleLengthZeroBlocksRender(t *testing.T) {
	// canRenderDashboardCycleHero with cycleLength=0 must return false.
	stats := CycleStats{CurrentCycleDay: 3}
	ctx := DashboardCycleContext{DisplayOvulationExact: true}
	if canRenderDashboardCycleHero(0, stats, ctx) {
		t.Fatal("canRenderDashboardCycleHero must return false for cycleLength=0")
	}
}

// TestDashboardCycleHeroCycleLengthPositiveAllowsRender ensures that a positive
// cycleLength (e.g. 1) is not incorrectly blocked — kills the >= mutation.
func TestDashboardCycleHeroCycleLengthPositiveAllowsRender(t *testing.T) {
	stats := CycleStats{CurrentCycleDay: 1}
	ctx := DashboardCycleContext{DisplayOvulationExact: true}
	// Should pass the cycleLength check (returns true for that condition alone).
	// cycleLength=28 with all other flags default — should be renderable if no flags set.
	if !canRenderDashboardCycleHero(28, stats, ctx) {
		t.Fatal("canRenderDashboardCycleHero must return true for cycleLength=28, day=1, no flags")
	}
}

// TestDashboardCycleHeroCurrentCycleDayZeroReturnsInvisible pins line 84: CurrentCycleDay > 0.
// Mutant: change > to >= would reject day==1 (valid first day) as invisible.
func TestDashboardCycleHeroCurrentCycleDayZeroReturnsInvisible(t *testing.T) {
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     0, // zero → must be invisible
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if hero.Visible {
		t.Fatal("expected invisible hero when CurrentCycleDay == 0")
	}
}

// TestDashboardCycleHeroCurrentCycleDayOneRendersVisible pins the other side:
// day==1 must render (kills the mutant that would flip > to >=).
func TestDashboardCycleHeroCurrentCycleDayOneRendersVisible(t *testing.T) {
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     1,
		CurrentPhase:        "menstrual",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("expected visible hero for day 1 (first day of cycle)")
	}
}

// TestDashboardCycleHeroCurrentCycleDayExceedsCycleLengthReturnsInvisible pins
// line 85: CurrentCycleDay <= cycleLength.
// Mutant: change <= to < makes day==cycleLength invisible when it should render.
func TestDashboardCycleHeroCurrentCycleDayExceedsCycleLengthReturnsInvisible(t *testing.T) {
	user := dashboardcycleheroCovUser28() // cycleLength=28
	stats := CycleStats{
		CurrentCycleDay:     29, // > 28 → must be invisible
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if hero.Visible {
		t.Fatal("expected invisible hero when CurrentCycleDay > cycleLength")
	}
}

// TestDashboardCycleHeroCurrentCycleDayEqualsLengthRendersVisible pins the
// equality side: day == cycleLength must render.
func TestDashboardCycleHeroCurrentCycleDayEqualsLengthRendersVisible(t *testing.T) {
	user := dashboardcycleheroCovUser28() // cycleLength=28
	stats := CycleStats{
		CurrentCycleDay:     28, // == cycleLength → must render
		CurrentPhase:        "luteal",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("expected visible hero when CurrentCycleDay == cycleLength (last day)")
	}
	if hero.CurrentDay != 28 {
		t.Fatalf("expected CurrentDay 28, got %d", hero.CurrentDay)
	}
}

// ---------------------------------------------------------------------------
// SURVIVING — lines 133-134: segment day-count and dash calculation
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroDayCellsCarryTheirPhaseSpan asserts that every axis day
// falls under the phase whose span contains it, with the counts the phase cards
// declare. A mutant loosening either bound in dashboardCycleHeroPhaseForDay
// moves a boundary day into the neighbouring phase, which is exactly the drift
// the shared encoding contract exists to prevent.
func TestDashboardCycleHeroDayCellsCarryTheirPhaseSpan(t *testing.T) {
	// cycle=28, period=5, ovulation=14
	// menstrual: days 1-5 → 5 days
	// follicular: days 6-13 → 8 days
	// ovulation: days 14-14 → 1 day
	// luteal: days 15-28 → 14 days
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     3,
		CurrentPhase:        "menstrual",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{})
	if !hero.Visible {
		t.Fatal("expected visible hero for ribbon test setup")
	}
	if len(hero.Days) != 28 {
		t.Fatalf("expected 28 day cells, got %d", len(hero.Days))
	}

	counts := map[string]int{}
	for index, day := range hero.Days {
		if day.Day != index+1 {
			t.Fatalf("cell %d carries day %d — the ribbon must run in cycle-day order", index, day.Day)
		}
		counts[day.Phase]++
	}

	for phase, want := range map[string]int{"menstrual": 5, "follicular": 8, "ovulation": 1, "luteal": 14} {
		if counts[phase] != want {
			t.Fatalf("expected %d %s days, got %d", want, phase, counts[phase])
		}
	}
	if hero.Days[4].Phase != "menstrual" || hero.Days[5].Phase != "follicular" {
		t.Fatalf("day 5/6 boundary is wrong: %q then %q", hero.Days[4].Phase, hero.Days[5].Phase)
	}
	if hero.Days[13].Phase != "ovulation" || hero.Days[14].Phase != "luteal" {
		t.Fatalf("day 14/15 boundary is wrong: %q then %q", hero.Days[13].Phase, hero.Days[14].Phase)
	}
}

// TestDashboardCycleHeroFertileWindowRidesTheFirstCycleGate asserts the ribbon
// obeys the same suppression the status header does: before the first completed
// cycle the fertile window is the onboarding slider projected forward, so no
// cell may carry it. Once a cycle is observed the window shades the days the
// calendar shades, with the ovulation date as its peak.
func TestDashboardCycleHeroFertileWindowRidesTheFirstCycleGate(t *testing.T) {
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	stats := CycleStats{
		CurrentCycleDay:      7,
		CurrentPhase:         "follicular",
		AveragePeriodLength:  5,
		LutealPhase:          14,
		CompletedCycleCount:  4,
		LastPeriodStart:      cycleStart,
		FertilityWindowStart: cycleStart.AddDate(0, 0, 8),  // day 9
		FertilityWindowEnd:   cycleStart.AddDate(0, 0, 14), // day 15
		OvulationDate:        cycleStart.AddDate(0, 0, 13), // day 14
	}
	input := dashboardCycleHeroInput{Today: cycleStart.AddDate(0, 0, 6), Location: time.UTC}

	shown := BuildDashboardCycleHero(dashboardcycleheroCovUser28(), stats, dashboardcycleheroCovExactContext(), input)
	if !shown.Visible {
		t.Fatal("expected visible hero for fertile-window setup")
	}

	fertileDays := []int{}
	peakDays := []int{}
	for _, day := range shown.Days {
		if day.IsFertile {
			fertileDays = append(fertileDays, day.Day)
		}
		if day.IsFertilePeak {
			peakDays = append(peakDays, day.Day)
		}
	}
	if len(fertileDays) != 7 || fertileDays[0] != 9 || fertileDays[6] != 15 {
		t.Fatalf("expected fertile days 9-15, got %v", fertileDays)
	}
	if len(peakDays) != 1 || peakDays[0] != 14 {
		t.Fatalf("expected a single peak on day 14, got %v", peakDays)
	}

	awaiting := dashboardcycleheroCovExactContext()
	awaiting.AwaitingFirstCycle = true
	withheld := BuildDashboardCycleHero(dashboardcycleheroCovUser28(), stats, awaiting, input)
	for _, day := range withheld.Days {
		if day.IsFertile || day.IsFertilePeak {
			t.Fatalf("day %d shades a fertile window before the first completed cycle", day.Day)
		}
	}
}

// TestDashboardCycleHeroStartWindowExtendsTheAxis asserts the graded tail: the
// days the next period may start on are read from DashboardPredictionRange —
// the definition the calendar and the status line already use — and the axis
// grows to reach them, since a window drawn only as far as the average cycle
// length would hide its own upper half.
func TestDashboardCycleHeroStartWindowExtendsTheAxis(t *testing.T) {
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, IrregularCycle: true}
	stats := CycleStats{
		CurrentCycleDay:     7,
		CurrentPhase:        "follicular",
		AveragePeriodLength: 5,
		LutealPhase:         14,
		AverageCycleLength:  28,
		MedianCycleLength:   28,
		MinCycleLength:      26,
		MaxCycleLength:      33,
		CompletedCycleCount: 4,
		LastPeriodStart:     cycleStart,
		NextPeriodStart:     cycleStart.AddDate(0, 0, 28),
	}

	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{
		Today:    cycleStart.AddDate(0, 0, 6),
		Location: time.UTC,
	})
	if !hero.Visible {
		t.Fatal("expected visible hero for start-window setup")
	}

	// Irregular range: min..max cycle length → cycle days 27..34.
	if hero.AxisDays != 34 {
		t.Fatalf("expected the axis to reach the window end at day 34, got %d", hero.AxisDays)
	}
	if len(hero.Days) != 34 {
		t.Fatalf("expected 34 day cells, got %d", len(hero.Days))
	}

	windowDays := []int{}
	for _, day := range hero.Days {
		if day.IsStartWindow {
			windowDays = append(windowDays, day.Day)
		}
	}
	if len(windowDays) != 8 || windowDays[0] != 27 || windowDays[7] != 34 {
		t.Fatalf("expected start-window days 27-34, got %v", windowDays)
	}
	// Past the projected cycle length there is no phase left to claim.
	if hero.Days[28].Phase != phaseBeyondProjectedCycle {
		t.Fatalf("day 29 sits past the projected cycle, expected %q, got %q", phaseBeyondProjectedCycle, hero.Days[28].Phase)
	}
}

// TestDashboardCycleHeroMarksOnlyRecordedDaysAsLogged pins the fact side of the
// ribbon: a day carries a logged mark only when an entry holds data, only from
// the current cycle, and never in the future.
func TestDashboardCycleHeroMarksOnlyRecordedDaysAsLogged(t *testing.T) {
	cycleStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	today := cycleStart.AddDate(0, 0, 6) // cycle day 7
	notes := "cramps"
	stats := CycleStats{
		CurrentCycleDay:     7,
		CurrentPhase:        "follicular",
		AveragePeriodLength: 5,
		LutealPhase:         14,
		CompletedCycleCount: 4,
		LastPeriodStart:     cycleStart,
	}

	hero := BuildDashboardCycleHero(dashboardcycleheroCovUser28(), stats, dashboardcycleheroCovExactContext(), dashboardCycleHeroInput{
		Logs: []models.DailyLog{
			{Date: cycleStart, IsPeriod: true},                   // day 1, recorded
			{Date: cycleStart.AddDate(0, 0, 2), Notes: notes},    // day 3, recorded
			{Date: cycleStart.AddDate(0, 0, 3)},                  // day 4, empty entry
			{Date: cycleStart.AddDate(0, 0, 9), IsPeriod: true},  // day 10, in the future
			{Date: cycleStart.AddDate(0, 0, -4), IsPeriod: true}, // previous cycle
		},
		Today:    today,
		Location: time.UTC,
	})
	if !hero.Visible {
		t.Fatal("expected visible hero for logged-day setup")
	}

	logged := []int{}
	for _, day := range hero.Days {
		if day.IsLogged {
			logged = append(logged, day.Day)
		}
	}
	if len(logged) != 2 || logged[0] != 1 || logged[1] != 3 {
		t.Fatalf("expected logged marks on days 1 and 3, got %v", logged)
	}
}

// ---------------------------------------------------------------------------
// NOT COVERED — lines 154, 156, 158: fallback numeric switch in
// dashboardCycleHeroCurrentPhase (called when currentPhase is unrecognised)
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToNumericMenstrual covers
// line 154: currentDay >= 1 && currentDay <= periodLength → "menstrual".
func TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToNumericMenstrual(t *testing.T) {
	// Pass an unrecognised currentPhase ("") so the first switch falls through.
	// currentDay=3 is within periodLength=5 → should return "menstrual".
	got := dashboardCycleHeroCurrentPhase("", 3, 5, 14, 28)
	if got != "menstrual" {
		t.Fatalf("expected menstrual fallback for day 3 in period 1-5, got %q", got)
	}
}

// TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToNumericOvulation covers
// line 156: currentDay == ovulationDay → "ovulation".
func TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToNumericOvulation(t *testing.T) {
	got := dashboardCycleHeroCurrentPhase("", 14, 5, 14, 28)
	if got != "ovulation" {
		t.Fatalf("expected ovulation fallback for day == ovulationDay, got %q", got)
	}
}

// TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToNumericLuteal covers
// line 158: currentDay > ovulationDay && currentDay <= cycleLength → "luteal".
func TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToNumericLuteal(t *testing.T) {
	got := dashboardCycleHeroCurrentPhase("", 20, 5, 14, 28)
	if got != "luteal" {
		t.Fatalf("expected luteal fallback for day 20 (past ovulation), got %q", got)
	}
}

// TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToFollicular covers the
// default branch: a day between period and ovulation (exclusive) → "follicular".
func TestDashboardCycleHeroCurrentPhaseUnknownFallsBackToFollicular(t *testing.T) {
	// day=10 is after period (5) and before ovulation (14) → follicular
	got := dashboardCycleHeroCurrentPhase("", 10, 5, 14, 28)
	if got != "follicular" {
		t.Fatalf("expected follicular fallback for day 10 (between period and ovulation), got %q", got)
	}
}

// TestDashboardCycleHeroCurrentPhaseKnownValuesPassThrough asserts that the first
// switch correctly passes through recognised phase strings without numeric override.
func TestDashboardCycleHeroCurrentPhaseKnownValuesPassThrough(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"menstrual", "menstrual"},
		{"ovulation", "ovulation"},
		{"luteal", "luteal"},
		{"follicular", "follicular"},
	}
	for _, tc := range cases {
		got := dashboardCycleHeroCurrentPhase(tc.input, 20, 5, 14, 28)
		if got != tc.want {
			t.Fatalf("input %q: expected %q, got %q", tc.input, tc.want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: CycleDataStale blocks rendering (survives line 83-85 area)
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroStaleDataBlocksRender ensures CycleDataStale=true
// makes hero invisible even with otherwise valid inputs.
func TestDashboardCycleHeroStaleDataBlocksRender(t *testing.T) {
	user := dashboardcycleheroCovUser28()
	stats := dashboardcycleheroCovBaseStats()
	ctx := DashboardCycleContext{
		CycleDataStale:        true,
		DisplayOvulationExact: true,
	}
	hero := BuildDashboardCycleHero(user, stats, ctx, dashboardCycleHeroInput{})
	if hero.Visible {
		t.Fatal("expected invisible hero when CycleDataStale is true")
	}
}

// TestDashboardCycleHeroDisplayOvulationImpossibleBlocksRender ensures
// DisplayOvulationImpossible=true blocks rendering.
func TestDashboardCycleHeroDisplayOvulationImpossibleBlocksRender(t *testing.T) {
	user := dashboardcycleheroCovUser28()
	stats := dashboardcycleheroCovBaseStats()
	ctx := DashboardCycleContext{
		DisplayOvulationImpossible: true,
		DisplayOvulationExact:      true,
	}
	hero := BuildDashboardCycleHero(user, stats, ctx, dashboardCycleHeroInput{})
	if hero.Visible {
		t.Fatal("expected invisible hero when DisplayOvulationImpossible is true")
	}
}
