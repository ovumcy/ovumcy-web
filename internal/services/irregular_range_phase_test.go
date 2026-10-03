package services

// irregular_range_phase_test.go — what the irregular range mode's widened
// fertile window does to the PHASE, to the calendar cells it shares with the
// next projected period, and which ovulation days it must contain.
//
// The window runs to the longest recent cycle's ovulation while the published
// ovulation day stays the median one, so the days between the two are called
// fertile because the ovulation may still be ahead. "Luteal" says it is behind.

import (
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// rangeModeOvertakingFixture is an owner in irregular-cycle mode with completed
// cycles of 25, 45 and 28 days (five logged bleeding days each) and the running
// cycle starting 2026-05-01. With the 14-day default luteal phase:
//   - the median (28) ovulation is cycle day 14, 2026-05-14;
//   - the shortest cycle (25) ovulates on day 11, so the window opens 05-06;
//   - the longest cycle (45) ovulates on day 31, 05-31 — the window's last day;
//   - the next period is projected on day 29, 05-29, before that last day.
func rangeModeOvertakingFixture(t *testing.T) (*models.User, []models.DailyLog) {
	t.Helper()

	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, IrregularCycle: true}
	logs := make([]models.DailyLog, 0, 20)
	for _, start := range []string{"2026-01-23", "2026-02-17", "2026-04-03", "2026-05-01"} {
		first := mustParseDay(t, start)
		for offset := range 5 {
			logs = append(logs, models.DailyLog{Date: first.AddDate(0, 0, offset), IsPeriod: true, CycleStart: offset == 0, Flow: models.FlowMedium})
		}
	}
	return user, logs
}

// logsUpTo keeps the entries a real account could hold on that day.
func logsUpTo(logs []models.DailyLog, today time.Time) []models.DailyLog {
	kept := make([]models.DailyLog, 0, len(logs))
	for _, entry := range logs {
		if !entry.Date.After(today) {
			kept = append(kept, entry)
		}
	}
	return kept
}

func assertRangeModeFixture(t *testing.T, user *models.User, stats CycleStats) {
	t.Helper()

	if stats.MinCycleLength != 25 || stats.MedianCycleLength != 28 || stats.MaxCycleLength != 45 {
		t.Fatalf("fixture: min/median/max = %d/%d/%d, want 25/28/45", stats.MinCycleLength, stats.MedianCycleLength, stats.MaxCycleLength)
	}
	if !dashboardIrregularPredictionRangeEnabled(user, stats) {
		t.Fatal("fixture: the account must be in the irregular range mode")
	}
}

// TestRangeModePublishesNoPhaseThatPlacesTheOvulationOnAFertileDay walks every
// day of the running cycle and asks each phase producer — the owner baseline
// (DetectCurrentPhase), the published copy the JSON API and both pages read,
// and the dashboard hero's label, which the dashboard header prints in place of
// the published phase — whether it pairs a projection-basis "fertile" with
// "luteal". "Menstrual" beside it is no contradiction: the window opens five days
// before the shortest cycle's ovulation, which for a short cycle is still a
// projected or logged period day.
func TestRangeModePublishesNoPhaseThatPlacesTheOvulationOnAFertileDay(t *testing.T) {
	user, allLogs := rangeModeOvertakingFixture(t)
	cycleStart := mustParseDay(t, "2026-05-01")

	bandDays, heroBandDays := 0, 0
	for cycleDay := 1; cycleDay <= 40; cycleDay++ {
		today := cycleStart.AddDate(0, 0, cycleDay-1)
		logs := logsUpTo(allLogs, today)
		stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
		assertRangeModeFixture(t, user, stats)

		published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
		cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
		hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})

		producers := []struct {
			name, phase, status, basis string
		}{
			{"owner baseline", stats.CurrentPhase, stats.CurrentFertility, stats.FertilityBasis},
			{"published copy", published.CurrentPhase, published.CurrentFertility, published.FertilityBasis},
		}
		if hero.Visible {
			producers = append(producers, struct{ name, phase, status, basis string }{"dashboard hero", hero.CurrentPhase, published.CurrentFertility, published.FertilityBasis})
		}
		for _, producer := range producers {
			if producer.basis != FertilityBasisProjection || producer.status != FertilityStatusFertile {
				continue
			}
			if producer.phase == "luteal" {
				t.Errorf("cycle day %d (%s): the %s publishes phase %q beside a projected %q — window %s..%s, ovulation %s",
					cycleDay, CalendarDayKey(today), producer.name, producer.phase, producer.status,
					CalendarDayKey(stats.FertilityWindowStart), CalendarDayKey(stats.FertilityWindowEnd), CalendarDayKey(stats.OvulationDate))
			}
		}

		inBand := today.After(stats.OvulationDate) && !today.After(stats.FertilityWindowEnd)
		if inBand {
			bandDays++
			if stats.CurrentPhase != "unknown" || published.CurrentPhase != "unknown" {
				t.Errorf("cycle day %d: phase %q / published %q, want unknown after the median ovulation inside the widened window", cycleDay, stats.CurrentPhase, published.CurrentPhase)
			}
			if hero.Visible {
				heroBandDays++
				if hero.CurrentPhase != "unknown" {
					t.Errorf("cycle day %d: the dashboard hero says %q, want unknown as the published phase says", cycleDay, hero.CurrentPhase)
				}
			}
		}
	}
	// Anti-vacuity: days 15..31 are the band, the hero is drawn on some of them
	// (its axis is the 33-day reference), and three of them (29..31) lie past
	// the projected next period.
	if bandDays != 17 {
		t.Fatalf("the walk met %d band days, want 17 (cycle days 15..31)", bandDays)
	}
	if heroBandDays == 0 {
		t.Fatal("the dashboard hero was drawn on no band day, so its label was never compared")
	}

	// The positive control: past the widened window, and before the data goes
	// out of date on day 34, the ovulation is behind every estimate and the
	// phase is named again.
	today := cycleStart.AddDate(0, 0, 31)
	logs := logsUpTo(allLogs, today)
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	if stats.CurrentPhase != "luteal" || published.CurrentPhase != "luteal" || published.CurrentFertility != FertilityStatusOutsideEstimatedWindow {
		t.Fatalf("cycle day 32: phase %q / published %q status %q, want luteal outside the window", stats.CurrentPhase, published.CurrentPhase, published.CurrentFertility)
	}
}

// TestRangeModeHeroKeepsABleedingDayLoggedInsideTheBand: a period day logged
// past the median ovulation without a cycle-start mark (bleeding the owner did
// not call a new cycle) leaves the baseline on the explicit start, so the band
// and its widened window stay. resolveCyclePhase answers "menstrual" off the log
// before it asks ovulationTimingUndetermined; the hero's label — what the
// dashboard header prints — must not turn that into "unknown".
func TestRangeModeHeroKeepsABleedingDayLoggedInsideTheBand(t *testing.T) {
	user, allLogs := rangeModeOvertakingFixture(t)
	today := mustParseDay(t, "2026-05-20")
	logs := append(logsUpTo(allLogs, today), models.DailyLog{Date: today, IsPeriod: true, Flow: models.FlowLight})
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)

	if got := CalendarDayKey(stats.LastPeriodStart); got != "2026-05-01" {
		t.Fatalf("fixture: the unmarked bleeding day moved the anchor to %s, want 2026-05-01", got)
	}
	if !dashboardIrregularPredictionRangeEnabled(user, stats) || !ovulationTimingUndetermined(stats, today) {
		t.Fatalf("fixture: range mode %v, window %s..%s, ovulation %s — today must sit in the band",
			dashboardIrregularPredictionRangeEnabled(user, stats), CalendarDayKey(stats.FertilityWindowStart),
			CalendarDayKey(stats.FertilityWindowEnd), CalendarDayKey(stats.OvulationDate))
	}

	published, _, _ := PublishedOverviewStats(user, logs, stats, today, time.UTC)
	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})
	if !hero.Visible {
		t.Fatal("the dashboard hero was not drawn, so its label was never compared")
	}
	if stats.CurrentPhase != "menstrual" || published.CurrentPhase != "menstrual" {
		t.Fatalf("phase %q / published %q, want menstrual off the logged bleeding day", stats.CurrentPhase, published.CurrentPhase)
	}
	if hero.CurrentPhase != published.CurrentPhase {
		t.Fatalf("the dashboard hero says %q while the published phase is %q", hero.CurrentPhase, published.CurrentPhase)
	}
}

// TestRangeModeHeroRibbonPaintsNoFertileCellLuteal: the hero's cells answer
// what its header answers. Every ribbon cell the widened window covers past the
// published ovulation is "unknown", the day after the window is luteal again,
// and the regular-mode ribbon of the same history keeps its four cards. The
// fixture's 33-day reference projects the ribbon's own ovulation on day 19,
// five days after the published one, so a walk over days 15..19 is what tells
// the two anchors apart.
func TestRangeModeHeroRibbonPaintsNoFertileCellLuteal(t *testing.T) {
	user, allLogs := rangeModeOvertakingFixture(t)
	cycleStart := mustParseDay(t, "2026-05-01")
	walkedBand := 0
	for cycleDay := 1; cycleDay <= 33; cycleDay++ {
		today := cycleStart.AddDate(0, 0, cycleDay-1)
		logs := logsUpTo(allLogs, today)
		stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
		cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
		hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})
		if !hero.Visible {
			continue
		}
		for _, day := range hero.Days {
			if day.IsToday && day.Phase != hero.CurrentPhase && hero.CurrentPhase != "menstrual" {
				t.Errorf("cycle day %d: today's cell says %q while the header says %q", cycleDay, day.Phase, hero.CurrentPhase)
			}
		}
		for _, card := range hero.PhaseCards {
			if card.IsCurrent && (cycleDay < card.StartDay || cycleDay > card.EndDay) {
				t.Errorf("cycle day %d: the current card %q covers days %d..%d, not today", cycleDay, card.Phase, card.StartDay, card.EndDay)
			}
		}
		if cycleDay >= 15 && cycleDay <= 19 && hero.CurrentPhase == "unknown" {
			walkedBand++
		}
	}
	if walkedBand != 5 {
		t.Fatalf("the walk met %d of the band days 15..19 with an unknown header, want 5", walkedBand)
	}

	today := mustParseDay(t, "2026-05-20")
	logs := logsUpTo(allLogs, today)
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	assertRangeModeFixture(t, user, stats)

	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	hero := BuildDashboardCycleHero(user, stats, cycleContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})
	if !hero.Visible {
		t.Fatal("the dashboard hero was not drawn, so its cells were never compared")
	}

	windowEndDay := CalendarDaysBetween(stats.LastPeriodStart, stats.FertilityWindowEnd) + 1
	unknownCells := 0
	for _, day := range hero.Days {
		if day.IsFertile && day.Phase == "luteal" {
			t.Errorf("cycle day %d: a fertile cell is painted luteal", day.Day)
		}
		if day.Phase == "unknown" {
			unknownCells++
			if !day.IsFertile {
				t.Errorf("cycle day %d: an unknown cell outside the fertile window", day.Day)
			}
		}
		if day.Day == windowEndDay+1 && day.Phase != "luteal" {
			t.Errorf("cycle day %d (the day after the window): phase %q, want luteal", day.Day, day.Phase)
		}
		if day.IsToday && day.Phase != hero.CurrentPhase {
			t.Errorf("today's cell says %q while the header says %q", day.Phase, hero.CurrentPhase)
		}
	}
	if unknownCells == 0 {
		t.Fatal("the ribbon drew no unknown cell, so the band was never painted")
	}
	current := 0
	for _, card := range hero.PhaseCards {
		if card.IsCurrent {
			current++
			if card.Phase != "unknown" {
				t.Errorf("the current card is %q, want unknown as the header says", card.Phase)
			}
		}
	}
	if current != 1 {
		t.Errorf("%d current cards, want exactly one", current)
	}

	user.IrregularCycle = false
	regularStats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	regularContext := BuildDashboardCycleContext(user, logs, regularStats, today, time.UTC)
	regular := BuildDashboardCycleHero(user, regularStats, regularContext, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})
	if !regular.Visible {
		t.Fatal("control: the regular-mode hero was not drawn")
	}
	phases := make([]string, 0, len(regular.PhaseCards))
	for _, card := range regular.PhaseCards {
		phases = append(phases, card.Phase)
	}
	if got := strings.Join(phases, ","); got != "menstrual,follicular,ovulation,luteal" {
		t.Fatalf("control: regular-mode cards %s, want the four named phases", got)
	}
}

// TestRegularModeKeepsTheLutealPhaseAfterTheOvulation is the control for the
// rule's reach: the same history without the irregular setting keeps the
// median window, which ends on its own ovulation day, so the day after it is
// luteal as it always was.
func TestRegularModeKeepsTheLutealPhaseAfterTheOvulation(t *testing.T) {
	user, allLogs := rangeModeOvertakingFixture(t)
	user.IrregularCycle = false

	today := mustParseDay(t, "2026-05-20")
	logs := logsUpTo(allLogs, today)
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	if !sameDay(stats.FertilityWindowEnd, stats.OvulationDate) {
		t.Fatalf("fixture: the median window must end on its ovulation day, got %s vs %s", CalendarDayKey(stats.FertilityWindowEnd), CalendarDayKey(stats.OvulationDate))
	}
	if stats.CurrentPhase != "luteal" || stats.CurrentFertility != FertilityStatusOutsideEstimatedWindow {
		t.Fatalf("phase %q status %q, want luteal outside the window", stats.CurrentPhase, stats.CurrentFertility)
	}
}

// TestRangeModeWindowOutrunningTheNextPeriodResolvesOneAnswerPerCell: the
// widened window is not clamped at the projected next period — that would
// under-claim the days the longest cycle can still ovulate on — so the cells
// they share carry both facts, and the overlap reading the precedence ladder
// resolves them by is set on each.
func TestRangeModeWindowOutrunningTheNextPeriodResolvesOneAnswerPerCell(t *testing.T) {
	user, allLogs := rangeModeOvertakingFixture(t)
	today := mustParseDay(t, "2026-05-30")
	logs := logsUpTo(allLogs, today)
	stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
	assertRangeModeFixture(t, user, stats)

	if got := CalendarDayKey(stats.NextPeriodStart); got != "2026-05-29" {
		t.Fatalf("fixture: next period = %s, want 2026-05-29", got)
	}
	if got := CalendarDayKey(stats.FertilityWindowEnd); got != "2026-05-31" {
		t.Fatalf("window end = %s, want 2026-05-31 — the status window is not clamped at the next period", got)
	}
	if stats.CurrentFertility != FertilityStatusFertile || stats.CurrentPhase != "unknown" {
		t.Fatalf("cycle day 30: status %q phase %q, want fertile with no phase", stats.CurrentFertility, stats.CurrentPhase)
	}

	cells := map[string]CalendarDayState{}
	for _, cell := range BuildCalendarDayStates(user, mustParseDay(t, "2026-05-01"), logs, stats, today, time.UTC) {
		cells[cell.DateString] = cell
	}
	for _, day := range []string{"2026-05-29", "2026-05-30", "2026-05-31"} {
		cell := cells[day]
		fertile := cell.IsFertilityEdge || cell.IsFertilityPeak
		if !cell.IsPredicted || !fertile {
			t.Fatalf("%s: predicted %v fertile %v, want both — neither fact is dropped", day, cell.IsPredicted, fertile)
		}
		if !cell.IsPredictedFertileOverlap || !cell.IsPredictedStartWindow {
			t.Fatalf("%s: overlap %v start window %v, want both set so the ladder paints one answer", day, cell.IsPredictedFertileOverlap, cell.IsPredictedStartWindow)
		}
	}
	// The day after the window keeps the projected period alone.
	if after := cells["2026-06-01"]; !after.IsPredicted || after.IsPredictedFertileOverlap || after.IsFertilityEdge {
		t.Fatalf("2026-06-01: predicted %v overlap %v fertile %v, want the projected period alone", after.IsPredicted, after.IsPredictedFertileOverlap, after.IsFertilityEdge)
	}
}

// TestRangeModeWindowContainsEveryOvulationTheSpreadAllows: for the stats the
// window is built from, it contains the shortest and the longest recent
// cycle's ovulation days, as PredictCycleWindow places them.
func TestRangeModeWindowContainsEveryOvulationTheSpreadAllows(t *testing.T) {
	user := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, IrregularCycle: true}
	current := mustParseDay(t, "2026-06-01")
	checked := 0
	for shortest := 16; shortest <= 40; shortest++ {
		for longest := shortest; longest <= 60; longest += 3 {
			middle := (shortest + longest) / 2
			starts := []time.Time{
				current.AddDate(0, 0, -(shortest + middle + longest)),
				current.AddDate(0, 0, -(middle + longest)),
				current.AddDate(0, 0, -longest),
				current,
			}
			logs := make([]models.DailyLog, 0, len(starts))
			for _, start := range starts {
				logs = append(logs, models.DailyLog{Date: start, IsPeriod: true, CycleStart: true, Flow: models.FlowMedium})
			}
			today := current.AddDate(0, 0, 2)
			stats := BuildCycleStatsFromLogs(user, logs, today, time.UTC)
			if !dashboardIrregularPredictionRangeEnabled(user, stats) || stats.MinCycleLength != shortest || stats.MaxCycleLength != longest {
				t.Fatalf("%d/%d/%d: range mode %v, min/max %d/%d", shortest, middle, longest, dashboardIrregularPredictionRangeEnabled(user, stats), stats.MinCycleLength, stats.MaxCycleLength)
			}
			earliest := PredictCycleWindow(stats.LastPeriodStart, stats.MinCycleLength, stats.LutealPhase)
			latest := PredictCycleWindow(stats.LastPeriodStart, stats.MaxCycleLength, stats.LutealPhase)
			if !earliest.Calculable || !latest.Calculable {
				continue
			}
			if !betweenInclusive(earliest.OvulationDate, stats.FertilityWindowStart, stats.FertilityWindowEnd) ||
				!betweenInclusive(latest.OvulationDate, stats.FertilityWindowStart, stats.FertilityWindowEnd) {
				t.Fatalf("%d/%d/%d: window %s..%s misses the ovulation range %s..%s", shortest, middle, longest,
					CalendarDayKey(stats.FertilityWindowStart), CalendarDayKey(stats.FertilityWindowEnd),
					CalendarDayKey(earliest.OvulationDate), CalendarDayKey(latest.OvulationDate))
			}
			checked++
		}
	}
	if checked < 100 {
		t.Fatalf("only %d spreads placed both ovulations; the property was barely exercised", checked)
	}
}
