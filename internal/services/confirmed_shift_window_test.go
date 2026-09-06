package services

// confirmed_shift_window_test.go — when the owner's own temperatures confirm an
// ovulation in the CURRENT cycle, the confirmed day, the fertile window derived
// from it and the fertility status computed over that window travel together.
//
// Before ResolveConfirmedCycleStats, only the DATE followed the shift: the
// window and CurrentFertility stayed on the projection, so a surface could
// publish "ovulation on the 11th" beside a window ending on the 14th and a
// "fertile" status for a day the shift had already placed behind the owner. A
// confirmed shift asserts one thing and no more: the ovulation has happened —
// which is exactly why the status it produces after the third high day is
// not_fertile.

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// projectedWindowFixture is thermalShiftFixture plus the window the model
// itself would have produced for that cycle (PredictCycleWindow, the same call
// ApplyUserCycleBaseline makes): ovulation 2026-03-14, fertile 2026-03-09..14,
// and today — 2026-03-14 — inside it, so the projected status is "fertile".
// The detector confirms 2026-03-11, whose window is 2026-03-06..11, three days
// short of today.
func projectedWindowFixture(t *testing.T) (*models.User, []models.DailyLog, CycleStats, time.Time) {
	t.Helper()

	user, logs, stats, today := confirmedOvulationFixture(t)
	window := PredictCycleWindow(stats.LastPeriodStart, 28, 14)
	if !window.Calculable {
		t.Fatal("fixture: the model must project a window for a 28/14 cycle")
	}
	stats.FertilityWindowStart = window.FertilityWindowStart
	stats.FertilityWindowEnd = window.FertilityWindowEnd
	stats.OvulationExact = window.OvulationExact
	stats.CurrentFertility = ResolveFertilityStatus(stats, today)
	if stats.CurrentFertility != FertilityStatusFertile {
		t.Fatalf("fixture: the projection must read fertile on today, got %q", stats.CurrentFertility)
	}
	return user, logs, stats, today
}

// impossibleProjectionShiftFixture is confirmedOvulationFixture's cycle with
// its median shortened to 14 — one below minLutealPhaseDays+minOvulationCycleDay
// (15), so CalcOvulationDay refuses it and DashboardProjectionCycleLength (which
// prefers the median) makes the dashboard's own projection report
// OvulationImpossible. AverageCycleLength is raised to 25 so
// DashboardCycleReferenceLength — which prefers the average — still hands the
// hero's OWN, independent ovulation-day placement a cycle long enough to seat
// one: the fixture isolates the impossibility claim the projection makes from
// the unrelated placement guard the hero computes for itself, so a hero that
// stays hidden points at the flag this change fixes, not at that guard.
func impossibleProjectionShiftFixture(t *testing.T) (*models.User, []models.DailyLog, CycleStats, time.Time) {
	t.Helper()

	user, logs, stats, today := confirmedOvulationFixture(t)
	stats.MedianCycleLength = 14
	stats.AverageCycleLength = 25
	stats.OvulationImpossible = true
	return user, logs, stats, today
}

// calendarFertileDays reports the window the grid shades for the fixture's
// month, as "2026-03-06"-style keys, so a window assertion reads the RENDERED
// days rather than the struct the builder was handed.
func calendarFertileDays(t *testing.T, user *models.User, logs []models.DailyLog, stats CycleStats, now time.Time) (fertile map[string]bool, ovulation map[string]bool) {
	t.Helper()

	monthStart := time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	fertile = make(map[string]bool)
	ovulation = make(map[string]bool)
	for _, day := range BuildCalendarDayStates(user, monthStart, logs, stats, now, time.UTC) {
		if day.IsFertilityEdge || day.IsFertilityPeak {
			fertile[day.DateString] = true
		}
		if day.IsOvulation {
			ovulation[day.DateString] = true
		}
	}
	return fertile, ovulation
}

// TestConfirmedShiftDrivesTheWindowAndFertilityStatus is the reproduction: on
// the published overview the whole triple must follow the confirmed day.
func TestConfirmedShiftDrivesTheWindowAndFertilityStatus(t *testing.T) {
	user, logs, stats, today := projectedWindowFixture(t)

	published, suppression, confirmed := PublishedOverviewStats(user, logs, stats, today, time.UTC)

	if suppression.FertilitySuppressed || suppression.PredictionsSuppressed {
		t.Fatalf("fixture: nothing may be suppressed here, got %+v", suppression)
	}
	if !confirmed {
		t.Fatal("the overview must report the shift as confirmed")
	}
	if got := CalendarDayKey(published.OvulationDate); got != "2026-03-11" {
		t.Fatalf("ovulation date = %s, want the confirmed 2026-03-11", got)
	}
	if got := CalendarDayKey(published.FertilityWindowEnd); got != "2026-03-11" {
		t.Fatalf("fertility window end = %s, want the confirmed 2026-03-11", got)
	}
	if got := CalendarDayKey(published.FertilityWindowStart); got != "2026-03-06" {
		t.Fatalf("fertility window start = %s, want 2026-03-06 (confirmed day - 5)", got)
	}
	if published.CurrentFertility != FertilityStatusNotFertile {
		t.Fatalf("current fertility = %q, want %q after the third high day", published.CurrentFertility, FertilityStatusNotFertile)
	}
}

// TestUnconfirmedShiftLeavesTheProjectionUntouched is the negative control: with
// no confirmed shift the resolver is a pass-through, so the assertions above
// cannot be a resolver that rewrites every cycle's window.
func TestUnconfirmedShiftLeavesTheProjectionUntouched(t *testing.T) {
	user, logs, stats, today := projectedWindowFixture(t)
	// Drop the elevated readings: the coverline window stays, the 3-over-6
	// streak does not, so nothing confirms.
	kept := make([]models.DailyLog, 0, len(logs))
	for _, log := range logs {
		if log.BBT != nil && *log.BBT >= thermalShiftHighBBT {
			continue
		}
		kept = append(kept, log)
	}
	if _, ok := ConfirmedCurrentCycleOvulation(user, kept, stats, today, time.UTC); ok {
		t.Fatal("fixture: the trimmed series must confirm nothing")
	}

	resolved, confirmed := ResolveConfirmedCycleStats(user, kept, stats, today, time.UTC)
	if confirmed {
		t.Fatal("the resolver must report no confirmation for an unconfirmed cycle")
	}
	if resolved != stats {
		t.Fatalf("the resolver must return the stats unchanged, got %+v want %+v", resolved, stats)
	}

	published, _, publishedConfirmed := PublishedOverviewStats(user, kept, stats, today, time.UTC)
	if publishedConfirmed {
		t.Fatal("the overview must not report a confirmation")
	}
	if got := CalendarDayKey(published.FertilityWindowEnd); got != "2026-03-14" {
		t.Fatalf("window end = %s, want the projected 2026-03-14", got)
	}
	if published.CurrentFertility != FertilityStatusFertile {
		t.Fatalf("current fertility = %q, want the projection's %q", published.CurrentFertility, FertilityStatusFertile)
	}
}

// TestConfirmedShiftDrivesTheWindowWithoutAProjection is the guard case: an
// account whose median cycle leaves the model no room to place an ovulation
// (clearPredictedCycleWindow — no ovulation date, no next period start,
// OvulationImpossible) still gets the day its own temperatures name, because a
// window derived from a confirmed shift needs no projection to exist. The
// calendar is asserted beside the resolver: that surface refused the signal on
// the same two absent dates.
func TestConfirmedShiftDrivesTheWindowWithoutAProjection(t *testing.T) {
	user, logs, stats, today := projectedWindowFixture(t)
	stats.OvulationDate = time.Time{}
	stats.OvulationExact = false
	stats.OvulationImpossible = true
	stats.NextPeriodStart = time.Time{}
	stats.FertilityWindowStart = time.Time{}
	stats.FertilityWindowEnd = time.Time{}
	stats.CurrentFertility = ResolveFertilityStatus(stats, today)
	if stats.CurrentFertility != FertilityStatusUnknown {
		t.Fatalf("fixture: a withheld projection must classify as %q, got %q", FertilityStatusUnknown, stats.CurrentFertility)
	}

	resolved, confirmed := ResolveConfirmedCycleStats(user, logs, stats, today, time.UTC)
	if !confirmed {
		t.Fatal("a confirmed shift must survive a projection the model could not compute")
	}
	if got := CalendarDayKey(resolved.OvulationDate); got != "2026-03-11" {
		t.Fatalf("ovulation date = %s, want the confirmed 2026-03-11", got)
	}
	if got := CalendarDayKey(resolved.FertilityWindowStart); got != "2026-03-06" {
		t.Fatalf("window start = %s, want 2026-03-06", got)
	}
	if got := CalendarDayKey(resolved.FertilityWindowEnd); got != "2026-03-11" {
		t.Fatalf("window end = %s, want 2026-03-11", got)
	}
	if resolved.OvulationImpossible {
		t.Fatal("a recorded shift answers the projection's impossibility claim; it must not survive it")
	}
	if resolved.CurrentFertility != FertilityStatusNotFertile {
		t.Fatalf("current fertility = %q, want %q", resolved.CurrentFertility, FertilityStatusNotFertile)
	}

	fertile, ovulation := calendarFertileDays(t, user, logs, stats, today)
	if !ovulation["2026-03-11"] {
		t.Fatal("the grid must carry the confirmed ovulation marker when the projection is withheld")
	}
	if !fertile["2026-03-06"] || !fertile["2026-03-11"] {
		t.Fatalf("the grid must shade the confirmed window 2026-03-06..11, got %v", fertile)
	}
}

// TestSuppressedFertilityProjectionStillWithholdsTheConfirmedWindow is the
// medical-safety control: a surface that is silent today stays silent. The
// resolver carries no gate of its own — it rides the one
// ConfirmedCurrentCycleOvulation already reads — and this pins that a suppressed
// tier publishes no window, no date and no fertility status even though the
// temperatures would have confirmed one.
func TestSuppressedFertilityProjectionStillWithholdsTheConfirmedWindow(t *testing.T) {
	for name, suppress := range map[string]func(*models.User, *CycleStats){
		"unpredictable-cycle mode":      func(user *models.User, _ *CycleStats) { user.UnpredictableCycle = true },
		"pregnancy pause":               func(_ *models.User, stats *CycleStats) { stats.PregnancyPaused = true },
		"cycle overdue":                 func(_ *models.User, stats *CycleStats) { stats.CurrentCycleDay = 54 },
		"awaiting the first full cycle": func(_ *models.User, stats *CycleStats) { stats.CompletedCycleCount = 0 },
	} {
		t.Run(name, func(t *testing.T) {
			user, logs, stats, today := projectedWindowFixture(t)
			suppress(user, &stats)
			if !FertilityProjectionSuppressed(user, stats) {
				t.Fatal("fixture: this tier must suppress the fertility projection")
			}

			if _, confirmed := ResolveConfirmedCycleStats(user, logs, stats, today, time.UTC); confirmed {
				t.Fatal("a suppressed tier must confirm nothing")
			}

			published, suppression, confirmed := PublishedOverviewStats(user, logs, stats, today, time.UTC)
			if !suppression.FertilitySuppressed {
				t.Fatal("the published verdict must carry the fertility suppression")
			}
			if confirmed {
				t.Fatal("the overview must not report a confirmation a suppressed tier withholds")
			}
			if !published.OvulationDate.IsZero() || !published.FertilityWindowStart.IsZero() || !published.FertilityWindowEnd.IsZero() {
				t.Fatalf("a suppressed tier must publish no ovulation date and no window, got %+v", published)
			}
			if published.CurrentFertility != FertilityStatusUnknown {
				t.Fatalf("current fertility = %q, want %q", published.CurrentFertility, FertilityStatusUnknown)
			}

			fertile, ovulation := calendarFertileDays(t, user, logs, stats, today)
			if len(ovulation) != 0 || len(fertile) != 0 {
				t.Fatalf("a suppressed tier must leave the grid unshaded, got fertile=%v ovulation=%v", fertile, ovulation)
			}
		})
	}
}

// TestStatsPagePublishesTheConfirmedWindow is the /stats reader's verdict. That
// page published the projection's window untouched while its own BBT chart
// already drew the coverline and the marker for the very shift the window
// ignored. The logs here are real history — the stats the page publishes are
// derived from them, not handed in — so the projection it starts from is the
// production one.
func TestStatsPagePublishesTheConfirmedWindow(t *testing.T) {
	logs := []models.DailyLog{
		{Date: mustParseStatsServiceDay(t, "2026-02-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseStatsServiceDay(t, "2026-02-02"), IsPeriod: true, Flow: models.FlowMedium},
		{Date: mustParseStatsServiceDay(t, "2026-03-01"), IsPeriod: true, CycleStart: true, Flow: models.FlowMedium},
		{Date: mustParseStatsServiceDay(t, "2026-03-02"), IsPeriod: true, Flow: models.FlowMedium},
	}
	firstLowDay := mustParseStatsServiceDay(t, "2026-03-06")
	for offset := range 6 {
		logs = append(logs, models.DailyLog{Date: AddCalendarDays(firstLowDay, offset, time.UTC), BBT: new(thermalShiftLowBBT)})
	}
	for offset := range 3 {
		logs = append(logs, models.DailyLog{Date: AddCalendarDays(firstLowDay, 6+offset, time.UTC), BBT: new(thermalShiftHighBBT)})
	}

	user := &models.User{ID: 21, Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14, TrackBBT: true}
	now := mustParseStatsServiceDay(t, "2026-03-14")

	// The projection this page starts from, so the assertions below name a
	// window that genuinely moved rather than one that was already right.
	projected := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if got := CalendarDayKey(projected.FertilityWindowEnd); got != "2026-03-14" {
		t.Fatalf("fixture: the projected window must end on 2026-03-14, got %s", got)
	}
	confirmedDay, ok := ConfirmedCurrentCycleOvulation(user, logs, projected, now, time.UTC)
	if !ok || CalendarDayKey(confirmedDay) != "2026-03-11" {
		t.Fatalf("fixture: the detector must confirm 2026-03-11, got %s (ok=%v)", CalendarDayKey(confirmedDay), ok)
	}

	service := NewStatsService(&stubStatsDayReader{logsForRange: logs, logsForAll: logs}, &stubStatsSymptomReader{})
	viewData, err := service.BuildStatsPageViewData(context.Background(), user, "en", "Cycle %d", now, time.UTC, 12)
	if err != nil {
		t.Fatalf("BuildStatsPageViewData() unexpected error: %v", err)
	}

	if got := CalendarDayKey(viewData.Stats.OvulationDate); got != "2026-03-11" {
		t.Fatalf("published ovulation date = %s, want the confirmed 2026-03-11", got)
	}
	if got := CalendarDayKey(viewData.Stats.FertilityWindowEnd); got != "2026-03-11" {
		t.Fatalf("published window end = %s, want the confirmed 2026-03-11", got)
	}
	if got := CalendarDayKey(viewData.Stats.FertilityWindowStart); got != "2026-03-06" {
		t.Fatalf("published window start = %s, want 2026-03-06", got)
	}
	if viewData.Stats.CurrentFertility != FertilityStatusNotFertile {
		t.Fatalf("published fertility = %q, want %q", viewData.Stats.CurrentFertility, FertilityStatusNotFertile)
	}
}

// TestDashboardPublishesAndShadesTheConfirmedWindow is the dashboard's verdict,
// for both readers it feeds: the PUBLISHED CycleStats every partial below the
// page reads, and the hero ring, which shaded its fertile arc straight off the
// projection while the ovulation line above it already named the confirmed day.
func TestDashboardPublishesAndShadesTheConfirmedWindow(t *testing.T) {
	user, logs, stats, today := projectedWindowFixture(t)

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{stats: stats},
		&stubDashboardViewerProvider{logEntry: models.DailyLog{Date: today}},
		&stubDashboardDayStateProvider{logs: logs},
	)
	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}

	if got := CalendarDayKey(viewData.Stats.FertilityWindowEnd); got != "2026-03-11" {
		t.Fatalf("published window end = %s, want the confirmed 2026-03-11", got)
	}
	if got := CalendarDayKey(viewData.Stats.FertilityWindowStart); got != "2026-03-06" {
		t.Fatalf("published window start = %s, want 2026-03-06", got)
	}
	if viewData.Stats.CurrentFertility != FertilityStatusNotFertile {
		t.Fatalf("published fertility = %q, want %q", viewData.Stats.CurrentFertility, FertilityStatusNotFertile)
	}

	if !viewData.CycleHero.Visible {
		t.Fatal("fixture: the hero ring must render, or its assertions below are vacuous")
	}
	// Cycle day 1 is 2026-03-01, so the confirmed window covers days 6..11 and
	// the superseded projection covered days 9..14.
	fertileDays := make(map[int]bool)
	peakDays := make(map[int]bool)
	for _, day := range viewData.CycleHero.Days {
		if day.IsFertile {
			fertileDays[day.Day] = true
		}
		if day.IsFertilePeak {
			peakDays[day.Day] = true
		}
	}
	for day := 6; day <= 11; day++ {
		if !fertileDays[day] {
			t.Fatalf("the hero must mark cycle day %d fertile, got %v", day, fertileDays)
		}
	}
	for day := 12; day <= 14; day++ {
		if fertileDays[day] {
			t.Fatalf("the hero must drop the superseded cycle day %d, got %v", day, fertileDays)
		}
	}
	if !peakDays[11] || peakDays[14] {
		t.Fatalf("the hero peak must sit on the confirmed cycle day 11, got %v", peakDays)
	}
}

// TestDashboardShowsTheHeroWhenAConfirmedShiftAnswersTheImpossibilityClaim
// reproduces the dashboard's own N-of-N+1: ResolveConfirmedCycleStats already
// clears OvulationImpossible once a shift is confirmed, but the dashboard's
// display builder derives its own copy of the flag from the raw projection and
// never reads the substitution, so a median cycle too short to place an
// ovulation kept the hero hidden and the reminder banner silent even though a
// confirmed shift had just named the day and the window.
func TestDashboardShowsTheHeroWhenAConfirmedShiftAnswersTheImpossibilityClaim(t *testing.T) {
	user, logs, stats, today := impossibleProjectionShiftFixture(t)

	service := NewDashboardViewService(
		&stubDashboardStatsProvider{stats: stats},
		&stubDashboardViewerProvider{logEntry: models.DailyLog{Date: today}},
		&stubDashboardDayStateProvider{logs: logs},
	)
	viewData, err := service.BuildDashboardViewData(context.Background(), user, "en", today, time.UTC)
	if err != nil {
		t.Fatalf("BuildDashboardViewData() unexpected error: %v", err)
	}

	if viewData.CycleContext.DisplayOvulationImpossible {
		t.Fatal("a confirmed shift answers the projection's impossibility claim; the display must not keep it")
	}
	if !viewData.CycleHero.Visible {
		t.Fatal("the hero must render once the confirmed shift answers the impossibility claim")
	}
}

// TestCalendarShadesTheConfirmedWindowForTheCurrentCycle is the calendar
// reader's verdict: the grid shades [confirmed-5, confirmed] and drops the
// projected days the shift superseded — the solid marker had already moved onto
// the detector's day while the shading under it had not.
func TestCalendarShadesTheConfirmedWindowForTheCurrentCycle(t *testing.T) {
	user, logs, stats, today := projectedWindowFixture(t)

	fertile, ovulation := calendarFertileDays(t, user, logs, stats, today)

	if !ovulation["2026-03-11"] || ovulation["2026-03-14"] {
		t.Fatalf("the solid marker must sit on the confirmed 2026-03-11 only, got %v", ovulation)
	}
	for _, day := range []string{"2026-03-06", "2026-03-07", "2026-03-08", "2026-03-09", "2026-03-10", "2026-03-11"} {
		if !fertile[day] {
			t.Fatalf("the confirmed window day %s must be shaded, got %v", day, fertile)
		}
	}
	for _, day := range []string{"2026-03-12", "2026-03-13", "2026-03-14"} {
		if fertile[day] {
			t.Fatalf("the superseded projected day %s must lose its shading, got %v", day, fertile)
		}
	}
}

// TestDashboardHeroOvulationCardSitsOnTheConfirmedPeak is the hero's own N-of-N+1:
// dashboardCycleHeroFertileSpan already rides the confirmed peak (cycle day 11),
// but ovulationDay — the phase-card geometry the "ovulation" card and
// dashboardCycleHeroCurrentPhase's fallback both use — was still
// CalcOvulationDay's PROJECTED cycle day (14, from a 28/14 cycle), so the
// ribbon's peak cell and the card labeled "ovulation" named two different days.
func TestDashboardHeroOvulationCardSitsOnTheConfirmedPeak(t *testing.T) {
	user, logs, stats, today := projectedWindowFixture(t)

	confirmed, ok := ResolveConfirmedCycleStats(user, logs, stats, today, time.UTC)
	if !ok {
		t.Fatal("fixture: the shift must confirm")
	}

	hero := BuildDashboardCycleHero(user, confirmed, DashboardCycleContext{}, dashboardCycleHeroInput{Logs: logs, Today: today, Location: time.UTC})
	if !hero.Visible {
		t.Fatal("fixture: the hero must render")
	}

	var peakDay int
	for _, day := range hero.Days {
		if day.IsFertilePeak {
			peakDay = day.Day
		}
	}
	if peakDay != 11 {
		t.Fatalf("fixture: the ribbon peak must sit on the confirmed cycle day 11, got %d", peakDay)
	}

	var ovulationCard DashboardCycleHeroPhaseCard
	for _, card := range hero.PhaseCards {
		if card.Phase == "ovulation" {
			ovulationCard = card
		}
	}
	if ovulationCard.StartDay != peakDay || ovulationCard.EndDay != peakDay {
		t.Fatalf(`the "ovulation" card must sit on the ribbon peak (cycle day %d), got %d-%d`, peakDay, ovulationCard.StartDay, ovulationCard.EndDay)
	}
}

// TestResolveConfirmedCycleStatsRecomputesCurrentPhase is the phase-axis half of
// F2: CurrentPhase was computed against the PROJECTED ovulation date before the
// resolver ran and never recomputed afterward, so a published response could
// name "ovulation" as the current phase beside a confirmed ovulation_date and a
// current_fertility of "not_fertile" for the very same day — three claims about
// one day that contradicted each other. Phase and fertility are two orthogonal
// axes (#416), but both are geometric and both must describe the date actually
// published beside them.
func TestResolveConfirmedCycleStatsRecomputesCurrentPhase(t *testing.T) {
	user, logs, stats, today := projectedWindowFixture(t)
	// Simulate the pre-resolver pipeline: CurrentPhase computed from the
	// still-projected OvulationDate (2026-03-14), which today sits exactly on.
	stats.CurrentPhase = detectCyclePhase(stats, logs, today)
	if stats.CurrentPhase != "ovulation" {
		t.Fatalf("fixture: the projection must classify today as %q, got %q", "ovulation", stats.CurrentPhase)
	}

	resolved, ok := ResolveConfirmedCycleStats(user, logs, stats, today, time.UTC)
	if !ok {
		t.Fatal("fixture: the shift must confirm")
	}
	if resolved.CurrentPhase != "luteal" {
		t.Fatalf("current phase = %q, want %q: the confirmed ovulation (2026-03-11) is 3 days behind today", resolved.CurrentPhase, "luteal")
	}
}
