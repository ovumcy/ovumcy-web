package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestNoRolledWindowCoversTodayInAnyMode pins why the "fertile" reading of
// reconcileMovedWindow is a backstop since WEB-378 (WEB-386). A rolled window
// covers today only once the running cycle is past its projection length,
// where every regular account has the fertility half withheld before the roll;
// irregular range mode keeps that half but never rolls (its ovulation range is
// placed from the recorded start). So on every cycle day up to the overdue gate
// a reconciled overview names no "fertile" status, and range mode reconciles
// nothing at all.
func TestNoRolledWindowCoversTodayInAnyMode(t *testing.T) {
	regular := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14}
	irregular := &models.User{Role: models.RoleOwner, CycleLength: 28, PeriodLength: 5, LutealPhase: 14, IrregularCycle: true}
	for _, history := range []struct {
		name      string
		user      *models.User
		starts    []string
		rangeMode bool
	}{
		{name: "28-day history", user: regular, starts: []string{"2026-06-15", "2026-07-13", "2026-08-10", "2026-09-07"}},
		{name: "27-28-28-36", user: regular, starts: []string{"2026-06-03", "2026-06-30", "2026-07-28", "2026-08-25", "2026-09-30"}},
		{name: "24-24-24-40-40", user: regular, starts: []string{"2026-01-01", "2026-01-25", "2026-02-18", "2026-03-14", "2026-04-23", "2026-06-02"}},
		{name: "range mode 24-28-28-34-36", user: irregular, rangeMode: true, starts: []string{"2026-05-03", "2026-05-27", "2026-06-24", "2026-07-22", "2026-08-25", "2026-09-30"}},
		{name: "range mode 24-24-24-40-40", user: irregular, rangeMode: true, starts: []string{"2026-01-01", "2026-01-25", "2026-02-18", "2026-03-14", "2026-04-23", "2026-06-02"}},
	} {
		t.Run(history.name, func(t *testing.T) {
			forEachParityZone(t, func(t *testing.T, location *time.Location) {
				logs := cycleStartLogs(t, history.starts...)
				runningStart := mustParseDay(t, history.starts[len(history.starts)-1])

				reconciled, exercised := 0, 0
				for cycleDay := 1; cycleDay <= 60; cycleDay++ {
					now := localNoon(AddCalendarDays(runningStart, cycleDay-1, time.UTC), location)
					today := DateAtLocation(now, location)
					stats := ApplyUserCycleBaseline(history.user, logs, BuildCycleStatsFromLogs(history.user, logs, now, location), today, location)
					if dashboardIrregularPredictionRangeEnabled(history.user, stats) != history.rangeMode {
						t.Fatalf("cycle day %d: fixture: range mode %v, want %v", cycleDay, !history.rangeMode, history.rangeMode)
					}
					overview := publishedOverviewStats(history.user, logs, stats, today, location)
					if overview.suppression.PredictionsSuppressed {
						continue
					}
					exercised++
					if !overview.reconciled {
						continue
					}
					reconciled++
					if overview.published.CurrentFertility == FertilityStatusFertile {
						t.Errorf("cycle day %d: the rolled window %s..%s covers today",
							cycleDay, CalendarDayKey(overview.published.FertilityWindowStart), CalendarDayKey(overview.published.FertilityWindowEnd))
					}
				}
				if exercised == 0 {
					t.Fatal("no cycle day of the fixture published a projection")
				}
				if history.rangeMode && reconciled != 0 {
					t.Errorf("range mode reconciled %d day(s) against a rolled window", reconciled)
				}
				// Anti-vacuity: a regular history does roll on the days before its
				// median start, so the loop above reads real reconciles.
				if !history.rangeMode && reconciled == 0 {
					t.Error("no cycle day of the fixture reconciled a rolled window")
				}
			})
		})
	}
}

// TestReconcileMovedWindowReadsAWindowCoveringTodayAsFertile pins the backstop
// TestNoRolledWindowCoversTodayInAnyMode shows no caller reaches: handed a
// rolled window that covers today, the status reads "fertile" against it and
// the running cycle's "luteal" gives way to "unknown", so one response never
// calls a day fertile and luteal at once.
func TestReconcileMovedWindowReadsAWindowCoveringTodayAsFertile(t *testing.T) {
	today := mustParseDay(t, "2026-10-15")
	published := CycleStats{
		OvulationDate:        mustParseDay(t, "2026-10-18"),
		FertilityWindowStart: mustParseDay(t, "2026-10-13"),
		FertilityWindowEnd:   mustParseDay(t, "2026-10-18"),
		CurrentPhase:         "luteal",
	}
	reconcileMovedWindow(&published, nil, today, time.UTC)
	assertPublishedStatusAndPhase(t, published, FertilityStatusFertile, FertilityBasisProjection, "unknown")
}
