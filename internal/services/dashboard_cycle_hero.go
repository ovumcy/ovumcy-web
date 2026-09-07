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

// phaseFertilityWithheld labels the axis days a suppressed tier still HAS —
// the cycle is running — but whose phase is defined in terms of the ovulation
// day the shared fertility gate withholds. It is deliberately not
// phaseBeyondProjectedCycle: "beyond" means the projection ENDED and the
// ribbon paints nothing there, so letting these days fall through to it told a
// mid-cycle owner that tracking had stopped rather than that the fertile
// details were held back.
const phaseFertilityWithheld = "withheld"

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

	// FertilitySuppressed is FertilityProjectionSuppressed(user, stats) resolved
	// once on the context — the same predicate the fertile-span peak below
	// already deferred to (dashboardCycleHeroFertileSpan) and the one
	// PublishedStats clears the fertility fields against. It, not
	// AwaitingFirstCycle alone, is the gate here: AwaitingFirstCycle is only ONE
	// of the reasons this predicate suppresses (unpredictable-cycle mode and a
	// pregnancy pause are the others), and the hero is built from
	// confirmedStats — the pre-publication copy — so it must defer to the same
	// floor PublishedStats applies rather than a narrower one of its own.
	fertilitySuppressed := cycleContext.FertilitySuppressed

	ovulationDay, confirmedAnchor := dashboardCycleHeroOvulationDay(stats, cycleContext, cycleLength, cycleStart, location)
	if confirmedAnchor {
		// A confirmed day is an OBSERVATION; the projected periodLength above it
		// is a projection of the average. When they disagree the observation
		// narrows the menstrual/follicular split (and every card and cell that
		// reads periodLength below), rather than the projection hiding the whole
		// ribbon out from under the cohort that tracks its temperatures most
		// closely. The only refusal left here is geometric impossibility —
		// see the comment on dashboardCycleHeroOvulationDay for why the confirmed
		// branch drops the periodLength+1 floor the projected branch still needs.
		if ovulationDay < 2 || ovulationDay > cycleLength {
			return DashboardCycleHero{}
		}
		periodLength = min(periodLength, ovulationDay-1)
	} else if ovulationDay <= periodLength+1 || ovulationDay > cycleLength {
		return DashboardCycleHero{}
	}

	currentDay := stats.CurrentCycleDay
	// The ribbon geometry (axis, day cells, the "today" marker) stays visible
	// under suppression — the owner rejected hiding the whole hero for this —
	// but the parts that NAME the ovulation day go quiet: the current-phase
	// label and the phase-card breakdown both stop revealing anything past the
	// menstrual card, which is read off recorded bleeding rather than the
	// suppressed ovulation projection.
	currentPhase := dashboardCycleHeroCurrentPhase(stats.CurrentPhase, currentDay, periodLength, ovulationDay, cycleLength, fertilitySuppressed)
	phaseCards := dashboardCycleHeroPhaseCards(currentPhase, periodLength, ovulationDay, cycleLength, fertilitySuppressed)

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
// same day dashboardCycleHeroFertileSpan already peaks on: the confirmed
// OvulationDate, read as a cycle day off the same cycleStart, rather than a
// second, independent CalcOvulationDay projection. Left unconditional, this
// anchor also fires for an account with no confirmed shift, whose
// stats.OvulationDate is a MEDIAN-driven projection (DashboardProjectionCycleLength)
// while the ribbon's own geometry — cycleLength here — is the AVERAGE
// (DashboardCycleReferenceLength); a median well above the average lands that
// date's cycle day outside the ribbon and the bounds check below silently
// hides the hero for an owner who never recorded a thermal shift. So the
// confirmed date is only trusted when cycleContext.DisplayOvulationConfirmed
// says a resolver actually substituted it; otherwise this falls back to the
// same CalcOvulationDay(cycleLength, LutealPhase) projection used before
// confirmed-shift anchoring existed.
//
// The two callers apply two DIFFERENT bounds, because the two returned days
// carry different guarantees. The projected branch's caller still requires
// ovulationDay > periodLength+1: CalcOvulationDay is itself a projection
// built from the SAME AveragePeriodLength periodLength projects, so a day it
// places inside or one after the projected period is that projection
// disagreeing with itself, not a real cycle — a legitimate refusal to render.
// The confirmed branch's caller drops that floor to the geometric minimum
// (ovulationDay >= 2): the day is an OBSERVATION the detector read off
// recorded temperatures, periodLength is still a projection of the average,
// and when an observation lands inside or before the projected period the
// observation narrows periodLength instead — the projection has no standing
// to hide a ribbon an observation can place. Only ovulationDay > cycleLength
// stays a refusal on both branches: that is a day past the ribbon's own axis,
// which no clamp on periodLength can fix.
// A suppressed tier never trusts the confirmed anchor: ResolveConfirmedCycleStats
// and PublishedStats already withhold the confirmed OvulationDate for exactly
// this cohort, and letting the hero substitute it anyway — even only to narrow
// periodLength below — would place the observation's day one past the
// menstrual card the ribbon still draws, naming it by construction.
func dashboardCycleHeroOvulationDay(stats CycleStats, cycleContext DashboardCycleContext, cycleLength int, cycleStart time.Time, location *time.Location) (int, bool) {
	if !cycleContext.FertilitySuppressed && cycleContext.DisplayOvulationConfirmed && !stats.OvulationDate.IsZero() && !cycleStart.IsZero() {
		return CalendarDaysBetween(cycleStart, CalendarDay(stats.OvulationDate, location)) + 1, true
	}
	projected, _ := CalcOvulationDay(cycleLength, stats.LutealPhase)
	return projected, false
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

// dashboardCycleHeroPhaseCards splits the cycle into cards for the ribbon's
// phase breakdown. The menstrual card is the only NAMED phase that survives
// suppression: it is read off recorded bleeding (periodLength), never off the
// ovulation day, so it carries nothing the shared fertility gate withholds.
// The follicular, ovulation and luteal cards all have a boundary defined IN
// TERMS OF ovulationDay — cutting any one of them in would still spell the day
// out through its StartDay/EndDay, so all three drop together rather than only
// the "ovulation" card whose name is the obvious tell. What replaces them is
// one card carrying no boundary of its own, so that the days keep a status
// instead of falling through to "beyond".
func dashboardCycleHeroPhaseCards(currentPhase string, periodLength int, ovulationDay int, cycleLength int, fertilitySuppressed bool) []DashboardCycleHeroPhaseCard {
	cards := []DashboardCycleHeroPhaseCard{
		{
			Phase:     "menstrual",
			StartDay:  1,
			EndDay:    periodLength,
			IsCurrent: currentPhase == "menstrual",
		},
	}
	if fertilitySuppressed {
		// periodLength < cycleLength is the caller's invariant, not a case to
		// re-test: BuildDashboardCycleHero returns an empty hero above when the
		// projected period fills the cycle, and the confirmed branch only ever
		// lowers periodLength. The three unsuppressed cards below rest on the
		// same guarantee, and guarding only this one would have bought a test
		// that exercises a state the ribbon cannot be built in.
		cards = append(cards, DashboardCycleHeroPhaseCard{
			Phase:     phaseFertilityWithheld,
			StartDay:  periodLength + 1,
			EndDay:    cycleLength,
			IsCurrent: currentPhase == phaseFertilityWithheld,
		})
		return cards
	}
	return append(cards,
		DashboardCycleHeroPhaseCard{
			Phase:     "follicular",
			StartDay:  periodLength + 1,
			EndDay:    ovulationDay - 1,
			IsCurrent: currentPhase == "follicular",
		},
		DashboardCycleHeroPhaseCard{
			Phase:     "ovulation",
			StartDay:  ovulationDay,
			EndDay:    ovulationDay,
			IsCurrent: currentPhase == "ovulation",
		},
		DashboardCycleHeroPhaseCard{
			Phase:     "luteal",
			StartDay:  ovulationDay + 1,
			EndDay:    cycleLength,
			IsCurrent: currentPhase == "luteal",
		},
	)
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
// it, with the ovulation day as its peak. It gates on FertilitySuppressed —
// FertilityProjectionSuppressed(user, stats) resolved once on the context —
// rather than AwaitingFirstCycle alone: the first-cycle floor is only ONE of
// the reasons that predicate withholds the fertile half (unpredictable-cycle
// mode and a pregnancy pause are the other two), and this span must defer to
// the same floor every other fertility surface — the calendar grid, the .ics
// feed, the published stats copy — already defers to, or an account
// suppressed for one of those other two reasons would still see the peak here.
func dashboardCycleHeroFertileSpan(stats CycleStats, cycleContext DashboardCycleContext, cycleStart time.Time, location *time.Location) dashboardCycleHeroDaySpan {
	if cycleContext.FertilitySuppressed || cycleStart.IsZero() {
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

// dashboardCycleHeroCurrentPhase names today's phase. The resolution below is
// unchanged from before suppression was a concern; fertilitySuppressed only
// filters the RESULT, because "menstrual" is read off recorded bleeding
// (periodLength) while every other branch is defined in terms of
// ovulationDay — the follicular/ovulation/luteal split the fertility gate
// withholds. A suppressed tier keeps "menstrual" and answers "withheld" for
// anything else, rather than resolving a phase whose boundary names the day.
// The status line and the ribbon cells below it are one widget answering about
// one cycle, so the header says the same word the cells do; a second spelling
// for the same suppression is a second answer.
func dashboardCycleHeroCurrentPhase(currentPhase string, currentDay int, periodLength int, ovulationDay int, cycleLength int, fertilitySuppressed bool) string {
	resolved := currentPhase
	switch currentPhase {
	case "menstrual", "ovulation", "luteal", "follicular":
		// Trust the caller's own classification as-is.
	default:
		switch {
		case currentDay >= 1 && currentDay <= periodLength:
			resolved = "menstrual"
		case currentDay == ovulationDay:
			resolved = "ovulation"
		case currentDay > ovulationDay && currentDay <= cycleLength:
			resolved = "luteal"
		default:
			resolved = "follicular"
		}
	}

	if fertilitySuppressed && resolved != "menstrual" {
		return phaseFertilityWithheld
	}
	return resolved
}
