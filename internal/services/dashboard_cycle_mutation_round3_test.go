package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Kills dashboard_cycle.go BOUNDARY mutant on
// dashboardIrregularPredictionRangeEnabled's
// `stats.MaxCycleLength >= stats.MinCycleLength`. With Min==Max the range must
// stay ENABLED; the `>=`->`>` mutant would wrongly suppress it.
func TestMR3Dash_IrregularRangeEnabledWhenMinEqualsMax(t *testing.T) {
	user := &models.User{ID: 72, Role: models.RoleOwner, IrregularCycle: true, CycleLength: 30}
	location := time.UTC
	today := mr3dashDay(t, "2026-06-13")
	stats := CycleStats{
		CompletedCycleCount: 3,
		MedianCycleLength:   30,
		MinCycleLength:      30,
		MaxCycleLength:      30,
		LastPeriodStart:     mr3dashDay(t, "2026-06-01"),
		NextPeriodStart:     mr3dashDay(t, "2026-07-01"),
	}

	cycleContext := BuildDashboardCycleContext(user, stats, today, location)

	if !cycleContext.DisplayNextPeriodUseRange {
		t.Fatalf("expected irregular next-period range enabled when MinCycleLength==MaxCycleLength")
	}
}
