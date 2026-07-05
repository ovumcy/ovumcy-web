package api

import (
	"net/url"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestOnboardingFlowCompletesWithOngoingPeriodRangeAndFlowNone(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow@example.com", "StrongPass1", false)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	stepDate := today.AddDate(0, 0, -2)
	stepDateRaw := stepDate.Format("2006-01-02")

	submitOnboardingStep1(t, app, authCookie, url.Values{
		"last_period_start": {stepDateRaw},
	})

	submitOnboardingStep2(t, app, authCookie, url.Values{
		"cycle_length":     {"30"},
		"period_length":    {"5"},
		"auto_period_fill": {"true"},
		"irregular_cycle":  {"true"},
	})

	submitOnboardingComplete(t, app, authCookie)

	var updatedUser models.User
	if err := database.First(&updatedUser, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if !updatedUser.OnboardingCompleted {
		t.Fatalf("expected onboarding to be completed")
	}
	if updatedUser.CycleLength != 30 {
		t.Fatalf("expected cycle length 30, got %d", updatedUser.CycleLength)
	}
	if updatedUser.PeriodLength != 5 {
		t.Fatalf("expected period length 5, got %d", updatedUser.PeriodLength)
	}
	if !updatedUser.IrregularCycle {
		t.Fatalf("expected irregular_cycle to be persisted from onboarding")
	}
	if updatedUser.LastPeriodStart == nil {
		t.Fatalf("expected last period start to be saved")
	}
	if updatedUser.LastPeriodStart.Format("2006-01-02") != stepDateRaw {
		t.Fatalf("expected last period start %s, got %s", stepDateRaw, updatedUser.LastPeriodStart.Format("2006-01-02"))
	}
	for offset := range 5 {
		day := stepDate.AddDate(0, 0, offset)
		assertOnboardingPeriodLogForDay(t, database, updatedUser.ID, day)
	}

	assertOnboardingPeriodLogForDay(t, database, updatedUser.ID, today)
}
