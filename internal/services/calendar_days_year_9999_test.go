package services

import (
	"testing"
	"time"
)

// WEB-125: the December 9999 grid fills its last week with days ParseDayDate
// refuses. They are drawn, but not offered for selection, and they are in the
// future of any day before them — which a comparison of date keys got wrong,
// because "10000-01-01" orders as text before every four-digit year.
func TestDecember9999GridDaysPastDayDateMaxAreFutureAndNotSelectable(t *testing.T) {
	t.Parallel()

	monthStart := time.Date(9999, time.December, 1, 0, 0, 0, 0, time.UTC)
	for _, now := range []time.Time{
		time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC),
		time.Date(9999, time.December, 30, 12, 0, 0, 0, time.UTC),
	} {
		states := BuildCalendarDayStates(nil, monthStart, nil, CycleStats{}, now, time.UTC)
		byKey := make(map[string]CalendarDayState, len(states))
		for _, state := range states {
			byKey[state.DateString] = state
		}

		for key, wantSelectable := range map[string]bool{
			"9999-12-01":  true,
			"9999-12-30":  true,
			"9999-12-31":  false,
			"10000-01-01": false,
		} {
			state, ok := byKey[key]
			if !ok {
				t.Fatalf("now %s: the December 9999 grid has no cell %s", now.Format("2006-01-02"), key)
			}
			if state.Selectable != wantSelectable {
				t.Errorf("now %s: %s Selectable = %v, want %v", now.Format("2006-01-02"), key, state.Selectable, wantSelectable)
			}
		}
		for _, key := range []string{"9999-12-31", "10000-01-01"} {
			if state := byKey[key]; !state.IsFuture || state.IsToday {
				t.Errorf("now %s: %s IsFuture = %v, IsToday = %v, want a future day", now.Format("2006-01-02"), key, state.IsFuture, state.IsToday)
			}
		}
	}
}

func TestCalendarDayStateTodayAndFutureCompareCalendarDays(t *testing.T) {
	t.Parallel()

	now := time.Date(9999, time.December, 30, 12, 0, 0, 0, time.UTC)
	states := BuildCalendarDayStates(nil, time.Date(9999, time.December, 1, 0, 0, 0, 0, time.UTC), nil, CycleStats{}, now, time.UTC)
	byKey := make(map[string]CalendarDayState, len(states))
	for _, state := range states {
		byKey[state.DateString] = state
	}
	for key, want := range map[string][2]bool{
		"9999-12-29": {false, false},
		"9999-12-30": {true, false},
		"9999-12-31": {false, true},
	} {
		state := byKey[key]
		if state.IsToday != want[0] || state.IsFuture != want[1] {
			t.Errorf("%s: IsToday = %v, IsFuture = %v, want %v, %v", key, state.IsToday, state.IsFuture, want[0], want[1])
		}
	}
}
