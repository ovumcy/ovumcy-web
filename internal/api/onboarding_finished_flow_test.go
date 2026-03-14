package api

import (
	"github.com/terraincognita07/ovumcy/internal/services"
	"net/url"
	"testing"
	"time"
)

func TestOnboardingFlowBuildsPeriodRangeFromSliderPeriodLength(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow-finished@example.com", "StrongPass1", false)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	startDay := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -14)
	periodLength := 8
	endDayExclusive := startDay.AddDate(0, 0, periodLength)

	submitOnboardingStep1(t, app, authCookie, url.Values{
		"last_period_start": {startDay.Format("2006-01-02")},
	})

	submitOnboardingStep2(t, app, authCookie, url.Values{
		"cycle_length":     {"28"},
		"period_length":    {"8"},
		"auto_period_fill": {"true"},
	})

	submitOnboardingComplete(t, app, authCookie)

	for day := startDay; day.Before(endDayExclusive); day = day.AddDate(0, 0, 1) {
		assertOnboardingPeriodLogForDay(t, database, user.ID, day)
	}

	assertNoOnboardingLogForDay(t, database, user.ID, endDayExclusive)
}

func TestOnboardingFlowSkipsPeriodLogsWhenAutoFillIsDisabled(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow-no-autofill@example.com", "StrongPass1", false)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	startDay := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -10)
	periodLength := 5
	endDayExclusive := startDay.AddDate(0, 0, periodLength)

	submitOnboardingStep1(t, app, authCookie, url.Values{
		"last_period_start": {startDay.Format("2006-01-02")},
	})

	submitOnboardingStep2(t, app, authCookie, url.Values{
		"cycle_length":  {"28"},
		"period_length": {"5"},
	})

	submitOnboardingComplete(t, app, authCookie)

	for day := startDay; day.Before(endDayExclusive); day = day.AddDate(0, 0, 1) {
		assertNoOnboardingLogForDay(t, database, user.ID, day)
	}
}
