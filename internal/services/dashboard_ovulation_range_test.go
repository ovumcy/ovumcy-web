package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"pgregory.net/rapid"
)

// The irregular-mode ovulation range must name the days the cycle model puts
// ovulation on in the shortest and in the longest observed cycle. Shifting the
// next-period range back by the luteal phase lands one day late on both ends,
// because ovulation is the day BEFORE the luteal phase begins.

// literalOvulationDay restates the documented arithmetic — ovulation falls
// (cycle length - luteal phase) days into the cycle, never earlier than cycle
// day 5 — without reading the predictor under test.
func literalOvulationDay(cycleLength, lutealPhase int) int {
	day := cycleLength - lutealPhase
	if day < 5 {
		day = 5
	}
	return day
}

func TestDashboardOvulationRangeNamesThePredictorsOvulationForBothLengths(t *testing.T) {
	t.Parallel()

	start := mustParseDashboardDay(t, "2026-03-01")
	for _, testCase := range []struct {
		name                 string
		minLength, maxLength int
		luteal               int
		wantStart, wantEnd   string
	}{
		{name: "default luteal", minLength: 24, maxLength: 45, luteal: 14, wantStart: "2026-03-10", wantEnd: "2026-03-31"},
		{name: "unset luteal reads the default", minLength: 24, maxLength: 45, luteal: 0, wantStart: "2026-03-10", wantEnd: "2026-03-31"},
		{name: "personal luteal 10", minLength: 26, maxLength: 34, luteal: 10, wantStart: "2026-03-16", wantEnd: "2026-03-24"},
		{name: "single observed length collapses to one day", minLength: 28, maxLength: 28, luteal: 14, wantStart: "2026-03-14", wantEnd: "2026-03-14"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotStart, gotEnd, ok := DashboardOvulationRange(start, testCase.minLength, testCase.maxLength, testCase.luteal, time.UTC)
			if !ok {
				t.Fatalf("expected a range for lengths %d..%d", testCase.minLength, testCase.maxLength)
			}
			if got := gotStart.Format("2006-01-02"); got != testCase.wantStart {
				t.Errorf("range start = %s, want %s", got, testCase.wantStart)
			}
			if got := gotEnd.Format("2006-01-02"); got != testCase.wantEnd {
				t.Errorf("range end = %s, want %s", got, testCase.wantEnd)
			}
		})
	}
}

// A shortest cycle under minPlaceableCycleLength days has no modelled ovulation
// of its own. The range must not vanish for it — the dashboard would then keep
// the single median date, presented as exact — so its start is the model's
// floor: the ovulation it places in the shortest cycle it can place.
func TestDashboardOvulationRangeStartsAtTheModelFloorForAShortestCycleItCannotPlace(t *testing.T) {
	t.Parallel()

	start := mustParseDashboardDay(t, "2026-03-01")
	for _, shortest := range []int{1, 12, minPlaceableCycleLength - 1} {
		for _, luteal := range []int{0, 10, 14} {
			gotStart, gotEnd, ok := DashboardOvulationRange(start, shortest, 45, luteal, time.UTC)
			if !ok {
				t.Fatalf("shortest %d, luteal %d: expected a range, the longest cycle is placeable", shortest, luteal)
			}
			if got := gotStart.Format("2006-01-02"); got != "2026-03-05" {
				t.Errorf("shortest %d, luteal %d: range start = %s, want 2026-03-05 (cycle day 5, the model's earliest)", shortest, luteal, got)
			}
			if got, want := gotEnd.Format("2006-01-02"), PredictCycleWindow(start, 45, luteal).OvulationDate.Format("2006-01-02"); got != want {
				t.Errorf("shortest %d, luteal %d: range end = %s, want %s", shortest, luteal, got, want)
			}
		}
	}
}

// TestDashboardShowsAnOvulationRangeNotAnExactDayForAShortestCycleUnderTheFloor
// pins what the owner sees: irregular history 12/30/45 gets the range, and the
// median-based single date is cleared rather than named as exact.
func TestDashboardShowsAnOvulationRangeNotAnExactDayForAShortestCycleUnderTheFloor(t *testing.T) {
	t.Parallel()

	start := mustParseDashboardDay(t, "2026-03-01")
	today := mustParseDashboardDay(t, "2026-03-03")
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     start,
		AverageCycleLength:  29,
		MedianCycleLength:   30,
		MinCycleLength:      12,
		MaxCycleLength:      45,
		LutealPhase:         14,
		CurrentCycleDay:     3,
		CompletedCycleCount: 3,
		NextPeriodStart:     start.AddDate(0, 0, 30),
	}

	context := BuildDashboardCycleContext(user, nil, stats, today, time.UTC)
	if !context.DisplayNextPeriodUseRange {
		t.Fatal("fixture: the next-period range must be shown for this account")
	}
	if !context.DisplayOvulationUseRange {
		t.Fatalf("the ovulation range must be shown wherever the next-period range is; got a single date %s (exact %v)",
			context.DisplayOvulationDate, context.DisplayOvulationExact)
	}
	if !context.DisplayOvulationDate.IsZero() || context.DisplayOvulationExact {
		t.Fatalf("a range is shown, so the single date must be cleared; got %s (exact %v)",
			context.DisplayOvulationDate, context.DisplayOvulationExact)
	}
	if got := context.DisplayOvulationRangeStart.Format("2006-01-02"); got != "2026-03-05" {
		t.Errorf("range start = %s, want 2026-03-05", got)
	}
	if got, want := context.DisplayOvulationRangeEnd.Format("2006-01-02"), PredictCycleWindow(start, 45, 14).OvulationDate.Format("2006-01-02"); got != want {
		t.Errorf("range end = %s, want %s", got, want)
	}
}

func TestDashboardOvulationRangeIsAbsentWhenTheModelCannotPlaceAnEnd(t *testing.T) {
	t.Parallel()

	start := mustParseDashboardDay(t, "2026-03-01")
	if _, _, ok := DashboardOvulationRange(start, 12, minPlaceableCycleLength-1, 14, time.UTC); ok {
		t.Fatalf("a longest cycle with no modelled ovulation leaves nothing to build a range on")
	}
	if _, _, ok := DashboardOvulationRange(start, 0, 40, 14, time.UTC); ok {
		t.Fatalf("a missing shortest cycle must not yield a range")
	}
	if _, _, ok := DashboardOvulationRange(start, 24, 0, 14, time.UTC); ok {
		t.Fatalf("a missing longest cycle must not yield a range")
	}
	if _, _, ok := DashboardOvulationRange(time.Time{}, 24, 45, 14, time.UTC); ok {
		t.Fatalf("a missing anchor must not yield a range")
	}
}

// TestApplyDashboardPredictionRangesWithholdsTheOvulationRangeWithTheNextPeriodRange
// pins the other half of the pairing: an irregular account with enough cycles
// that has no next-period projection to express a spread of gets no ovulation
// range either, rather than one built from lengths alone.
func TestApplyDashboardPredictionRangesWithholdsTheOvulationRangeWithTheNextPeriodRange(t *testing.T) {
	t.Parallel()

	user := &models.User{Role: models.RoleOwner, IrregularCycle: true, CycleLength: 28, LutealPhase: 14}
	stats := CycleStats{
		CompletedCycleCount: 3,
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		MinCycleLength:      24,
		MaxCycleLength:      45,
		LutealPhase:         14,
	}
	if !dashboardIrregularPredictionRangeEnabled(user, stats) {
		t.Fatal("fixture: the irregular range must be enabled for this account")
	}

	// No next-period start: DashboardPredictionRange reports no range.
	display := applyDashboardPredictionRanges(dashboardPredictionDisplay{}, user, stats, time.UTC)
	if display.nextPeriodUseRange || display.ovulationUseRange {
		t.Fatalf("nextPeriodUseRange = %v, ovulationUseRange = %v; both ranges must be absent without a next-period projection",
			display.nextPeriodUseRange, display.ovulationUseRange)
	}

	// The same account with a projection does get both, so the absence above is
	// the pairing and not a range that can never be built.
	display = applyDashboardPredictionRanges(dashboardPredictionDisplay{nextPeriodStart: mustParseDashboardDay(t, "2026-03-29")}, user, stats, time.UTC)
	if !display.nextPeriodUseRange || !display.ovulationUseRange {
		t.Fatalf("nextPeriodUseRange = %v, ovulationUseRange = %v; both ranges must be present with a projection",
			display.nextPeriodUseRange, display.ovulationUseRange)
	}
}

// TestDashboardOvulationRangeEndpointsEqualPredictCycleWindowProperty draws the
// anchor, both lengths, the luteal phase and the request zone, and requires each
// endpoint to be exactly the ovulation PredictCycleWindow reports for the
// matching length — and to match the literal arithmetic.
func TestDashboardOvulationRangeEndpointsEqualPredictCycleWindowProperty(t *testing.T) {
	zones := make([]*time.Location, 0, 4)
	for _, name := range []string{"UTC", "America/New_York", "Asia/Tokyo", "Pacific/Auckland"} {
		zones = append(zones, calendarDayComparisonZone(t, name))
	}

	rapid.Check(t, func(t *rapid.T) {
		start := drawCycleStartDate(t)
		minLength := rapid.IntRange(1, 60).Draw(t, "minLength")
		maxLength := rapid.IntRange(max(minLength, 15), 90).Draw(t, "maxLength")
		luteal := rapid.IntRange(0, 20).Draw(t, "luteal")
		location := rapid.SampledFrom(zones).Draw(t, "location")

		gotStart, gotEnd, ok := DashboardOvulationRange(CalendarDay(start, location), minLength, maxLength, luteal, location)
		if !ok {
			t.Fatalf("no range for lengths %d..%d luteal %d", minLength, maxLength, luteal)
		}
		// A shortest cycle the model cannot place is read at the shortest one it can.
		minLength = max(minLength, 15)

		resolved := ResolveLutealPhase(luteal)
		for _, end := range []struct {
			name   string
			got    time.Time
			length int
		}{
			{name: "start", got: gotStart, length: minLength},
			{name: "end", got: gotEnd, length: maxLength},
		} {
			window := PredictCycleWindow(start, end.length, luteal)
			if !window.Calculable {
				t.Fatalf("predictor cannot place ovulation in a %d-day cycle", end.length)
			}
			if got, want := CalendarDayKey(end.got), CalendarDayKey(window.OvulationDate); got != want {
				t.Fatalf("range %s = %s, want the predictor's ovulation %s (length %d, luteal %d)", end.name, got, want, end.length, luteal)
			}
			literal := CalendarDayKey(start.AddDate(0, 0, literalOvulationDay(end.length, resolved)-1))
			if got := CalendarDayKey(end.got); got != literal {
				t.Fatalf("range %s = %s, want %s from the documented arithmetic (length %d, luteal %d)", end.name, got, literal, end.length, luteal)
			}
			if h, m, s := end.got.In(location).Clock(); h != 0 || m != 0 || s != 0 {
				t.Fatalf("range %s %s is not at midnight of the request zone", end.name, end.got)
			}
		}
		if gotEnd.Before(gotStart) {
			t.Fatalf("range runs backwards: %s .. %s", gotStart, gotEnd)
		}
	})
}

// TestDashboardContextOvulationRangeMatchesThePredictorProperty drives the same
// property through the context the dashboard renders from, so a caller that
// stopped passing the observed lengths — or kept deriving the range from the
// next-period one — is caught where the owner sees it.
func TestDashboardContextOvulationRangeMatchesThePredictorProperty(t *testing.T) {
	zones := make([]*time.Location, 0, 3)
	for _, name := range []string{"UTC", "America/New_York", "Asia/Tokyo"} {
		zones = append(zones, calendarDayComparisonZone(t, name))
	}

	rapid.Check(t, func(t *rapid.T) {
		minLength := rapid.IntRange(20, 30).Draw(t, "minLength")
		maxLength := rapid.IntRange(minLength, 50).Draw(t, "maxLength")
		luteal := rapid.IntRange(10, 16).Draw(t, "luteal")
		location := rapid.SampledFrom(zones).Draw(t, "location")

		start := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
		today := CalendarDay(time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC), location)
		user := &models.User{IrregularCycle: true}
		stats := CycleStats{
			LastPeriodStart:     CalendarDay(start, location),
			AverageCycleLength:  float64(minLength+maxLength) / 2,
			MedianCycleLength:   (minLength + maxLength) / 2,
			MinCycleLength:      minLength,
			MaxCycleLength:      maxLength,
			LutealPhase:         luteal,
			CurrentCycleDay:     10,
			CompletedCycleCount: 3,
			NextPeriodStart:     start.AddDate(0, 0, (minLength+maxLength)/2),
		}

		context := BuildDashboardCycleContext(user, nil, stats, today, location)
		if !context.DisplayOvulationUseRange {
			t.Fatalf("expected an ovulation range for lengths %d..%d luteal %d", minLength, maxLength, luteal)
		}
		for _, end := range []struct {
			name   string
			got    time.Time
			length int
		}{
			{name: "start", got: context.DisplayOvulationRangeStart, length: minLength},
			{name: "end", got: context.DisplayOvulationRangeEnd, length: maxLength},
		} {
			want := PredictCycleWindow(start, end.length, luteal).OvulationDate
			if got := CalendarDayKey(end.got); got != CalendarDayKey(want) {
				t.Fatalf("context range %s = %s, want %s (length %d, luteal %d)", end.name, got, CalendarDayKey(want), end.length, luteal)
			}
		}
	})
}
