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
	// statsCycleRibbonOvulationPhase is the one phase value that names the
	// ovulation day; the fertile peak is the same claim in a second encoding
	// and reads it rather than recomputing it.
	statsCycleRibbonOvulationPhase = "ovulation"
)

type StatsCycleRibbon struct {
	Visible bool
	// AxisDays is the longest cycle in the stack, so every row is drawn against
	// the same scale and a longer cycle is visibly longer.
	AxisDays int
	// ShowPhases follows the owner's ShowHistoricalPhases preference, the same
	// switch that decides whether the calendar shades past cycles with inferred
	// phases — and, above it, the medical-safety suppression gate every projected
	// surface shares. Off, the stack draws only what was recorded.
	ShowPhases bool
	// AxisTruncated reports that at least one cycle is longer than the axis the
	// DOM bound allows, so the stack is no longer a complete comparison.
	AxisTruncated bool
	Rows          []StatsCycleRibbonRow
}

type StatsCycleRibbonRow struct {
	Start        time.Time
	CycleLength  int
	PeriodLength int
	// Truncated marks a row whose cycle outran the axis: its band stops at the
	// cap rather than at the cycle's own end, so its length may not be read
	// off the drawing — only off CycleLength beside it. Two such rows are the
	// same width whatever their lengths, and without this flag nothing said so.
	Truncated bool
	Days      []StatsCycleRibbonDay
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

	// Inferred phases need BOTH the owner's preference and the medical-safety
	// suppression gate open. Everything the phase axis adds to a row — the
	// fertile window, the peak day, the ovulation cell and the luteal span that
	// is only located relative to it — is PredictCycleWindow applied to a past
	// cycle, i.e. the same cycle math the account has told the product not to
	// trust. The calendar settles that reading: its historical-phase pass
	// (appendHistoricalCycles) runs below buildCalendarPredictionMaps' early
	// return on PredictionsSuppressed, so inferred history is not exempt there
	// and must not be exempt here. The gate is the shared predicate, never a
	// hand-picked disjunct, so a fourth suppression signal reaches this surface
	// with the others.
	showPhases := user != nil && user.ShowHistoricalPhases && !PredictionsSuppressed(user, stats)
	luteal := ResolveLutealPhase(stats.LutealPhase)
	loggedByDay := statsCycleRibbonLoggedDays(logs)

	rows := make([]StatsCycleRibbonRow, 0, len(shown))
	axisTruncated := false
	for _, cycle := range shown {
		row := statsCycleRibbonRow(cycle, axisDays, luteal, showPhases, loggedByDay)
		axisTruncated = axisTruncated || row.Truncated
		rows = append(rows, row)
	}

	return StatsCycleRibbon{
		Visible:       true,
		AxisDays:      axisDays,
		ShowPhases:    showPhases,
		AxisTruncated: axisTruncated,
		Rows:          rows,
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
	// A clamped window is not an estimate of the ovulation day, it is the
	// absence of one: CalcOvulationDay reports OvulationExact=false when the
	// luteal phase had to be SHORTENED to fit the cycle at all, which it does
	// for every completed cycle under 19 days. The day it returns is then the
	// earliest one the arithmetic allows rather than anything the account's own
	// data locates. Display confidence follows data confidence, and the cells
	// here are colour with no wording to qualify them — the dashboard's
	// "approximate" line has no counterpart on this surface — so suppression is
	// the floor and every ovulation-derived mark comes off the row. The
	// recorded length and the recorded period days are facts and stay.
	locatedOvulation := window.Calculable && window.OvulationExact
	days := make([]StatsCycleRibbonDay, 0, axisDays)

	for day := 1; day <= axisDays; day++ {
		if day > cycle.CycleLength {
			days = append(days, StatsCycleRibbonDay{Day: day})
			continue
		}

		date := AddCalendarDays(cycle.Start, day-1, cycle.Start.Location())
		isPeriod := day <= cycle.PeriodLength
		cell := StatsCycleRibbonDay{
			Day:      day,
			InCycle:  true,
			IsPeriod: isPeriod,
			IsLogged: logged[CalendarDayKey(date)],
		}

		if showPhases {
			cell.Phase = statsCycleRibbonPhase(day, cycle.PeriodLength, window, cycle.Start)
			if locatedOvulation {
				// date is a location midnight (cycle.Start carries the row's own
				// location) while the window bounds are UTC-midnight date-only
				// values, so both ends are compared as calendar days — exactly as
				// the phase axis above already does through CalendarDaysBetween.
				// As instants the pair drops the last fertile day in a UTC-minus
				// zone and the first one in a UTC-plus zone, and the peak equality
				// is never true outside UTC at all (issue #48 class).
				cell.IsFertile = CalendarDaysBetween(window.FertilityWindowStart, date) >= 0 &&
					CalendarDaysBetween(date, window.FertilityWindowEnd) >= 0
				// The peak and the ovulation phase are ONE claim in two
				// encodings, so the peak is read off the phase the row has
				// already resolved instead of recomputed beside it. Computed
				// twice they disagreed: a fertile day may overlap the period —
				// the two axes are orthogonal (#416) — but on a short cycle the
				// ovulation day itself landed inside the recorded period days,
				// where the phase taxonomy spends its one value on "menstrual",
				// and the cell then carried a period mark and a peak mark at
				// once with only CSS precedence deciding which an owner saw.
				cell.IsFertilePeak = cell.Phase == statsCycleRibbonOvulationPhase
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
		Truncated:    cycle.CycleLength > axisDays,
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
	// The same floor the fertility marks take: an ovulation day the luteal
	// phase had to be clamped to produce is not located by this cycle's data,
	// so the row paints neither the ovulation cell nor the luteal span, which
	// is only positioned relative to it. What remains is the follicular
	// fallback the no-window case already uses.
	if !window.Calculable || !window.OvulationExact {
		return "follicular"
	}

	ovulationDay := CalendarDaysBetween(cycleStart, window.OvulationDate) + 1
	switch {
	case day == ovulationDay:
		return statsCycleRibbonOvulationPhase
	case day > ovulationDay:
		return "luteal"
	default:
		return "follicular"
	}
}
