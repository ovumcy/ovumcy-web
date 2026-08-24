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
		if state.IsPeriod {
			// A period entry is always a recorded fact, even when dated in the
			// future: auto-fill never writes rows past today, so a future entry
			// is a manual log (the day editor already warns about future dates).
			// Rendering it as a projection would misstate the record.
			cellClass += " calendar-cell-period"
			stateKey = "period"
		} else if state.IsPredictedStartWindow {
			// The window the next period may START in outranks the projected
			// bleeding days it overlaps: it is the more specific statement about
			// the same day, and it is the quantity the dashboard's
			// "Next period: X — Y" line names. One class, so the graded fill can
			// never tie with the hatched one on the same cell.
			cellClass += " calendar-cell-start-window"
			stateKey = "predicted-start-window"
		} else if state.IsPredicted {
			cellClass += " calendar-cell-predicted"
			stateKey = "predicted-period"
		} else if state.IsTentativeOvulation {
			cellClass += " calendar-cell-ovulation-tentative"
			stateKey = "tentative-ovulation"
		} else if state.IsOvulation {
			cellClass += " calendar-cell-fertile"
			stateKey = "ovulation"
		} else if state.IsFertilityPeak {
			cellClass += " calendar-cell-fertile calendar-cell-fertile-peak"
			stateKey = "fertile-peak"
		} else if state.IsFertilityEdge {
			cellClass += " calendar-cell-fertile calendar-cell-fertile-edge"
			stateKey = "fertile-edge"
		} else if state.IsFertility {
			cellClass += " calendar-cell-fertile"
			stateKey = "fertile"
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
		})
	}
	return days
}
