package api

import (
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

func TestOnboardingFlowBuildsPeriodRangeFromSliderPeriodLength(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow-finished@example.com", "StrongPass1", false)

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	startDay := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -14)
	periodLength := 8
	endDayExclusive := startDay.AddDate(0, 0, periodLength)

	submitOnboardingStep1(t, app, authCookie, user.ID, url.Values{
		"last_period_start": {startDay.Format("2006-01-02")},
	})

	submitOnboardingStep2(t, app, authCookie, user.ID, url.Values{
		"cycle_length":     {"28"},
		"period_length":    {"8"},
		"auto_period_fill": {"true"},
	})

	submitOnboardingComplete(t, app, authCookie, user.ID)

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

	submitOnboardingStep1(t, app, authCookie, user.ID, url.Values{
		"last_period_start": {startDay.Format("2006-01-02")},
	})

	submitOnboardingStep2(t, app, authCookie, user.ID, url.Values{
		"cycle_length":  {"28"},
		"period_length": {"5"},
	})

	submitOnboardingComplete(t, app, authCookie, user.ID)

	// With auto-fill off only the start day the owner entered is logged (as a
	// cycle start); no other day of the period is seeded.
	var startRows []models.DailyLog
	if err := database.Where("user_id = ? AND date >= ? AND date < ?", user.ID, startDay, startDay.AddDate(0, 0, 1)).Find(&startRows).Error; err != nil {
		t.Fatalf("load the start day: %v", err)
	}
	if len(startRows) != 1 || !startRows[0].IsPeriod || !startRows[0].CycleStart {
		t.Fatalf("the start day must be one period day carrying the cycle-start mark, got %+v", startRows)
	}
	for day := startDay.AddDate(0, 0, 1); day.Before(endDayExclusive); day = day.AddDate(0, 0, 1) {
		assertNoOnboardingLogForDay(t, database, user.ID, day)
	}
}

func postOnboardingJSONForTest(t *testing.T, app *fiber.App, authCookie string, userID uint, path string, body string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Cookie", authCookie)
	sendRequestTimezoneForTest(request, testRequestTimezone)
	bindDayWriteForTest(t, request, userID)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("POST %s failed: %v", path, err)
	}
	return response
}

func reloadOnboardingUserForTest(t *testing.T, database *gorm.DB, userID uint) models.User {
	t.Helper()
	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return user
}

func countOnboardingLogsForTest(t *testing.T, database *gorm.DB, userID uint) int64 {
	t.Helper()
	var count int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		t.Fatalf("count daily logs: %v", err)
	}
	return count
}

// TestOnboardingSkippingTheLastPeriodDateCompletesWithoutAStart pins the whole
// skip path on the HTMX transport: step 1 skipped, step 2 carrying the skip
// completes, the account holds no start and no day log, and the dashboard shows
// the same start-date prompt (linking to the settings card) a clear-data wipe
// leaves behind.
func TestOnboardingSkippingTheLastPeriodDateCompletesWithoutAStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow-skip-start@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	submitOnboardingStep1(t, app, authCookie, user.ID, url.Values{"skip_last_period": {"1"}})
	submitOnboardingStep2(t, app, authCookie, user.ID, url.Values{
		"cycle_length":     {"28"},
		"period_length":    {"5"},
		"auto_period_fill": {"true"},
		"skip_last_period": {"1"},
	})

	reloaded := reloadOnboardingUserForTest(t, database, user.ID)
	if !reloaded.OnboardingCompleted {
		t.Fatal("expected onboarding to be completed after the skip")
	}
	if reloaded.LastPeriodStart != nil {
		t.Fatalf("expected no stored last period start, got %v", reloaded.LastPeriodStart)
	}
	if count := countOnboardingLogsForTest(t, database, user.ID); count != 0 {
		t.Fatalf("expected no day log from a skipped start, got %d", count)
	}

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusOK)
	if !strings.Contains(mustReadBodyString(t, response.Body), `href="/settings#settings-cycle"`) {
		t.Fatal("expected the dashboard to link the start-date prompt to the settings card")
	}
}

// TestOnboardingStep2WithoutTheSkipStillRefusesAMissingStart: a start-less
// account that never answered step 1 with the skip is not completed by step 2.
func TestOnboardingStep2WithoutTheSkipStillRefusesAMissingStart(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow-no-skip@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	submitOnboardingStep2(t, app, authCookie, user.ID, url.Values{"cycle_length": {"28"}, "period_length": {"5"}})

	if reloadOnboardingUserForTest(t, database, user.ID).OnboardingCompleted {
		t.Fatal("step 2 without a start and without the skip must not complete onboarding")
	}
}

// TestOnboardingStep1SkipOnJSONAndCompletion pins the JSON transport: an empty
// step 1 without the skip is refused, the skip is accepted and withdraws a date
// stored by an earlier submit, and /complete succeeds without a start only when
// it names the skip.
func TestOnboardingStep1SkipOnJSONAndCompletion(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow-skip-json@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	day := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -3).Format("2006-01-02")

	post := func(path string, body string, want int) {
		t.Helper()
		response := postOnboardingJSONForTest(t, app, authCookie, user.ID, path, body)
		defer func() { _ = response.Body.Close() }()
		assertStatusCode(t, response, want)
	}

	post("/api/v1/onboarding/steps/1", `{"last_period_start":""}`, http.StatusBadRequest)
	post("/api/v1/onboarding/steps/1", `{"last_period_start":"`+day+`"}`, http.StatusOK)
	if reloadOnboardingUserForTest(t, database, user.ID).LastPeriodStart == nil {
		t.Fatal("fixture: the dated step 1 must store a start")
	}

	post("/api/v1/onboarding/steps/1", `{"skip_last_period":true}`, http.StatusOK)
	if reloadOnboardingUserForTest(t, database, user.ID).LastPeriodStart != nil {
		t.Fatal("expected the skip to withdraw the start an earlier submit stored")
	}

	post("/api/v1/onboarding/complete", `{}`, http.StatusBadRequest)
	if reloadOnboardingUserForTest(t, database, user.ID).OnboardingCompleted {
		t.Fatal("completion without a start and without the skip must be refused")
	}
	post("/api/v1/onboarding/complete", `{"skip_last_period":true}`, http.StatusOK)

	reloaded := reloadOnboardingUserForTest(t, database, user.ID)
	if !reloaded.OnboardingCompleted || reloaded.LastPeriodStart != nil {
		t.Fatalf("expected a completed account with no start, got completed=%v start=%v", reloaded.OnboardingCompleted, reloaded.LastPeriodStart)
	}
	if count := countOnboardingLogsForTest(t, database, user.ID); count != 0 {
		t.Fatalf("expected no day log, got %d", count)
	}
}

// TestOnboardingStep1SkipFormFlowCarriesTheSkipToStepTwo pins the no-JS arm:
// the skip redirects to step 2 naming it, that page renders the step-2 skip
// field enabled, and a plain step-2 page renders it disabled (unsent).
func TestOnboardingStep1SkipFormFlowCarriesTheSkipToStepTwo(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "flow-skip-nojs@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{"skip_last_period": {"1"}, "last_period_start": {""}}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)
	sendRequestTimezoneForTest(request, testRequestTimezone)
	bindDayWriteForTest(t, request, user.ID)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("step1 request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusSeeOther)
	location := response.Header.Get("Location")
	if location != "/onboarding?step=2&skip_last_period=1" {
		t.Fatalf("expected the skip to land on step 2 naming it, got %q", location)
	}

	skipField := func(path string) string {
		page := httptest.NewRequest(http.MethodGet, path, nil)
		page.Header.Set("Cookie", authCookie)
		pageResponse := mustAppResponse(t, app, page)
		defer func() { _ = pageResponse.Body.Close() }()
		assertStatusCode(t, pageResponse, http.StatusOK)
		body := mustReadBodyString(t, pageResponse.Body)
		marker := strings.Index(body, "data-onboarding-skip-field")
		if marker < 0 {
			t.Fatal("expected the step-2 form to carry the skip field")
		}
		tagStart := strings.LastIndex(body[:marker], "<input")
		tagEnd := marker + strings.Index(body[marker:], ">")
		return body[tagStart : tagEnd+1]
	}
	if tag := skipField(location); strings.Contains(tag, "disabled") {
		t.Fatalf("expected the skip field enabled after a skip, got %s", tag)
	}
	if tag := skipField("/onboarding?step=2"); !strings.Contains(tag, "disabled") {
		t.Fatalf("expected the skip field disabled without a skip, got %s", tag)
	}
}

// TestOnboardingSkipTouchesOnlyTheCallersAccount: another owner's start and
// onboarding state survive an owner's skip and completion.
func TestOnboardingSkipTouchesOnlyTheCallersAccount(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	owner := createOnboardingTestUser(t, database, "flow-skip-owner@example.com", "StrongPass1", false)
	other := createOnboardingTestUser(t, database, "flow-skip-other@example.com", "StrongPass1", false)
	otherStart := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -5)
	if err := database.Model(&models.User{}).Where("id = ?", other.ID).Update("last_period_start", otherStart).Error; err != nil {
		t.Fatalf("seed the other owner's start: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, owner.Email, "StrongPass1")

	submitOnboardingStep1(t, app, authCookie, owner.ID, url.Values{"skip_last_period": {"1"}})
	submitOnboardingStep2(t, app, authCookie, owner.ID, url.Values{"cycle_length": {"28"}, "period_length": {"5"}, "skip_last_period": {"1"}})

	untouched := reloadOnboardingUserForTest(t, database, other.ID)
	if untouched.OnboardingCompleted || untouched.LastPeriodStart == nil {
		t.Fatalf("another owner's onboarding changed: completed=%v start=%v", untouched.OnboardingCompleted, untouched.LastPeriodStart)
	}
}
