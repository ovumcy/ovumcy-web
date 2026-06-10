package services

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestBuildDashboardCycleContextFlagsNextPeriodNeedsDataForSparseIrregularUser(t *testing.T) {
	user := &models.User{IrregularCycle: true}
	stats := CycleStats{
		LastPeriodStart:     mustParseDashboardDay(t, "2026-03-01"),
		AverageCycleLength:  32,
		CompletedCycleCount: 2,
		NextPeriodStart:     mustParseDashboardDay(t, "2026-04-02"),
		OvulationDate:       mustParseDashboardDay(t, "2026-03-18"),
	}
	today := mustParseDashboardDay(t, "2026-03-10")

	context := BuildDashboardCycleContext(user, stats, today, time.UTC)
	if !context.DisplayNextPeriodNeedsData {
		t.Fatalf("expected sparse irregular mode (IrregularCycle, <3 completed cycles, non-zero projected next period) to flag DisplayNextPeriodNeedsData=true")
	}
}
