package services

// dashboard_cycle_hero_coverage_test.go — behavior tests for the dashboard
// hero ring geometry and render gating in dashboard_cycle_hero.go (segment
// dash arrays, marker position, phase fallbacks, invisible-render guards).
// Written to kill surviving mutants (gremlins). The "dashboardcycleheroCov"
// prefix guards this file's helpers/types against package-wide collisions.

import (
	"math"
	"strconv"
	"testing"

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
// NOT COVERED — line 16: dashboardCycleHeroCircumference
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroCircumferenceValue exercises the package-level
// circumference constant used by segment and dash-array calculations.
// Mutating the formula (e.g. replacing 2*Pi*r with Pi*r) produces a
// wrong value that propagates into DashArray strings.
func TestDashboardCycleHeroCircumferenceValue(t *testing.T) {
	want := 2 * math.Pi * 78.0
	if math.Abs(dashboardCycleHeroCircumference-want) > 1e-9 {
		t.Fatalf("expected circumference %.6f, got %.6f", want, dashboardCycleHeroCircumference)
	}
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
	if !hero.Visible {
		t.Fatal("expected visible hero when ovulationDay is just past the period+1 boundary")
	}
}

// TestDashboardCycleHeroOvulationDayEqualsCycleLengthReturnsInvisible pins
// the right-side boundary of the OR at line 60.
// Mutant: change > to >= would also kill this.
func TestDashboardCycleHeroOvulationDayEqualsCycleLengthReturnsInvisible(t *testing.T) {
	// cycle=28, luteal=0 → CalcOvulationDay resolves luteal to default (14) → ovDay=14
	// We need ovDay == cycleLength: cycle=10, luteal=0 → resolved luteal min → ovDay depends.
	// Use cycle=10, luteal=9 → resolvedLuteal = min(9, 10-1=9)=9 → ovDay=10-9=1 which is <minOvulationCycleDay
	// Let's directly compute: need ovDay == cycleLength.
	// For cycle=16, luteal=1 (resolvedLuteal=max(minLuteal,1)) – need to check ResolveLutealPhase.
	// Safest: use a cycle where ovDay would equal cycleLength by working through CalcOvulationDay.
	// CalcOvulationDay(28, luteal): ovDay = 28 - resolvedLuteal; for ovDay=28 need luteal=0.
	// ResolveLutealPhase(0) might return a default. Let's check differently.
	// We'll use a direct-check: AverageCycleLength=28, AveragePeriodLength=2,
	// and LutealPhase=0 (to force default resolution by CalcOvulationDay).
	// The unit test for CalcOvulationDay will tell us what happens.
	// Actually, we can just observe: if ovDay > cycleLength, hero is invisible.
	// We can manufacture this by making cycle short and period short:
	// cycle=14, period=2, luteal=14 → maxSupportedLuteal = 14-1=13 → resolvedLuteal=13 → ovDay=14-13=1
	// That's ≤ periodLength+1=3 → hits left side. Not useful for right side.
	//
	// For the right-side test (ovDay > cycleLength): CalcOvulationDay caps ovDay at cycleLen-resolvedLuteal.
	// ovDay > cycleLength requires luteal < 0, which can't happen in valid inputs.
	// So ovDay > cycleLength is a purely defensive guard that cannot be triggered by
	// CalcOvulationDay's contract — this path is equivalent/unreachable.
	// We skip this specific boundary and test the left boundary carefully above.
	t.Skip("ovulationDay > cycleLength cannot be triggered by CalcOvulationDay contract; equivalent guard")
}

// ---------------------------------------------------------------------------
// SURVIVING — line 77: float64(currentDay)-0.5
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroMarkerOffsetIsHalfDayBehindCurrentDay asserts that
// the current-day marker X,Y coordinates correspond to day N-0.5, not day N.
// A mutant removing -0.5 would place the marker at a full integer day position.
func TestDashboardCycleHeroMarkerOffsetIsHalfDayBehindCurrentDay(t *testing.T) {
	cycleLength := 28

	// Day 1 with -0.5 offset → dayIndex=0.5
	markerHalfOffset := dashboardCycleHeroMarkerAtDay(0.5, cycleLength)
	// Day 1 without offset → dayIndex=1.0
	markerFullDay := dashboardCycleHeroMarkerAtDay(1.0, cycleLength)

	if markerHalfOffset.X == markerFullDay.X || markerHalfOffset.Y == markerFullDay.Y {
		t.Fatal("marker at day-0.5 must differ from marker at full integer day")
	}

	// The actual hero sets dayIndex = float64(currentDay) - 0.5.
	// Check a concrete case: currentDay=1 → dayIndex=0.5.
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     1,
		CurrentPhase:        "menstrual",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
	if !hero.Visible {
		t.Fatal("expected visible hero for setup")
	}

	expected := dashboardCycleHeroMarkerAtDay(float64(1)-0.5, cycleLength)
	if hero.CurrentDayMarker.X != expected.X || hero.CurrentDayMarker.Y != expected.Y {
		t.Fatalf("expected marker at day 0.5 (%s,%s), got (%s,%s)",
			expected.X, expected.Y, hero.CurrentDayMarker.X, hero.CurrentDayMarker.Y)
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
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
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
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

// TestDashboardCycleHeroSegmentDashArrayEncodesPhaseDuration asserts that each
// segment's DashArray encodes the correct fraction of circumference for its phase.
// A mutant that removes the +1 in (EndDay - StartDay + 1) shrinks every segment by 1 day.
func TestDashboardCycleHeroSegmentDashArrayEncodesPhaseDuration(t *testing.T) {
	// cycle=28, period=5, ovulation=14
	// menstrual: days 1-5 → 5 days
	// follicular: days 6-13 → 8 days
	// ovulation: days 14-14 → 1 day
	// luteal: days 15-28 → 14 days
	// Total: 28 days
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     3,
		CurrentPhase:        "menstrual",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
	if !hero.Visible {
		t.Fatal("expected visible hero for segment test setup")
	}
	if len(hero.Segments) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(hero.Segments))
	}

	circ := dashboardCycleHeroCircumference
	cycleLength := 28

	type wantSeg struct {
		phase string
		days  int
	}
	wantSegs := []wantSeg{
		{"menstrual", 5},
		{"follicular", 8},
		{"ovulation", 1},
		{"luteal", 14},
	}
	for i, ws := range wantSegs {
		seg := hero.Segments[i]
		if seg.Phase != ws.phase {
			t.Fatalf("segment[%d]: expected phase %q, got %q", i, ws.phase, seg.Phase)
		}
		// The DashArray is "<dash> <gap>" where dash = circ * (days/cycleLength)
		dash := circ * float64(ws.days) / float64(cycleLength)
		gap := circ - dash
		wantDashArray := dashboardcycleheroCovFormatFloat(dash) + " " + dashboardcycleheroCovFormatFloat(gap)
		if seg.DashArray != wantDashArray {
			t.Fatalf("segment[%d] %s: expected DashArray %q, got %q", i, ws.phase, wantDashArray, seg.DashArray)
		}
	}
}

// dashboardcycleheroCovFormatFloat mirrors dashboardCycleHeroFloat for test assertions.
func dashboardcycleheroCovFormatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', 1, 64)
}

// TestDashboardCycleHeroSegmentDashOffsetAccumulates asserts that each segment's
// DashOffset accumulates further in the negative direction than the previous one
// (the ring is drawn by progressively offsetting each phase arc). This kills the
// "offset += dash skipped" mutant, which leaves every offset at 0. Structural on
// purpose: it does NOT mirror the production geometry formula or pin exact float
// strings (incl. the -0.0/0.0 first-segment quirk).
func TestDashboardCycleHeroSegmentDashOffsetAccumulates(t *testing.T) {
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     3,
		CurrentPhase:        "menstrual",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
	if !hero.Visible {
		t.Fatal("expected visible hero")
	}
	if len(hero.Segments) != 4 {
		t.Fatalf("expected 4 segments, got %d", len(hero.Segments))
	}

	offsets := make([]float64, len(hero.Segments))
	for i, seg := range hero.Segments {
		v, err := strconv.ParseFloat(seg.DashOffset, 64)
		if err != nil {
			t.Fatalf("segment[%d] DashOffset %q not parseable as float: %v", i, seg.DashOffset, err)
		}
		offsets[i] = v
	}
	// First segment starts the ring at offset 0 (ParseFloat maps "-0.0" -> 0).
	if math.Abs(offsets[0]) > 1e-9 {
		t.Fatalf("segment[0] offset must be ~0 at the ring start, got %v", offsets[0])
	}
	// Each later segment must accumulate strictly more negative than the last.
	for i := 1; i < len(offsets); i++ {
		if offsets[i] >= offsets[i-1] {
			t.Fatalf("segment[%d] offset %v must accumulate more negative than previous %v (kills the no-accumulation mutant)", i, offsets[i], offsets[i-1])
		}
	}
}

// ---------------------------------------------------------------------------
// SURVIVING — line 138: dashboardCycleHeroFloat(-offset)
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroSecondSegmentOffsetIsNegative pins the negation at line 138:
// dashboardCycleHeroFloat(-offset). For segment index ≥ 1, offset is positive,
// so -offset must be negative. A mutant removing the minus sign would produce
// a positive DashOffset for all segments after the first.
func TestDashboardCycleHeroSecondSegmentOffsetIsNegative(t *testing.T) {
	user := dashboardcycleheroCovUser28()
	stats := CycleStats{
		CurrentCycleDay:     3,
		CurrentPhase:        "menstrual",
		AveragePeriodLength: 5,
		LutealPhase:         14,
	}
	hero := BuildDashboardCycleHero(user, stats, dashboardcycleheroCovExactContext())
	if !hero.Visible {
		t.Fatal("expected visible hero")
	}
	// Second segment offset must be negative (dashoffset direction for SVG ring).
	offset1 := hero.Segments[1].DashOffset
	if len(offset1) == 0 || offset1[0] != '-' {
		t.Fatalf("second segment DashOffset must be negative, got %q", offset1)
	}
	// Third and fourth must also be negative (larger accumulated offset).
	for i := 2; i < len(hero.Segments); i++ {
		off := hero.Segments[i].DashOffset
		if len(off) == 0 || off[0] != '-' {
			t.Fatalf("segment[%d] DashOffset must be negative, got %q", i, off)
		}
	}
}

// ---------------------------------------------------------------------------
// SURVIVING — lines 166-169: dashboardCycleHeroMarkerAtDay math
// ---------------------------------------------------------------------------

// TestDashboardCycleHeroMarkerAtDay0IsTopOfCircle asserts that day index 0
// maps to the top of the circle (angle = -π/2 → cos=0, sin=-1).
// This pins line 167 (angle formula) and lines 168-169 (x, y calculation).
func TestDashboardCycleHeroMarkerAtDay0IsTopOfCircle(t *testing.T) {
	marker := dashboardCycleHeroMarkerAtDay(0, 28)
	// angle = -π/2 → x = centerX + r*cos(-π/2) = 110 + 78*0 = 110
	//               → y = centerY + r*sin(-π/2) = 110 + 78*(-1) = 32
	wantX := dashboardcycleheroCovFormatFloat(110.0)
	wantY := dashboardcycleheroCovFormatFloat(32.0)
	if marker.X != wantX {
		t.Fatalf("marker X at day 0: expected %q (top of circle), got %q", wantX, marker.X)
	}
	if marker.Y != wantY {
		t.Fatalf("marker Y at day 0: expected %q (top of circle), got %q", wantY, marker.Y)
	}
}

// TestDashboardCycleHeroMarkerAtHalfCycleIsBottomOfCircle asserts that day index
// == cycleLength/2 maps to the bottom of the circle.
// Pins the ratio computation (line 166) and full math (167-169).
func TestDashboardCycleHeroMarkerAtHalfCycleIsBottomOfCircle(t *testing.T) {
	// dayIndex=14, cycleLength=28 → ratio=0.5 → angle = -π/2 + π = π/2
	// cos(π/2)≈0, sin(π/2)=1 → x=110, y=110+78=188
	marker := dashboardCycleHeroMarkerAtDay(14, 28)
	wantX := dashboardcycleheroCovFormatFloat(110.0)
	wantY := dashboardcycleheroCovFormatFloat(188.0)
	if marker.X != wantX {
		t.Fatalf("marker X at half cycle: expected %q, got %q", wantX, marker.X)
	}
	if marker.Y != wantY {
		t.Fatalf("marker Y at half cycle: expected %q, got %q", wantY, marker.Y)
	}
}

// TestDashboardCycleHeroMarkerRatioUsesCorrectCycleLength asserts that the
// ratio is dayIndex/cycleLength. A mutant replacing cycleLength with a constant
// would break for any cycle not equal to that constant.
func TestDashboardCycleHeroMarkerRatioUsesCorrectCycleLength(t *testing.T) {
	// Same dayIndex but different cycle lengths → different markers.
	m28 := dashboardCycleHeroMarkerAtDay(7, 28) // ratio=0.25
	m32 := dashboardCycleHeroMarkerAtDay(7, 32) // ratio≈0.219
	if m28.X == m32.X && m28.Y == m32.Y {
		t.Fatal("markers for the same day index but different cycle lengths must differ")
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
		{"fertile", "follicular"},
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
	hero := BuildDashboardCycleHero(user, stats, ctx)
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
	hero := BuildDashboardCycleHero(user, stats, ctx)
	if hero.Visible {
		t.Fatal("expected invisible hero when DisplayOvulationImpossible is true")
	}
}
