package api

import (
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func (handler *Handler) buildCalendarDays(states []services.CalendarDayState) []CalendarDay {
	days := make([]CalendarDay, 0, len(states))
	for _, state := range states {
		cellClass := "calendar-cell"
		textClass := "calendar-day-number"
		badgeClass := "calendar-tag"
		stateKey := "default"
		if state.IsPeriod {
			// A period entry is always a recorded fact, even when dated in the
			// future: auto-fill never writes rows past today, so a future entry
			// is a manual log (the day editor already warns about future dates).
			// Rendering it as a projection would misstate the record.
			cellClass += " calendar-cell-period"
			badgeClass += " calendar-tag-period"
			stateKey = "period"
		} else if state.IsPredicted {
			cellClass += " calendar-cell-predicted"
			badgeClass += " calendar-tag-predicted"
			stateKey = "predicted-period"
		} else if state.IsTentativeOvulation {
			cellClass += " calendar-cell-ovulation-tentative"
			badgeClass += " calendar-tag-ovulation-tentative"
			stateKey = "tentative-ovulation"
		} else if state.IsOvulation {
			cellClass += " calendar-cell-fertile"
			badgeClass += " calendar-tag-ovulation"
			stateKey = "ovulation"
		} else if state.IsFertilityPeak {
			cellClass += " calendar-cell-fertile calendar-cell-fertile-peak"
			badgeClass += " calendar-tag-fertile"
			stateKey = "fertile-peak"
		} else if state.IsFertilityEdge {
			cellClass += " calendar-cell-fertile calendar-cell-fertile-edge"
			badgeClass += " calendar-tag-fertile"
			stateKey = "fertile-edge"
		} else if state.IsFertility {
			cellClass += " calendar-cell-fertile"
			badgeClass += " calendar-tag-fertile"
			stateKey = "fertile"
		} else if state.IsPreFertile {
			cellClass += " calendar-cell-pre-fertile"
			badgeClass += " calendar-tag-pre-fertile"
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
			InMonth:                state.InMonth,
			IsToday:                state.IsToday,
			OpenEditDirectly:       state.OpenEditDirectly,
			IsPeriod:               state.IsPeriod,
			IsPredicted:            state.IsPredicted,
			IsPreFertile:           state.IsPreFertile,
			IsFertility:            state.IsFertility,
			IsFertilityPeak:        state.IsFertilityPeak,
			IsFertilityEdge:        state.IsFertilityEdge,
			IsOvulation:            state.IsOvulation,
			IsTentativeOvulation:   state.IsTentativeOvulation,
			HasData:                state.HasData,
			HasSex:                 state.HasSex,
			CellClass:              cellClass,
			TextClass:              textClass,
			BadgeClass:             badgeClass,
			StateKey:               stateKey,
			OvulationDot:           state.IsOvulation,
			TentativeOvulationMark: state.IsTentativeOvulation,
		})
	}
	return days
}
