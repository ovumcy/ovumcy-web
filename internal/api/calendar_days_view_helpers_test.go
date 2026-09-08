package api

import (
	"slices"
	"strings"
	"testing"
	"time"

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
			// The other control, and the one that matters medically: a fertile
			// start-window day the projected BAND does not cover. On an
			// irregular cycle the start window runs for weeks and reaches
			// fertile days no bleeding is projected on. It keeps the plain
			// start-window class — painting the overlap here would print the
			// band's own hatch, and the words "predicted period", over a day
			// the model makes no such claim about.
			DateString:             "2026-04-14",
			Day:                    14,
			InMonth:                true,
			IsPredictedStartWindow: true,
			IsFertility:            true,
			IsFertilityEdge:        true,
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
