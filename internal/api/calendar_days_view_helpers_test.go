package api

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestBuildCalendarDaysRendersFuturePeriodEntryAsRecordedPeriod(t *testing.T) {
	handler := &Handler{}
	days := handler.buildCalendarDays([]services.CalendarDayState{
		{
			Date:       time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
			DateString: "2026-02-17",
			Day:        17,
			InMonth:    true,
			IsPeriod:   true,
			IsFuture:   false,
		},
		{
			Date:       time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC),
			DateString: "2026-03-20",
			Day:        20,
			InMonth:    true,
			IsPeriod:   true,
			IsFuture:   true,
		},
	})

	// A period entry is a recorded fact regardless of its date: auto-fill never
	// writes rows past today, so a future entry is a manual log and must not be
	// styled as a projection (regression: real records rendered as predictions).
	//
	// The projection classes named below are the ones the builder can actually
	// emit. An earlier form of this case asserted the absence of
	// `calendar-cell-period-projected`, a class no branch of buildCalendarDays
	// composes and no stylesheet defines: true for every possible input, and so
	// green about nothing. The class a mis-wired future day would really pick up
	// is whichever the projection arms produce.
	for i, day := range days {
		for _, projection := range []string{"calendar-cell-predicted", "calendar-cell-start-window"} {
			if strings.Contains(day.CellClass, projection) {
				t.Fatalf("day %d: a recorded period entry must not carry the projection class %q, got %q", i, projection, day.CellClass)
			}
		}
		if !strings.Contains(day.CellClass, "calendar-cell-period") {
			t.Fatalf("day %d: expected period class, got %q", i, day.CellClass)
		}
		if day.StateKey != "period" {
			t.Fatalf("day %d: stateKey = %q, want period", i, day.StateKey)
		}
	}

	// IsFuture is the seam. The service sets it on every day state it builds and
	// buildCalendarDays reads it nowhere, so the two entries above differ in
	// that flag alone and must therefore render identically. Asserting the two
	// cells match is what makes the flag's inertness a contract rather than an
	// accident: any branch that starts consulting it separates them here.
	if days[0].CellClass != days[1].CellClass {
		t.Fatalf("a future period entry rendered differently from a past one: %q vs %q", days[1].CellClass, days[0].CellClass)
	}
	if days[0].StateKey != days[1].StateKey {
		t.Fatalf("a future period entry took a different state key: %q vs %q", days[1].StateKey, days[0].StateKey)
	}
}

// The predicted start window and the projected bleeding days are two different
// quantities, so they must not resolve to one class — and where they overlap the
// window is the more specific statement, while a recorded period day outranks
// both. One class per cell keeps the graded fill from tying with the hatched one.
func TestBuildCalendarDaysSeparatesTheStartWindowFromProjectedPeriodDays(t *testing.T) {
	handler := &Handler{}
	days := handler.buildCalendarDays([]services.CalendarDayState{
		{
			DateString:             "2026-04-03",
			Day:                    3,
			InMonth:                true,
			IsPredictedStartWindow: true,
		},
		{
			DateString:             "2026-04-05",
			Day:                    5,
			InMonth:                true,
			IsPredicted:            true,
			IsPredictedStartWindow: true,
		},
		{
			DateString:  "2026-04-08",
			Day:         8,
			InMonth:     true,
			IsPredicted: true,
		},
		{
			DateString:             "2026-04-09",
			Day:                    9,
			InMonth:                true,
			IsPeriod:               true,
			IsPredicted:            true,
			IsPredictedStartWindow: true,
		},
	})

	for _, day := range days[:2] {
		if !strings.Contains(day.CellClass, "calendar-cell-start-window") {
			t.Fatalf("day %s: expected the start-window class, got %q", day.DateString, day.CellClass)
		}
		if strings.Contains(day.CellClass, "calendar-cell-predicted") {
			t.Fatalf("day %s: start window must not also carry the projected-period class, got %q", day.DateString, day.CellClass)
		}
		if day.StateKey != "predicted-start-window" {
			t.Fatalf("day %s: stateKey = %q, want predicted-start-window", day.DateString, day.StateKey)
		}
	}

	if !strings.Contains(days[2].CellClass, "calendar-cell-predicted") {
		t.Fatalf("expected a projected period day outside the window to keep its own class, got %q", days[2].CellClass)
	}
	if strings.Contains(days[2].CellClass, "calendar-cell-start-window") {
		t.Fatalf("a projected period day outside the window must not read as a start window, got %q", days[2].CellClass)
	}
	if days[2].StateKey != "predicted-period" {
		t.Fatalf("stateKey = %q, want predicted-period", days[2].StateKey)
	}

	if !strings.Contains(days[3].CellClass, "calendar-cell-period") || strings.Contains(days[3].CellClass, "calendar-cell-start-window") {
		t.Fatalf("a recorded period day must outrank every projection, got %q", days[3].CellClass)
	}
	if days[3].StateKey != "period" {
		t.Fatalf("stateKey = %q, want period", days[3].StateKey)
	}
}

// The overlap rung. A day that is both a projected bleeding day and a fertile
// window day used to fall through to the predicted-period rung, so the window
// was invisible on it. It now paints a fill of its own — and, just as
// importantly, the cells on either side of the overlap keep the exact class
// they had, which is the regression this rung could most easily cause.
//
// The class assertions compare whitespace-delimited TOKENS rather than
// substrings: "calendar-cell-overlap-period-fertile" was named so that neither
// "calendar-cell-predicted" nor "calendar-cell-fertile" is a substring of it,
// and a substring check here would not prove that.
func TestBuildCalendarDaysGivesTheBandAndWindowOverlapItsOwnFill(t *testing.T) {
	handler := &Handler{}
	days := handler.buildCalendarDays([]services.CalendarDayState{
		{
			DateString:                "2026-03-03",
			Day:                       3,
			InMonth:                   true,
			IsPredicted:               true,
			IsFertility:               true,
			IsFertilityEdge:           true,
			IsPredictedFertileOverlap: true,
		},
		{
			DateString:  "2026-03-30",
			Day:         30,
			InMonth:     true,
			IsPredicted: true,
		},
		{
			DateString:      "2026-03-06",
			Day:             6,
			InMonth:         true,
			IsFertility:     true,
			IsFertilityPeak: true,
		},
		{
			// A recorded bleeding day still outranks every projection, overlap
			// included: the ladder's top rung is a fact, not an estimate.
			DateString:                "2026-03-04",
			Day:                       4,
			InMonth:                   true,
			IsPeriod:                  true,
			IsPredicted:               true,
			IsFertilityEdge:           true,
			IsPredictedFertileOverlap: true,
		},
		{
			// The start window inside the fertile window: the overlap fill,
			// plus the modifier that restores the dotted start-window stroke.
			DateString:                "2026-03-23",
			Day:                       23,
			InMonth:                   true,
			IsPredicted:               true,
			IsPredictedStartWindow:    true,
			IsFertility:               true,
			IsFertilityEdge:           true,
			IsPredictedFertileOverlap: true,
		},
		{
			// The control beside it: a start-window day no window covers keeps
			// the plain start-window class, so the new rung cannot swallow the
			// whole range.
			DateString:             "2026-03-21",
			Day:                    21,
			InMonth:                true,
			IsPredictedStartWindow: true,
		},
		{
			// The second site of the same class, and the one that matters
			// medically: a fertile start-window day the projected BAND does not
			// cover. On an irregular cycle the start window runs for weeks and
			// reaches fertile days no bleeding is projected on, so anchoring
			// the overlap to the band left those days painted as start-window
			// only — the fertile window invisible on them, the hole this fill
			// exists to close.
			DateString:             "2026-04-14",
			Day:                    14,
			InMonth:                true,
			IsPredictedStartWindow: true,
			IsFertility:            true,
			IsFertilityEdge:        true,
		},
		{
			// Pre-fertile is a tier of its own, not window membership: a
			// start-window day beside the window keeps the plain fill.
			DateString:             "2026-04-16",
			Day:                    16,
			InMonth:                true,
			IsPredictedStartWindow: true,
			IsPreFertile:           true,
		},
	})

	cases := []struct {
		class    string
		stateKey string
	}{
		{"calendar-cell-overlap-period-fertile", "predicted-period-in-fertile-window"},
		{"calendar-cell-predicted", "predicted-period"},
		{"calendar-cell-fertile calendar-cell-fertile-peak", "fertile-peak"},
		{"calendar-cell-period", "period"},
		{
			"calendar-cell-overlap-period-fertile calendar-cell-overlap-start-window",
			"predicted-start-window-in-fertile-window",
		},
		{"calendar-cell-start-window", "predicted-start-window"},
		{
			"calendar-cell-overlap-period-fertile calendar-cell-overlap-start-window",
			"predicted-start-window-in-fertile-window",
		},
		{"calendar-cell-start-window", "predicted-start-window"},
	}
	for index, want := range cases {
		got := days[index]
		if classes := strings.Fields(got.CellClass); !slices.Equal(classes, append([]string{"calendar-cell"}, strings.Fields(want.class)...)) {
			t.Errorf("day %s: cellClass = %q, want the %q state", got.DateString, got.CellClass, want.stateKey)
		}
		if got.StateKey != want.stateKey {
			t.Errorf("day %s: stateKey = %q, want %q", got.DateString, got.StateKey, want.stateKey)
		}
	}
}

// The same overlap one rung up, on the site the band-anchored rung above cannot
// see: a fertile day the projected START WINDOW covers and the projected band
// never reaches. On an irregular cycle the window runs from the shortest
// observed cycle to the longest — 21 to 60 days here — so it spans the next
// cycle's fertile window, while the five projected bleeding days sit weeks
// earlier inside it. Those cells fell to the plain start-window rung and the
// fertile window went missing from them, which is the hole this PR set out to
// close; a class closed at one of its two sites is not closed.
//
// The states come from the service rather than being written by hand, because
// the claim under test is that this combination OCCURS, not merely that the
// ladder would render it if it did.
func TestBuildCalendarDaysFillsTheFertileStartWindowTheBandNeverReaches(t *testing.T) {
	location := time.UTC
	lastPeriodStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, location)
	user := &models.User{IrregularCycle: true, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
	stats := services.CycleStats{
		MedianCycleLength:    28,
		AverageCycleLength:   28,
		MinCycleLength:       21,
		MaxCycleLength:       60,
		CompletedCycleCount:  4,
		AveragePeriodLength:  5,
		LutealPhase:          14,
		LastPeriodStart:      lastPeriodStart,
		NextPeriodStart:      lastPeriodStart.AddDate(0, 0, 28),
		OvulationDate:        lastPeriodStart.AddDate(0, 0, 14),
		FertilityWindowStart: lastPeriodStart.AddDate(0, 0, 9),
		FertilityWindowEnd:   lastPeriodStart.AddDate(0, 0, 14),
	}

	states := services.BuildCalendarDayStates(
		user,
		time.Date(2026, time.April, 1, 0, 0, 0, 0, location),
		nil,
		stats,
		lastPeriodStart.AddDate(0, 0, 4),
		location,
	)

	fertileStartWindow := make([]string, 0, 6)
	for _, state := range states {
		if state.IsPredictedStartWindow && (state.IsFertilityEdge || state.IsFertilityPeak) && !state.IsPredicted {
			fertileStartWindow = append(fertileStartWindow, state.DateString)
		}
	}
	// The setup assertion, not the subject: the scenario has to be real before
	// its rendering is worth asserting.
	if want := []string{"2026-04-06", "2026-04-07", "2026-04-08", "2026-04-09", "2026-04-10", "2026-04-11"}; !slices.Equal(fertileStartWindow, want) {
		t.Fatalf("test setup: fertile start-window days outside the band = %v, want %v", fertileStartWindow, want)
	}

	handler := &Handler{}
	days := handler.buildCalendarDays(states)
	byDate := make(map[string]CalendarDay, len(days))
	for _, day := range days {
		byDate[day.DateString] = day
	}

	for _, dateString := range fertileStartWindow {
		day := byDate[dateString]
		classes := strings.Fields(day.CellClass)
		// Neither statement is suppressed: the overlap fill carries the fertile
		// window, the modifier keeps the dotted stroke that means start window.
		for _, want := range []string{"calendar-cell-overlap-period-fertile", "calendar-cell-overlap-start-window"} {
			if !slices.Contains(classes, want) {
				t.Errorf("day %s: cellClass = %q, want the %q token", dateString, day.CellClass, want)
			}
		}
		if slices.Contains(classes, "calendar-cell-start-window") {
			t.Errorf("day %s: the plain start-window fill hides the fertile window it covers, got %q", dateString, day.CellClass)
		}
		if day.StateKey != "predicted-start-window-in-fertile-window" {
			t.Errorf("day %s: stateKey = %q, want predicted-start-window-in-fertile-window", dateString, day.StateKey)
		}
	}

	// The control right beside the window: a start-window day no fertile window
	// covers keeps the plain fill, so the rung cannot swallow the whole range.
	if classes := strings.Fields(byDate["2026-04-05"].CellClass); !slices.Equal(classes, []string{"calendar-cell", "calendar-cell-start-window"}) {
		t.Errorf("day 2026-04-05: cellClass = %q, want the plain start-window state", byDate["2026-04-05"].CellClass)
	}
}

func TestBuildCalendarDaysMapsStateToTemplateClasses(t *testing.T) {
	handler := &Handler{}
	states := []services.CalendarDayState{
		{
			Date:        time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
			DateString:  "2026-02-17",
			Day:         17,
			InMonth:     true,
			IsToday:     false,
			IsPeriod:    true,
			IsPredicted: false,
			IsFertility: false,
			IsOvulation: false,
		},
		{
			Date:        time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
			DateString:  "2026-03-01",
			Day:         1,
			InMonth:     false,
			IsToday:     true,
			IsPeriod:    false,
			IsPredicted: false,
			IsFertility: false,
			IsOvulation: true,
		},
		{
			Date:         time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC),
			DateString:   "2026-03-02",
			Day:          2,
			InMonth:      true,
			IsToday:      false,
			IsPeriod:     false,
			IsPredicted:  false,
			IsPreFertile: true,
			IsFertility:  false,
			IsOvulation:  false,
			HasData:      true,
		},
	}

	days := handler.buildCalendarDays(states)
	if len(days) != 3 {
		t.Fatalf("expected three mapped calendar days, got %d", len(days))
	}

	if !strings.Contains(days[0].CellClass, "calendar-cell-period") {
		t.Fatalf("expected period class for first day, got %q", days[0].CellClass)
	}
	if !strings.Contains(days[1].CellClass, "calendar-cell-fertile") {
		t.Fatalf("expected fertile class for ovulation day, got %q", days[1].CellClass)
	}
	if !strings.Contains(days[1].CellClass, "calendar-cell-out") {
		t.Fatalf("expected out-of-month class for second day, got %q", days[1].CellClass)
	}
	if !strings.Contains(days[1].CellClass, "calendar-cell-today") {
		t.Fatalf("expected today class for second day, got %q", days[1].CellClass)
	}
	if !strings.Contains(days[1].TextClass, "calendar-day-out") {
		t.Fatalf("expected out-of-month text class, got %q", days[1].TextClass)
	}
	if !days[1].OvulationDot {
		t.Fatalf("expected ovulation dot for second day")
	}

	if !strings.Contains(days[2].CellClass, "calendar-cell-pre-fertile") {
		t.Fatalf("expected pre-fertile class for third day, got %q", days[2].CellClass)
	}
	if days[2].StateKey != "pre-fertile" {
		t.Fatalf("expected pre-fertile state key, got %q", days[2].StateKey)
	}
	if !days[2].HasData {
		t.Fatalf("expected third day to preserve logged-data marker state")
	}
}
