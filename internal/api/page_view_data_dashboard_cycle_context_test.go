package api

import (
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestBuildDashboardViewDataKeepsPredictionsFutureAndFlagsStaleCycleData(t *testing.T) {
	t.Parallel()

	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "dashboard-cycle-context@example.com")

	now := time.Date(2026, time.February, 22, 0, 0, 0, 0, time.UTC)
	lastPeriodStart := now.AddDate(0, 0, -60)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      28,
		"period_length":     5,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}

	user.CycleLength = 28
	user.PeriodLength = 5
	user.LastPeriodStart = &lastPeriodStart

	data, errorMessage, err := handler.buildDashboardViewData(&user, "en", map[string]string{
		"meta.title.dashboard": "Dashboard",
	}, now)
	if err != nil {
		t.Fatalf("buildDashboardViewData returned error: %v", err)
	}
	if errorMessage != "" {
		t.Fatalf("expected empty error message, got %q", errorMessage)
	}

	if reference, ok := data["CycleDayReference"].(int); !ok || reference != 28 {
		t.Fatalf("expected cycle day reference 28, got %#v", data["CycleDayReference"])
	}
	if stale, ok := data["CycleDataStale"].(bool); !ok || !stale {
		t.Fatalf("expected stale cycle data warning=true, got %#v", data["CycleDataStale"])
	}
	if past, ok := data["NextPeriodInPast"].(bool); !ok || past {
		t.Fatalf("expected next period to be projected into future, got %#v", data["NextPeriodInPast"])
	}
	if past, ok := data["OvulationInPast"].(bool); !ok || past {
		t.Fatalf("expected ovulation to be projected into future, got %#v", data["OvulationInPast"])
	}

	stats, ok := data["Stats"].(services.CycleStats)
	if !ok {
		t.Fatalf("expected cycle stats in view data, got %#v", data["Stats"])
	}
	if stats.CurrentCycleDay <= 0 || stats.CurrentCycleDay > 28 {
		t.Fatalf("expected cycle day within 1..28, got %d", stats.CurrentCycleDay)
	}

	displayNextPeriodStart, ok := data["DisplayNextPeriodStart"].(time.Time)
	if !ok {
		t.Fatalf("expected display next period in view data, got %#v", data["DisplayNextPeriodStart"])
	}
	if !displayNextPeriodStart.After(now) {
		t.Fatalf("expected display next period after now, got %s", displayNextPeriodStart.Format("2006-01-02"))
	}

	displayOvulationDate, ok := data["DisplayOvulationDate"].(time.Time)
	if !ok {
		t.Fatalf("expected display ovulation in view data, got %#v", data["DisplayOvulationDate"])
	}
	if !displayOvulationDate.IsZero() && displayOvulationDate.Before(now) {
		t.Fatalf("expected display ovulation date not in past, got %s", displayOvulationDate.Format("2006-01-02"))
	}
}

func TestBuildDashboardViewDataMarksOvulationAsUncalculableForIncompatibleCycle(t *testing.T) {
	t.Parallel()

	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "dashboard-ovulation-impossible@example.com")

	lastPeriodStart := time.Date(2026, time.February, 10, 0, 0, 0, 0, time.UTC)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":      15,
		"period_length":     10,
		"last_period_start": lastPeriodStart,
	}).Error; err != nil {
		t.Fatalf("update user cycle context: %v", err)
	}
	if err := database.Create(&models.DailyLog{
		UserID:   user.ID,
		Date:     lastPeriodStart,
		IsPeriod: true,
		Flow:     models.FlowMedium,
	}).Error; err != nil {
		t.Fatalf("create period log: %v", err)
	}

	user.CycleLength = 15
	user.PeriodLength = 10
	user.LastPeriodStart = &lastPeriodStart

	now := time.Date(2026, time.February, 22, 0, 0, 0, 0, time.UTC)
	data, errorMessage, err := handler.buildDashboardViewData(&user, "en", map[string]string{
		"meta.title.dashboard": "Dashboard",
	}, now)
	if err != nil {
		t.Fatalf("buildDashboardViewData returned error: %v", err)
	}
	if errorMessage != "" {
		t.Fatalf("expected empty error message, got %q", errorMessage)
	}

	stats, ok := data["Stats"].(services.CycleStats)
	if !ok {
		t.Fatalf("expected cycle stats in view data, got %#v", data["Stats"])
	}
	if !stats.OvulationImpossible {
		t.Fatalf("expected ovulation impossible flag=true for incompatible cycle values")
	}
	if !stats.OvulationDate.IsZero() {
		t.Fatalf("expected no ovulation date for incompatible cycle values")
	}
	displayImpossible, ok := data["DisplayOvulationImpossible"].(bool)
	if !ok || !displayImpossible {
		t.Fatalf("expected display ovulation impossible=true, got %#v", data["DisplayOvulationImpossible"])
	}
}
