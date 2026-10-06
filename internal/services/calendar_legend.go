package services

// CalendarLegend names the states the month grid actually draws for one view,
// so the legend can promise exactly those and no others. It is derived from the
// day states the grid is painted from, never from a second reading of the
// owner's settings: a legend that asked its own question could list a swatch
// the grid, withholding a projection for the same owner, never draws — the
// account with one completed cycle saw eight entries over a grid with no
// projected mark on it.
//
// Each field is true when at least one cell of the grid carries the state, in
// the same terms the cell resolves its fill from. The legend lists CONCEPTS,
// not CSS fills, so the two overlap rungs of the cell ladder (the projected
// bleeding band and the start window, each inside the fertile window) share one
// entry.
type CalendarLegend struct {
	PeriodRecorded             bool
	PredictedPeriod            bool
	StartWindow                bool
	Fertility                  bool
	PredictedPeriodInFertility bool
	// OvulationTentative is true when a projected ovulation awaits a temperature
	// shift: the legend then shows the dot-and-dash pair. OvulationEstimate is the
	// solid dot alone, for a view that draws no unconfirmed projection.
	OvulationTentative bool
	OvulationEstimate  bool
	Today              bool
	LoggedEntry        bool
}

// Any reports whether the legend has an entry at all, so the template can leave
// the legend out of a view that draws nothing to explain.
func (legend CalendarLegend) Any() bool {
	return legend.PeriodRecorded || legend.PredictedPeriod || legend.StartWindow ||
		legend.Fertility || legend.PredictedPeriodInFertility ||
		legend.OvulationTentative || legend.OvulationEstimate ||
		legend.Today || legend.LoggedEntry
}

// BuildCalendarLegend reads the states the grid draws off its day states.
func BuildCalendarLegend(days []CalendarDayState) CalendarLegend {
	legend := CalendarLegend{}
	for _, day := range days {
		inFertileWindow := day.IsFertilityEdge || day.IsFertilityPeak
		legend.PeriodRecorded = legend.PeriodRecorded || day.IsPeriod
		legend.PredictedPeriod = legend.PredictedPeriod || day.IsPredicted
		legend.StartWindow = legend.StartWindow || day.IsPredictedStartWindow
		legend.Fertility = legend.Fertility || inFertileWindow || day.IsFertility
		legend.PredictedPeriodInFertility = legend.PredictedPeriodInFertility || day.IsPredictedFertileOverlap || (day.IsPredictedStartWindow && inFertileWindow)
		legend.OvulationTentative = legend.OvulationTentative || day.IsTentativeOvulation
		legend.OvulationEstimate = legend.OvulationEstimate || day.IsOvulation
		legend.Today = legend.Today || day.IsToday
		legend.LoggedEntry = legend.LoggedEntry || day.HasData || day.HasSex
	}
	return legend
}
