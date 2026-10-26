package api

import (
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// buildCalendarDays turns the service's day states into the cells the calendar
// template renders. The ladder below is a precedence order, not a set: each day
// resolves to exactly one state, and the two outputs that state produces are the
// classes the cell paints with and the stable key tests and the browser address
// it by.
func (handler *Handler) buildCalendarDays(states []services.CalendarDayState) []CalendarDay {
	days := make([]CalendarDay, 0, len(states))
	for _, state := range states {
		cellClass := "calendar-cell"
		textClass := "calendar-day-number"
		stateKey := "default"
		// The words a screen reader hears for the fill: the legend entry that
		// explains it, with the estimate named for a fertile fill. A fill the
		// legend lists no entry for adds none.
		var stateNames []string
		if state.IsPeriod {
			// A period entry is always a recorded fact, even when dated in the
			// future: auto-fill never writes rows past today, so a future entry
			// is a manual log (the day editor already warns about future dates).
			// Rendering it as a projection would misstate the record.
			cellClass += " calendar-cell-period"
			stateKey = "period"
			stateNames = []string{"calendar.legend.period_recorded"}
		} else if state.IsPredictedStartWindow && (state.IsFertilityEdge || state.IsFertilityPeak) {
			// A start-window day the fertile window also covers. The rung is
			// needed because the start window outranks both the band and the
			// window: without it these days paint as start-window only and cut
			// a hole through the middle of a fertile window whose other days
			// show it.
			//
			// Fertility is read as window MEMBERSHIP — the same edge-or-peak
			// pair services.IsPredictedFertileOverlap is built from, so the two
			// rungs cover one class between them and pre-fertile days, a tier
			// of their own, stay out. Reading the band-anchored flag alone left
			// the hole open on exactly the accounts where it is widest: on an
			// irregular cycle the start window runs from the shortest observed
			// cycle to the longest and reaches fertile days weeks past the five
			// projected bleeding days inside it.
			cellClass += " calendar-cell-overlap-period-fertile calendar-cell-overlap-start-window"
			stateKey = "predicted-start-window-in-fertile-window"
			stateNames = []string{"calendar.legend.start_window", "calendar.fertility_estimate"}
		} else if state.IsPredictedStartWindow {
			// The window the next period may START in outranks the projected
			// bleeding days it overlaps: it is the more specific statement about
			// the same day, and it is the quantity the dashboard's
			// "Next period: X — Y" line names. One class, so the graded fill can
			// never tie with the hatched one on the same cell.
			cellClass += " calendar-cell-start-window"
			stateKey = "predicted-start-window"
			stateNames = []string{"calendar.legend.start_window"}
		} else if state.IsPredictedFertileOverlap {
			// A projected bleeding day that is also inside the fertile window.
			// One class per cell means the predicted-period rung below simply
			// won these days and the window vanished from them, so the reader
			// saw a shorter fertile window than the account has. Both facts
			// hold, so the day gets a fill of its own rather than either half.
			// This rung sits exactly where that one does, so it can only
			// re-paint a cell that already painted as predicted-period.
			//
			// The class stem extends neither "…-predicted" nor "…-fertile":
			// both are asserted elsewhere by substring and by regex, which a
			// longer name built on either would satisfy silently.
			cellClass += " calendar-cell-overlap-period-fertile"
			stateKey = "predicted-period-in-fertile-window"
			stateNames = []string{"calendar.legend.predicted_period_in_fertility"}
		} else if state.IsPredicted {
			cellClass += " calendar-cell-predicted"
			stateKey = "predicted-period"
			stateNames = []string{"calendar.legend.predicted_period"}
		} else if state.IsTentativeOvulation {
			cellClass += " calendar-cell-ovulation-tentative"
			stateKey = "tentative-ovulation"
		} else if state.IsOvulation {
			cellClass += " calendar-cell-fertile"
			stateKey = "ovulation"
			stateNames = []string{"calendar.fertility_estimate"}
		} else if state.IsFertilityPeak {
			cellClass += " calendar-cell-fertile calendar-cell-fertile-peak"
			stateKey = "fertile-peak"
			stateNames = []string{"calendar.fertility_estimate"}
		} else if state.IsFertilityEdge {
			cellClass += " calendar-cell-fertile calendar-cell-fertile-edge"
			stateKey = "fertile-edge"
			stateNames = []string{"calendar.fertility_estimate"}
		} else if state.IsFertility {
			cellClass += " calendar-cell-fertile"
			stateKey = "fertile"
			stateNames = []string{"calendar.fertility_estimate"}
		} else if state.IsPreFertile {
			cellClass += " calendar-cell-pre-fertile"
			stateKey = "pre-fertile"
		}
		if !state.InMonth {
			cellClass += " calendar-cell-out"
			textClass += " calendar-day-out"
		}
		if state.IsToday {
			cellClass += " calendar-cell-today"
		}

		days = append(days, CalendarDay{
			NameKeys:               calendarDayNameKeys(state, stateNames),
			Date:                   state.Date,
			DateString:             state.DateString,
			Day:                    state.Day,
			IsToday:                state.IsToday,
			OpenEditDirectly:       state.OpenEditDirectly,
			HasData:                state.HasData,
			HasSex:                 state.HasSex,
			CellClass:              cellClass,
			TextClass:              textClass,
			StateKey:               stateKey,
			OvulationDot:           state.IsOvulation,
			TentativeOvulationMark: state.IsTentativeOvulation,
			Selectable:             state.Selectable,
		})
	}
	return days
}

// calendarDayNameKeys orders what a cell's accessible name reads after its
// date. The tentative-ovulation fill has no legend entry of its own; its dash
// says it.
func calendarDayNameKeys(state services.CalendarDayState, stateNames []string) []string {
	var keys []string
	if state.IsToday {
		keys = append(keys, "calendar.tag.today")
	}
	keys = append(keys, stateNames...)
	if state.IsOvulation {
		keys = append(keys, "calendar.ovulation_icon")
	}
	if state.IsTentativeOvulation {
		keys = append(keys, "calendar.ovulation_tentative")
	}
	if state.HasData {
		keys = append(keys, "calendar.data_logged")
	}
	if state.HasSex {
		keys = append(keys, "calendar.sex_logged")
	}
	return keys
}
