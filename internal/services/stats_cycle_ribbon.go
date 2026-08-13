package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The Insights cycle stack: the last few completed cycles drawn one under
// another on a shared axis, so their lengths compare by eye.
//
// It shares the dashboard ribbon's ENCODING CONTRACT — which colour means which
// phase, which mark means a recorded day — and nothing else. The two answer
// different questions: the dashboard band is one running cycle with a projected
// tail, this is four finished ones compared, and forcing a single component on
// them produces a compromise serving neither. What must not diverge is the
// meaning of a colour, and that lives in the --phase-* tokens both read.
//
// Everything here is RECORDED history. A completed cycle has an observed length
// and observed period days, so no projection is drawn over it — with one gated
// exception below.
const (
	statsCycleRibbonRows        = 4
	statsCycleRibbonMinCycles   = 2
	statsCycleRibbonMaxAxisDays = 60
)

type StatsCycleRibbon struct {
	Visible bool
	// AxisDays is the longest cycle in the stack, so every row is drawn against
	// the same scale and a longer cycle is visibly longer.
	AxisDays int
	// ShowPhases follows the owner's ShowHistoricalPhases preference, the same
	// switch that decides whether the calendar shades past cycles with inferred
	// phases. Off, the stack draws only what was recorded.
	ShowPhases bool
	Rows       []StatsCycleRibbonRow
}

type StatsCycleRibbonRow struct {
	Start        time.Time
	CycleLength  int
	PeriodLength int
	Days         []StatsCycleRibbonDay
}

type StatsCycleRibbonDay struct {
	Day int
	// InCycle is false for the axis days past this row's own length: the cell
	// exists so the rows line up, and paints nothing.
	InCycle bool
	// Phase is empty unless ShowPhases; menstrual always comes from the
	// observed period days rather than from a prediction.
	Phase         string
	IsPeriod      bool
	IsLogged      bool
	IsFertile     bool
	IsFertilePeak bool
}

// buildStatsCycleRibbon takes no location: buildCompletedCycleSpans has already
// resolved every span start to a calendar day, and a log's date is a date-only
// value read through CalendarDayKey. A second shift here would move an entry a
// day either way against the span it belongs to.
func buildStatsCycleRibbon(user *models.User, stats CycleStats, logs []models.DailyLog, completedCycles []completedCycleSpan) StatsCycleRibbon {
	if len(completedCycles) < statsCycleRibbonMinCycles {
		return StatsCycleRibbon{}
	}

	shown := completedCycles
	if len(shown) > statsCycleRibbonRows {
		shown = shown[len(shown)-statsCycleRibbonRows:]
	}

	axisDays := statsCycleRibbonAxisDays(shown)
	if axisDays <= 0 {
		return StatsCycleRibbon{}
	}

	showPhases := user != nil && user.ShowHistoricalPhases
	luteal := ResolveLutealPhase(stats.LutealPhase)
	loggedByDay := statsCycleRibbonLoggedDays(logs)

	rows := make([]StatsCycleRibbonRow, 0, len(shown))
	for _, cycle := range shown {
		rows = append(rows, statsCycleRibbonRow(cycle, axisDays, luteal, showPhases, loggedByDay))
	}

	return StatsCycleRibbon{
		Visible:    true,
		AxisDays:   axisDays,
		ShowPhases: showPhases,
		Rows:       rows,
	}
}

func statsCycleRibbonAxisDays(cycles []completedCycleSpan) int {
	axisDays := 0
	for _, cycle := range cycles {
		if cycle.CycleLength > axisDays {
			axisDays = cycle.CycleLength
		}
	}
	if axisDays > statsCycleRibbonMaxAxisDays {
		axisDays = statsCycleRibbonMaxAxisDays
	}
	return axisDays
}

// statsCycleRibbonLoggedDays maps every calendar day carrying an entry with
// data. Keyed by day rather than per cycle so the whole history is walked once
// however many rows the stack shows. DailyLog.Date is a date-only value stored
// at UTC midnight, so it is read through CalendarDayKey directly — no location
// shift, which would move an entry a day either way.
func statsCycleRibbonLoggedDays(logs []models.DailyLog) map[string]bool {
	logged := make(map[string]bool, len(logs))
	for _, logEntry := range logs {
		if !DayHasData(logEntry) {
			continue
		}
		logged[CalendarDayKey(logEntry.Date)] = true
	}
	return logged
}

func statsCycleRibbonRow(cycle completedCycleSpan, axisDays int, luteal int, showPhases bool, logged map[string]bool) StatsCycleRibbonRow {
	window := PredictCycleWindow(cycle.Start, cycle.CycleLength, luteal)
	days := make([]StatsCycleRibbonDay, 0, axisDays)

	for day := 1; day <= axisDays; day++ {
		if day > cycle.CycleLength {
			days = append(days, StatsCycleRibbonDay{Day: day})
			continue
		}

		date := cycle.Start.AddDate(0, 0, day-1)
		isPeriod := day <= cycle.PeriodLength
		cell := StatsCycleRibbonDay{
			Day:      day,
			InCycle:  true,
			IsPeriod: isPeriod,
			IsLogged: logged[CalendarDayKey(date)],
		}

		if showPhases {
			cell.Phase = statsCycleRibbonPhase(day, cycle.PeriodLength, window, cycle.Start)
			if window.Calculable {
				cell.IsFertile = !date.Before(window.FertilityWindowStart) && !date.After(window.FertilityWindowEnd)
				cell.IsFertilePeak = date.Equal(window.OvulationDate)
			}
		} else if isPeriod {
			// With inferred phases off, the observed period days are still a
			// recorded fact and keep their own colour; the rest of the row is
			// the bare cycle length, which is what the stack is for.
			cell.Phase = "menstrual"
		}

		days = append(days, cell)
	}

	return StatsCycleRibbonRow{
		Start:        cycle.Start,
		CycleLength:  cycle.CycleLength,
		PeriodLength: cycle.PeriodLength,
		Days:         days,
	}
}

// statsCycleRibbonPhase resolves one day of a COMPLETED cycle. Menstrual is the
// observed period length, never a predicted one — that is the difference
// between this stack and the dashboard band, where the period days still ahead
// are an estimate.
func statsCycleRibbonPhase(day int, periodLength int, window CycleWindowPrediction, cycleStart time.Time) string {
	if day <= periodLength {
		return "menstrual"
	}
	if !window.Calculable {
		return "follicular"
	}

	ovulationDay := CalendarDaysBetween(cycleStart, window.OvulationDate) + 1
	switch {
	case day == ovulationDay:
		return "ovulation"
	case day > ovulationDay:
		return "luteal"
	default:
		return "follicular"
	}
}
