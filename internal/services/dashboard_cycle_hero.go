package services

import (
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// dashboardCycleHeroMaxAxisDays bounds the day cells the ribbon renders. The
// axis normally ends at the reference cycle length, but an irregular account's
// predicted start window runs to MaxCycleLength, and a cycle merged by a missed
// period log makes that number arbitrarily large. Past this bound the window is
// drawn as far as the axis reaches — the status line still states its exact
// dates — rather than growing the DOM without limit.
const dashboardCycleHeroMaxAxisDays = 60

// phaseBeyondProjectedCycle labels the axis days past the projected cycle
// length: they carry no phase (the projection ended) and exist only because the
// predicted start window reaches them.
const phaseBeyondProjectedCycle = "beyond"

type DashboardCycleHero struct {
	Visible      bool
	Approximate  bool
	CurrentDay   int
	CycleLength  int
	AxisDays     int
	CurrentPhase string
	PhaseCards   []DashboardCycleHeroPhaseCard
	// Days is the ribbon itself: one cell per day of the axis, in order, each
	// carrying every encoding that day is under. The presentation is a row of
	// equal-width cells rather than SVG geometry so that no coordinate is
	// computed twice and no inline style is needed (strict CSP).
	Days []DashboardCycleHeroDay
}

type DashboardCycleHeroPhaseCard struct {
	Phase     string
	StartDay  int
	EndDay    int
	IsCurrent bool
}

// DashboardCycleHeroDay is one day column of the ribbon. Phase and fertility
// are two orthogonal axes (#416), so a day carries both independently, and the
// recorded/projected distinction is a third: it is what separates a fact from
// an estimate on a surface that shows them side by side.
type DashboardCycleHeroDay struct {
	Day             int
	Phase           string
	IsToday         bool
	IsProjected     bool
	IsLogged        bool
	IsFertile       bool
	IsFertilePeak   bool
	IsStartWindow   bool
	IsPredictedFlow bool
}

// dashboardCycleHeroInput is everything the ribbon needs beyond the cycle
// context: the account's own logs decide which days carry a recorded entry, and
// the location resolves them to calendar days.
type dashboardCycleHeroInput struct {
	Logs     []models.DailyLog
	Today    time.Time
	Location *time.Location
}

func BuildDashboardCycleHero(user *models.User, stats CycleStats, cycleContext DashboardCycleContext, input dashboardCycleHeroInput) DashboardCycleHero {
	cycleLength := DashboardCycleReferenceLength(user, stats)
	if !canRenderDashboardCycleHero(cycleLength, stats, cycleContext) {
		return DashboardCycleHero{}
	}

	periodLength := predictedPeriodLength(stats.AveragePeriodLength)
	if periodLength >= cycleLength {
		return DashboardCycleHero{}
	}

	location := input.Location
	if location == nil {
		location = time.UTC
	}
	cycleStart := CalendarDay(stats.LastPeriodStart, location)

	ovulationDay := dashboardCycleHeroOvulationDay(stats, cycleLength, cycleStart, location)
	if ovulationDay <= periodLength+1 || ovulationDay > cycleLength {
		return DashboardCycleHero{}
	}

	currentDay := stats.CurrentCycleDay
	currentPhase := dashboardCycleHeroCurrentPhase(stats.CurrentPhase, currentDay, periodLength, ovulationDay, cycleLength)
	phaseCards := dashboardCycleHeroPhaseCards(currentPhase, periodLength, ovulationDay, cycleLength)

	startWindow := dashboardCycleHeroStartWindow(user, stats, cycleStart, location)
	axisDays := dashboardCycleHeroAxisDays(cycleLength, startWindow)

	return DashboardCycleHero{
		Visible:      true,
		Approximate:  dashboardCycleHeroApproximate(cycleContext),
		CurrentDay:   currentDay,
		CycleLength:  cycleLength,
		AxisDays:     axisDays,
		CurrentPhase: currentPhase,
		PhaseCards:   phaseCards,
		Days: dashboardCycleHeroDays(
			axisDays,
			currentDay,
			phaseCards,
			startWindow,
			dashboardCycleHeroFertileSpan(stats, cycleContext, cycleStart, location),
			dashboardCycleHeroLoggedDays(input.Logs, cycleStart, input.Today, location),
			periodLength,
		),
	}
}

// dashboardCycleHeroOvulationDay anchors the phase-card geometry — and so the
// "ovulation" card and dashboardCycleHeroCurrentPhase's own fallback — on the
// same day dashboardCycleHeroFertileSpan already peaks on: the confirmed or
// published OvulationDate, read as a cycle day off the same cycleStart, rather
// than a second, independent CalcOvulationDay projection. Left on the
// projection, a confirmed shift moved the ribbon's fertile peak without moving
// the card labeled "ovulation" to sit on it. The bounds check the caller
// applies afterward is unchanged: it is the ribbon's own phase geometry, keyed
// to DashboardCycleReferenceLength (the average), and a confirmed day landing
// outside it is a legitimate refusal to render, not a defect.
func dashboardCycleHeroOvulationDay(stats CycleStats, cycleLength int, cycleStart time.Time, location *time.Location) int {
	if !stats.OvulationDate.IsZero() && !cycleStart.IsZero() {
		return CalendarDaysBetween(cycleStart, CalendarDay(stats.OvulationDate, location)) + 1
	}
	projected, _ := CalcOvulationDay(cycleLength, stats.LutealPhase)
	return projected
}

func canRenderDashboardCycleHero(cycleLength int, stats CycleStats, cycleContext DashboardCycleContext) bool {
	return cycleLength > 0 &&
		stats.CurrentCycleDay > 0 &&
		stats.CurrentCycleDay <= cycleLength &&
		!cycleContext.PredictionDisabled &&
		!cycleContext.CycleDataStale &&
		!cycleContext.DisplayNextPeriodPrompt &&
		!cycleContext.DisplayNextPeriodNeedsData &&
		!cycleContext.DisplayOvulationNeedsData &&
		!cycleContext.DisplayOvulationImpossible
}

func dashboardCycleHeroApproximate(cycleContext DashboardCycleContext) bool {
	return cycleContext.DisplayNextPeriodUseRange ||
		cycleContext.DisplayOvulationUseRange ||
		!cycleContext.DisplayOvulationExact
}

func dashboardCycleHeroPhaseCards(currentPhase string, periodLength int, ovulationDay int, cycleLength int) []DashboardCycleHeroPhaseCard {
	return []DashboardCycleHeroPhaseCard{
		{
			Phase:     "menstrual",
			StartDay:  1,
			EndDay:    periodLength,
			IsCurrent: currentPhase == "menstrual",
		},
		{
			Phase:     "follicular",
			StartDay:  periodLength + 1,
			EndDay:    ovulationDay - 1,
			IsCurrent: currentPhase == "follicular",
		},
		{
			Phase:     "ovulation",
			StartDay:  ovulationDay,
			EndDay:    ovulationDay,
			IsCurrent: currentPhase == "ovulation",
		},
		{
			Phase:     "luteal",
			StartDay:  ovulationDay + 1,
			EndDay:    cycleLength,
			IsCurrent: currentPhase == "luteal",
		},
	}
}

// dashboardCycleHeroDaySpan is a closed [StartDay, EndDay] run of cycle days,
// 1-based like the cycle day itself. A zero-value span covers nothing.
type dashboardCycleHeroDaySpan struct {
	StartDay int
	EndDay   int
	PeakDay  int
	Present  bool
}

func (span dashboardCycleHeroDaySpan) covers(day int) bool {
	return span.Present && day >= span.StartDay && day <= span.EndDay
}

// dashboardCycleHeroStartWindow is the run of days the NEXT period may start
// on, read from DashboardPredictionRange — the same definition the calendar
// grid shades and the status line prints, never a second derivation.
func dashboardCycleHeroStartWindow(user *models.User, stats CycleStats, cycleStart time.Time, location *time.Location) dashboardCycleHeroDaySpan {
	if cycleStart.IsZero() {
		return dashboardCycleHeroDaySpan{}
	}

	rangeStart, rangeEnd, hasRange := DashboardPredictionRange(user, stats, CalendarDay(stats.NextPeriodStart, location), location)
	if !hasRange {
		return dashboardCycleHeroDaySpan{}
	}
	return dashboardCycleHeroSpanFromDates(cycleStart, rangeStart, rangeEnd, time.Time{})
}

// dashboardCycleHeroFertileSpan is the fertile window as the calendar shades
// it, with the ovulation day as its peak. It rides ShowFertilityStatus's gate —
// before the first completed cycle the window is the onboarding slider projected
// forward, and the header withholds it for every goal (DashboardAwaitingFirstCycle).
func dashboardCycleHeroFertileSpan(stats CycleStats, cycleContext DashboardCycleContext, cycleStart time.Time, location *time.Location) dashboardCycleHeroDaySpan {
	if cycleContext.AwaitingFirstCycle || cycleStart.IsZero() {
		return dashboardCycleHeroDaySpan{}
	}
	return dashboardCycleHeroSpanFromDates(
		cycleStart,
		CalendarDay(stats.FertilityWindowStart, location),
		CalendarDay(stats.FertilityWindowEnd, location),
		CalendarDay(stats.OvulationDate, location),
	)
}

func dashboardCycleHeroSpanFromDates(cycleStart time.Time, start time.Time, end time.Time, peak time.Time) dashboardCycleHeroDaySpan {
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return dashboardCycleHeroDaySpan{}
	}

	startDay := CalendarDaysBetween(cycleStart, start) + 1
	endDay := CalendarDaysBetween(cycleStart, end) + 1
	if endDay < 1 {
		return dashboardCycleHeroDaySpan{}
	}
	if startDay < 1 {
		startDay = 1
	}

	span := dashboardCycleHeroDaySpan{StartDay: startDay, EndDay: endDay, Present: true}
	if !peak.IsZero() {
		span.PeakDay = CalendarDaysBetween(cycleStart, peak) + 1
	}
	return span
}

// dashboardCycleHeroLoggedDays reports which cycle days carry a recorded entry.
// Only days up to today can: a future day has nothing to record, and marking one
// would put a fact on the estimate side of the ribbon.
func dashboardCycleHeroLoggedDays(logs []models.DailyLog, cycleStart time.Time, today time.Time, location *time.Location) map[int]bool {
	if cycleStart.IsZero() {
		return nil
	}

	logged := make(map[int]bool, len(logs))
	for _, logEntry := range logs {
		if !DayHasData(logEntry) {
			continue
		}

		localDay := CalendarDay(logEntry.Date, location)
		if localDay.Before(cycleStart) || (!today.IsZero() && localDay.After(today)) {
			continue
		}

		dayNumber := CalendarDaysBetween(cycleStart, localDay) + 1
		if dayNumber >= 1 {
			logged[dayNumber] = true
		}
	}
	return logged
}

func dashboardCycleHeroAxisDays(cycleLength int, startWindow dashboardCycleHeroDaySpan) int {
	axisDays := cycleLength
	if startWindow.Present && startWindow.EndDay > axisDays {
		axisDays = startWindow.EndDay
	}
	if axisDays > dashboardCycleHeroMaxAxisDays {
		axisDays = dashboardCycleHeroMaxAxisDays
	}
	return axisDays
}

func dashboardCycleHeroDays(
	axisDays int,
	currentDay int,
	phaseCards []DashboardCycleHeroPhaseCard,
	startWindow dashboardCycleHeroDaySpan,
	fertile dashboardCycleHeroDaySpan,
	logged map[int]bool,
	periodLength int,
) []DashboardCycleHeroDay {
	days := make([]DashboardCycleHeroDay, 0, axisDays)
	for day := 1; day <= axisDays; day++ {
		days = append(days, DashboardCycleHeroDay{
			Day:         day,
			Phase:       dashboardCycleHeroPhaseForDay(day, phaseCards),
			IsToday:     day == currentDay,
			IsProjected: day > currentDay,
			IsLogged:    logged[day],
			// A recorded day is never repainted as an estimate: the fertile
			// shading and the projected-flow texture belong to what is still
			// ahead, and the fertile window keeps its shading in the past
			// because it is where the recorded temperatures were taken.
			IsFertile:       fertile.covers(day),
			IsFertilePeak:   fertile.Present && fertile.PeakDay == day,
			IsStartWindow:   startWindow.covers(day),
			IsPredictedFlow: day > currentDay && day <= periodLength,
		})
	}
	return days
}

func dashboardCycleHeroPhaseForDay(day int, phaseCards []DashboardCycleHeroPhaseCard) string {
	for _, card := range phaseCards {
		if day >= card.StartDay && day <= card.EndDay {
			return card.Phase
		}
	}
	return phaseBeyondProjectedCycle
}

func dashboardCycleHeroCurrentPhase(currentPhase string, currentDay int, periodLength int, ovulationDay int, cycleLength int) string {
	switch currentPhase {
	case "menstrual", "ovulation", "luteal", "follicular":
		return currentPhase
	}

	switch {
	case currentDay >= 1 && currentDay <= periodLength:
		return "menstrual"
	case currentDay == ovulationDay:
		return "ovulation"
	case currentDay > ovulationDay && currentDay <= cycleLength:
		return "luteal"
	default:
		return "follicular"
	}
}
