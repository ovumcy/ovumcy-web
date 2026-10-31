package services

import (
	"testing"
	"time"
)

// TestGridChainsFertilityToTheNextCycleOnly pins WEB-388: a regular account with
// three completed cycles that is not overdue gets a fertile window and an
// ovulation day on the running cycle and the one after it (N+1), and none on
// any cycle chained further, while the projected periods still run the whole
// grid horizon (three years).
//
// History: four starts 28 days apart, the running cycle from 2026-03-02; N+1
// opens 2026-03-30 with its ovulation on 2026-04-12, N+2 opens 2026-04-27.
func TestGridChainsFertilityToTheNextCycleOnly(t *testing.T) {
	user := predictableFeedUser(t, "2026-03-02")
	logs := predictableFeedLogs(t)
	now := mustParseDashboardDay(t, "2026-03-20")
	stats := ApplyUserCycleBaseline(user, logs, BuildCycleStatsFromLogs(user, logs, now, time.UTC), now, time.UTC)
	if FertilityProjectionSuppressed(user, stats) || ProjectionChainSuppressed(user, stats) {
		t.Fatal("fixture: the fertility half and the chain must both be on")
	}

	gridEnd := now.AddDate(3, 0, 0)
	maps := buildCalendarPredictionMaps(user, logs, stats, gridEnd, now, time.UTC)

	if !maps.ovulation["2026-04-12"] {
		t.Error("N+1 lost its ovulation day 2026-04-12")
	}
	nextChained := mustParseDashboardDay(t, "2026-04-27")
	for name, marked := range map[string]map[string]bool{
		"ovulation":      maps.ovulation,
		"fertility edge": maps.fertilityEdge,
		"fertility peak": maps.fertilityPeak,
		"pre-fertile":    maps.preFertile,
	} {
		for key := range marked {
			day := mustParseDashboardDay(t, key)
			if !day.Before(nextChained) {
				t.Errorf("%s marker on %s, inside a cycle chained past N+1", name, key)
			}
		}
	}

	farPeriods := 0
	for key := range maps.predictedPeriod {
		if mustParseDashboardDay(t, key).After(now.AddDate(2, 6, 0)) {
			farPeriods++
		}
	}
	if farPeriods == 0 {
		t.Error("the projected periods stop short of the grid horizon")
	}
}

// TestRangeModeGridChainsNoFertility pins the irregular range-mode half of
// WEB-388: the running cycle's range is the only fertility the grid names; the
// cycle after it is a median projection with no range of its own, so no window
// or ovulation day is drawn past the running cycle's next-period window.
func TestRangeModeGridChainsNoFertility(t *testing.T) {
	logs := irregularVerdictLogs(irregularRangeOffsets...)
	now := irregularVerdictBase.AddDate(0, 0, irregularRangeDayOffset)
	today := DateAtLocation(now, time.UTC)
	user := irregularVerdictUser(true)
	stats := BuildCycleStatsFromLogs(user, logs, now, time.UTC)
	if !dashboardIrregularPredictionRangeEnabled(user, stats) || ProjectionChainSuppressed(user, stats) {
		t.Fatal("fixture: range mode on, chain on")
	}
	cycleContext := BuildDashboardCycleContext(user, logs, stats, today, time.UTC)
	if !cycleContext.DisplayNextPeriodUseRange {
		t.Fatal("fixture: the dashboard must show a next-period window")
	}
	windowEnd := cycleContext.DisplayNextPeriodRangeEnd

	maps := buildCalendarPredictionMaps(user, logs, stats, today.AddDate(1, 0, 0), now, time.UTC)
	for _, marked := range []map[string]bool{maps.ovulation, maps.fertilityEdge, maps.fertilityPeak, maps.preFertile} {
		for key := range marked {
			if CalendarDaysBetween(windowEnd, mustParseDashboardDay(t, key)) > 0 {
				t.Errorf("range mode draws a chained fertility marker on %s, past the running cycle's window end %s", key, CalendarDayKey(windowEnd))
			}
		}
	}
	if len(maps.predictedPeriod) == 0 {
		t.Error("range mode lost its projected periods")
	}
}
