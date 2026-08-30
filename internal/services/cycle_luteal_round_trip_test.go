package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The round-trip invariant pinned here: an ovulation OBSERVED on cycle day N
// must train a luteal-phase parameter that predicts cycle day N again on an
// identical next cycle. Inference (InferUserLutealPhase) and prediction
// (CalcOvulationDay) are the two directions of one arithmetic, and nothing but
// a test crosses them — each is self-consistent alone, so a disagreement about
// whether the ovulation day itself belongs to the luteal phase is invisible
// from either side.
//
// It was not invisible to owners. Reading the parameter as the calendar span
// from the ovulation date to the next period start counts the ovulation day
// twice, making the value one day too large; CalcOvulationDay then subtracts it
// from the cycle length and lands one day early. An ovulation observed on cycle
// day 15 predicted cycle day 14 — moving the ovulation date and both edges of
// the fertile window a day earlier on the dashboard, the calendar, /api/v1, the
// webhook reminders and the .ics feed, for exactly the owners who had logged
// enough BBT or cervical-mucus signal to earn a personalized model.

type lutealSignalKind int

const (
	// lutealSignalBBT drives inference through the shared "3-over-6" thermal
	// detector; lutealSignalEggWhite drives it through the cervical-mucus
	// peak-day fallback. Both reach the same luteal-length computation, so both
	// have to round-trip.
	lutealSignalBBT lutealSignalKind = iota
	lutealSignalEggWhite
)

// lutealRoundTripLogs builds len(ovulationCycleDays)+1 observed cycle starts,
// cycleLength days apart from origin, and plants an ovulation signal in every
// cycle but the last (the last start only closes the previous cycle, exactly as
// an in-progress cycle does in production). Cycle i's signal is placed so the
// inference must read its ovulation on cycle day ovulationCycleDays[i].
//
// The BBT layout is dictated by the detector: six flat readings open the cycle
// as the coverline window, then three consecutive elevated days start the day
// AFTER ovulation, because the thermal shift follows ovulation. That needs
// ovulationCycleDay >= 6 and ovulationCycleDay+3 <= cycleLength. The egg-white
// layout is the peak-day rule read backwards: ovulation is peak + 1, so the peak
// sits on the day before, which needs ovulationCycleDay >= 2.
//
// Those bounds are checked rather than merely documented. A case built outside
// them yields no detectable shift, and InferUserLutealPhase then declines to
// refine — a failure that reads as a defect in the production inference instead
// of as a fixture built outside its own range.
func lutealRoundTripLogs(t *testing.T, origin time.Time, cycleLength int, ovulationCycleDays []int, kind lutealSignalKind) []models.DailyLog {
	t.Helper()

	for _, ovulationCycleDay := range ovulationCycleDays {
		switch {
		case kind == lutealSignalEggWhite && ovulationCycleDay < 2:
			t.Fatalf("fixture: egg-white ovulation on cycle day %d leaves no room for the peak day before it", ovulationCycleDay)
		case kind == lutealSignalBBT && ovulationCycleDay < bbtCoverlineWindow:
			t.Fatalf("fixture: BBT ovulation on cycle day %d starts the elevated run before the %d-day coverline window is full", ovulationCycleDay, bbtCoverlineWindow)
		case kind == lutealSignalBBT && ovulationCycleDay+bbtElevatedStreakDays > cycleLength:
			t.Fatalf("fixture: BBT ovulation on cycle day %d leaves fewer than %d days for the elevated run inside a %d-day cycle", ovulationCycleDay, bbtElevatedStreakDays, cycleLength)
		case ovulationCycleDay > cycleLength:
			t.Fatalf("fixture: ovulation on cycle day %d falls outside a %d-day cycle", ovulationCycleDay, cycleLength)
		}
	}

	logs := make([]models.DailyLog, 0, (len(ovulationCycleDays)+1)*11)
	for cycle := range len(ovulationCycleDays) + 1 {
		start := origin.AddDate(0, 0, cycle*cycleLength)
		logs = append(logs,
			models.DailyLog{Date: start, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
			models.DailyLog{Date: start.AddDate(0, 0, 1), IsPeriod: true, Flow: models.FlowMedium},
		)
		if cycle == len(ovulationCycleDays) {
			continue
		}

		ovulationCycleDay := ovulationCycleDays[cycle]
		if kind == lutealSignalEggWhite {
			peak := start.AddDate(0, 0, ovulationCycleDay-2)
			logs = append(logs, models.DailyLog{Date: peak, CervicalMucus: models.CervicalMucusEggWhite})
			continue
		}
		for offset := range bbtCoverlineWindow {
			logs = append(logs, models.DailyLog{Date: start.AddDate(0, 0, offset), BBT: new(36.20)})
		}
		for offset := range bbtElevatedStreakDays {
			logs = append(logs, models.DailyLog{Date: start.AddDate(0, 0, ovulationCycleDay+offset), BBT: new(36.50)})
		}
	}
	return logs
}

// assertLutealRoundTrip runs the loop the owner-facing surfaces run: infer the
// parameter from the logged signal, then predict with it on the fixture's own
// in-progress cycle (nextCycleStart, the last start the logs carry) and check
// the prediction lands back on the observed cycle day — as a day number, as a
// date, and as the fertile window's peak edge.
func assertLutealRoundTrip(t *testing.T, logs []models.DailyLog, location *time.Location, cycleLength, wantOvulationCycleDay int, nextCycleStart time.Time) int {
	t.Helper()

	luteal, refined := InferUserLutealPhase(logs, location)
	if !refined {
		t.Fatal("InferUserLutealPhase declined to refine; the fixture must supply at least two usable cycles")
	}
	if wantLuteal := cycleLength - wantOvulationCycleDay; luteal != wantLuteal {
		t.Errorf("inferred luteal phase = %d, want %d (cycle length %d minus the observed ovulation day %d)",
			luteal, wantLuteal, cycleLength, wantOvulationCycleDay)
	}

	ovulationDay, exact := CalcOvulationDay(cycleLength, luteal)
	if !exact {
		t.Errorf("CalcOvulationDay(%d, %d) reported a clamped estimate; a luteal phase this inference accepted must fit the cycle exactly", cycleLength, luteal)
	}
	if ovulationDay != wantOvulationCycleDay {
		t.Fatalf("round trip broken: an ovulation observed on cycle day %d inferred luteal %d, which predicts cycle day %d",
			wantOvulationCycleDay, luteal, ovulationDay)
	}

	// The same trip as a date, through the function the surfaces actually call.
	window := PredictCycleWindow(nextCycleStart, cycleLength, luteal)
	if !window.Calculable {
		t.Fatalf("PredictCycleWindow(%s, %d, %d) returned no window", nextCycleStart.Format("2006-01-02"), cycleLength, luteal)
	}
	wantDate := dateOnly(nextCycleStart.AddDate(0, 0, wantOvulationCycleDay-1))
	if !window.OvulationDate.Equal(wantDate) {
		t.Errorf("predicted ovulation date = %s, want %s", window.OvulationDate.Format("2006-01-02"), wantDate.Format("2006-01-02"))
	}
	if !window.FertilityWindowEnd.Equal(wantDate) {
		t.Errorf("fertile window peak = %s, want %s (the window ends on the ovulation day)",
			window.FertilityWindowEnd.Format("2006-01-02"), wantDate.Format("2006-01-02"))
	}
	return luteal
}

// TestInferredLutealPhaseRoundTripsThroughPrediction is the regression for the
// off-by-one described at the top of this file. Every case observes an
// ovulation, infers the parameter from it, and predicts with that parameter on
// an identical cycle; the predicted cycle day must equal the observed one.
func TestInferredLutealPhaseRoundTripsThroughPrediction(t *testing.T) {
	t.Parallel()

	origin := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)

	cases := []struct {
		name              string
		kind              lutealSignalKind
		cycleLength       int
		ovulationCycleDay int
	}{
		// The headline case: a textbook 28-day cycle whose thermal shift starts
		// on cycle day 16, so ovulation is cycle day 15.
		{name: "bbt, 28-day cycle, ovulation on day 15", kind: lutealSignalBBT, cycleLength: 28, ovulationCycleDay: 15},
		{name: "mucus, 28-day cycle, ovulation on day 15", kind: lutealSignalEggWhite, cycleLength: 28, ovulationCycleDay: 15},
		// Short cycle: the follicular phase absorbs the shortening, so the
		// luteal phase stays physiological and the prediction stays exact.
		{name: "bbt, short 21-day cycle, ovulation on day 8", kind: lutealSignalBBT, cycleLength: 21, ovulationCycleDay: 8},
		{name: "mucus, short 21-day cycle, ovulation on day 9", kind: lutealSignalEggWhite, cycleLength: 21, ovulationCycleDay: 9},
		// Long cycles: the same, in the other direction.
		{name: "bbt, long 35-day cycle, ovulation on day 21", kind: lutealSignalBBT, cycleLength: 35, ovulationCycleDay: 21},
		{name: "mucus, long 40-day cycle, ovulation on day 26", kind: lutealSignalEggWhite, cycleLength: 40, ovulationCycleDay: 26},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			// Two signalled cycles plus the in-progress third, which is the one a
			// prediction is actually made for.
			logs := lutealRoundTripLogs(t, origin, testCase.cycleLength, []int{testCase.ovulationCycleDay, testCase.ovulationCycleDay}, testCase.kind)
			nextCycleStart := origin.AddDate(0, 0, 2*testCase.cycleLength)
			assertLutealRoundTrip(t, logs, time.UTC, testCase.cycleLength, testCase.ovulationCycleDay, nextCycleStart)
		})
	}
}

// TestInferredLutealPhaseRoundTripsAcrossSeveralSamples covers the aggregating
// path rather than the single-sample one: three cycles ovulating on days 14, 15
// and 16 of a 28-day cycle yield luteal samples 14, 13 and 12, whose mean is 13
// — the parameter that predicts the middle observation back. The aggregation is
// a mean of the surviving samples, not a median (the median is what selects the
// cycle LENGTH), so a shift applied per sample would survive it unchanged; the
// round trip is what catches it.
func TestInferredLutealPhaseRoundTripsAcrossSeveralSamples(t *testing.T) {
	t.Parallel()

	const cycleLength = 28
	origin := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	logs := lutealRoundTripLogs(t, origin, cycleLength, []int{14, 15, 16}, lutealSignalBBT)

	luteal := assertLutealRoundTrip(t, logs, time.UTC, cycleLength, 15, origin.AddDate(0, 0, 3*cycleLength))
	if luteal != 13 {
		t.Errorf("inferred luteal phase = %d, want 13 (mean of samples 14, 13, 12)", luteal)
	}
}

// TestInferredLutealPhaseRoundTripsAcrossDSTAndTimezones runs the loop in zones
// whose clocks move inside the observed cycles. Both quantities the INFERENCE
// measures — the cycle length and the ovulation's offset from the cycle start —
// are calendar-day counts taken through CalendarDaysBetween, so a transition
// between the two endpoints must not shorten either: the inferred parameter, and
// therefore the predicted cycle day, must be identical to the UTC run.
//
// The inference is the half that runs in the zone. PredictCycleWindow re-anchors
// its own operands to UTC midnight (dateOnly), so the date leg is
// zone-independent by construction rather than by test; the location-aware
// projection layer above it is pinned separately by
// TestCycleProjection_GoldenVectors, whose fixture carries its own DST vector.
func TestInferredLutealPhaseRoundTripsAcrossDSTAndTimezones(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		zone     string
		origin   time.Time
		kind     lutealSignalKind
		observed int
	}{
		// Europe/Berlin springs forward on 2026-03-29, which is cycle day 15 of
		// the first cycle — the observed ovulation day itself.
		{name: "spring forward on the ovulation day", zone: "Europe/Berlin", origin: time.Date(2026, time.March, 15, 0, 0, 0, 0, time.UTC), kind: lutealSignalBBT, observed: 15},
		// America/Toronto falls back on 2026-11-01, inside the luteal phase.
		{name: "fall back inside the luteal phase", zone: "America/Toronto", origin: time.Date(2026, time.October, 20, 0, 0, 0, 0, time.UTC), kind: lutealSignalEggWhite, observed: 15},
		// A fixed offset far from UTC, where every stored UTC-midnight value
		// lands on a different wall-clock day.
		{name: "fixed far-east offset", zone: "Pacific/Kiritimati", origin: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC), kind: lutealSignalBBT, observed: 15},
	}

	const cycleLength = 28

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			location, err := time.LoadLocation(testCase.zone)
			if err != nil {
				t.Skipf("tz database unavailable for %q: %v", testCase.zone, err)
			}

			logs := lutealRoundTripLogs(t, testCase.origin, cycleLength, []int{testCase.observed, testCase.observed}, testCase.kind)
			nextCycleStart := CalendarDay(testCase.origin.AddDate(0, 0, 2*cycleLength), location)
			zoned := assertLutealRoundTrip(t, logs, location, cycleLength, testCase.observed, nextCycleStart)

			utc, refined := InferUserLutealPhase(logs, time.UTC)
			if !refined {
				t.Fatal("the UTC control must refine from the same logs")
			}
			if zoned != utc {
				t.Errorf("inferred luteal phase = %d in %s but %d in UTC: the location changed a calendar-day count", zoned, testCase.zone, utc)
			}
		})
	}
}

// TestInferredLutealPhaseReachesTheOwnerSurfacesThroughTheBaseline closes the
// loop the tests above leave open. ApplyUserCycleBaseline is what every
// owner-facing surface goes through, and it is the only place that chooses
// between the live inference and the value persisted in users.luteal_phase.
//
// That column is a derived cache — DayService and ImportService write it from
// InferUserLutealPhase — so a row left behind by an OLDER inference is exactly
// the shape the precedence has to survive: the stored value here is the
// one-day-too-long 14 a pre-fix day save would have written for these very logs,
// and the projection must ignore it in favour of the corrected 13.
func TestInferredLutealPhaseReachesTheOwnerSurfacesThroughTheBaseline(t *testing.T) {
	t.Parallel()

	const cycleLength = 28
	origin := time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	logs := lutealRoundTripLogs(t, origin, cycleLength, []int{15, 15}, lutealSignalBBT)

	// Cycle starts Jan 1, Jan 29 and Feb 26; today sits inside the third.
	now := time.Date(2026, time.March, 3, 9, 0, 0, 0, time.UTC)
	user := models.User{
		ID:          1,
		Role:        models.RoleOwner,
		CycleLength: cycleLength,
		LutealPhase: 14,
	}

	stats := ApplyUserCycleBaseline(&user, logs, BuildCycleStats(logs, now), now, time.UTC)

	if stats.LutealPhase != 13 {
		t.Fatalf("stats.LutealPhase = %d, want 13: the live inference must win over the persisted column", stats.LutealPhase)
	}
	if !stats.OvulationExact {
		t.Error("stats.OvulationExact = false; a luteal phase this inference accepted fits a 28-day cycle exactly")
	}

	// The third cycle starts Feb 26, so its cycle day 15 is March 12. The stale
	// 14 would have named March 11 — the whole defect, in one assertion.
	wantOvulation := time.Date(2026, time.March, 12, 0, 0, 0, 0, time.UTC)
	if !stats.OvulationDate.Equal(wantOvulation) {
		t.Errorf("stats.OvulationDate = %s, want %s", stats.OvulationDate.Format("2006-01-02"), wantOvulation.Format("2006-01-02"))
	}
	if !stats.FertilityWindowEnd.Equal(wantOvulation) {
		t.Errorf("stats.FertilityWindowEnd = %s, want %s", stats.FertilityWindowEnd.Format("2006-01-02"), wantOvulation.Format("2006-01-02"))
	}
	wantFertilityStart := wantOvulation.AddDate(0, 0, -5)
	if !stats.FertilityWindowStart.Equal(wantFertilityStart) {
		t.Errorf("stats.FertilityWindowStart = %s, want %s", stats.FertilityWindowStart.Format("2006-01-02"), wantFertilityStart.Format("2006-01-02"))
	}

	// Only the fertility half of the projection moves. The next-period date is
	// the cycle start plus the predicted length and never touches the luteal
	// phase — it is also the one estimate anchored on a day the owner actually
	// recorded, and the one that survives every suppression gate, so a change
	// that reached it would be a scope violation rather than a fix.
	wantNextPeriod := time.Date(2026, time.March, 26, 0, 0, 0, 0, time.UTC)
	if !stats.NextPeriodStart.Equal(wantNextPeriod) {
		t.Errorf("stats.NextPeriodStart = %s, want %s: the luteal phase must not reach the next-period estimate",
			stats.NextPeriodStart.Format("2006-01-02"), wantNextPeriod.Format("2006-01-02"))
	}
}
