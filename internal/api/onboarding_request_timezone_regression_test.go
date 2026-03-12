package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func TestOnboardingPersistsLastPeriodStartUsingRequestTimezone(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "onboarding-tz@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location).Format("2006-01-02")

	submitOnboardingStep1WithTimezone(t, app, authCookie, timezoneName, url.Values{
		"last_period_start": {localToday},
	})
	submitOnboardingStep2WithTimezone(t, app, authCookie, timezoneName, url.Values{
		"cycle_length":     {"28"},
		"period_length":    {"5"},
		"auto_period_fill": {"true"},
	})
	submitOnboardingCompleteWithTimezone(t, app, authCookie, timezoneName)

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if persisted.LastPeriodStart == nil {
		t.Fatal("expected persisted last_period_start after onboarding")
	}

	savedLocalDay := services.DateAtLocation(persisted.LastPeriodStart.In(location), location).Format("2006-01-02")
	if savedLocalDay != localToday {
		t.Fatalf("expected persisted onboarding last_period_start %q, got %q", localToday, savedLocalDay)
	}

	settingsRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	settingsRequest.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	settingsRequest.Header.Set(timezoneHeaderName, timezoneName)

	settingsResponse := mustAppResponse(t, app, settingsRequest)
	assertStatusCode(t, settingsResponse, http.StatusOK)

	body := mustReadBodyString(t, settingsResponse.Body)
	if !strings.Contains(body, `name="last_period_start"`) {
		t.Fatalf("expected settings cycle input in response")
	}
	if !strings.Contains(body, `value="`+localToday+`"`) {
		t.Fatalf("expected settings to render onboarding last_period_start %q, got %q", localToday, body)
	}
}

func TestOnboardingPersistsLastPeriodStartUsingFormTimezoneFallback(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "onboarding-form-tz@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localToday := services.DateAtLocation(nowUTC.In(location), location).Format("2006-01-02")

	step1Form := url.Values{
		"last_period_start":         {localToday},
		onboardingTimezoneFieldName: {timezoneName},
	}
	step1Response := onboardingHTMXResponse(t, app, http.MethodPost, "/onboarding/step1", authCookie, "", step1Form)
	if step1Response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d with body %q", http.StatusNoContent, step1Response.StatusCode, mustReadBodyString(t, step1Response.Body))
	}

	timezoneCookie := responseCookie(step1Response.Cookies(), timezoneCookieName)
	if timezoneCookie == nil || strings.TrimSpace(timezoneCookie.Value) != timezoneName {
		t.Fatalf("expected timezone cookie %q=%q after onboarding step1, got %#v", timezoneCookieName, timezoneName, timezoneCookie)
	}
	timezoneCookieHeader := joinCookieHeader(authCookie, cookiePair(timezoneCookie))

	step2Form := url.Values{
		"cycle_length":              {"28"},
		"period_length":             {"5"},
		"auto_period_fill":          {"true"},
		onboardingTimezoneFieldName: {timezoneName},
	}
	step2Response := onboardingHTMXResponse(t, app, http.MethodPost, "/onboarding/step2", timezoneCookieHeader, "", step2Form)
	assertStatusCode(t, step2Response, http.StatusNoContent)

	completeForm := url.Values{
		onboardingTimezoneFieldName: {timezoneName},
	}
	completeResponse := onboardingHTMXResponse(t, app, http.MethodPost, "/onboarding/complete", timezoneCookieHeader, "", completeForm)
	assertStatusCode(t, completeResponse, http.StatusOK)
	if redirect := completeResponse.Header.Get("HX-Redirect"); redirect != "/dashboard" {
		t.Fatalf("expected HX-Redirect /dashboard, got %q", redirect)
	}

	persisted := models.User{}
	if err := database.First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if persisted.LastPeriodStart == nil {
		t.Fatal("expected persisted last_period_start after onboarding")
	}

	savedLocalDay := services.DateAtLocation(persisted.LastPeriodStart.In(location), location).Format("2006-01-02")
	if savedLocalDay != localToday {
		t.Fatalf("expected persisted onboarding last_period_start %q, got %q", localToday, savedLocalDay)
	}

	settingsRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	settingsRequest.Header.Set("Cookie", timezoneCookieHeader)

	settingsResponse := mustAppResponse(t, app, settingsRequest)
	assertStatusCode(t, settingsResponse, http.StatusOK)

	body := mustReadBodyString(t, settingsResponse.Body)
	if !strings.Contains(body, `value="`+localToday+`"`) {
		t.Fatalf("expected settings to render onboarding last_period_start %q, got %q", localToday, body)
	}
}

func submitOnboardingStep1WithTimezone(t *testing.T, app *fiber.App, authCookie string, timezoneName string, form url.Values) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/onboarding/step1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusNoContent)
}

func submitOnboardingStep2WithTimezone(t *testing.T, app *fiber.App, authCookie string, timezoneName string, form url.Values) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/onboarding/step2", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusNoContent)
}

func submitOnboardingCompleteWithTimezone(t *testing.T, app *fiber.App, authCookie string, timezoneName string) {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/onboarding/complete", nil)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName))
	request.Header.Set(timezoneHeaderName, timezoneName)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	if redirect := response.Header.Get("HX-Redirect"); redirect != "/dashboard" {
		t.Fatalf("expected HX-Redirect /dashboard, got %q", redirect)
	}
}

func onboardingHTMXResponse(t *testing.T, app *fiber.App, method string, path string, cookieHeader string, timezoneName string, form url.Values) *http.Response {
	t.Helper()

	var bodyReader *strings.Reader
	if form == nil {
		bodyReader = strings.NewReader("")
	} else {
		bodyReader = strings.NewReader(form.Encode())
	}

	request := httptest.NewRequest(method, path, bodyReader)
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	if strings.TrimSpace(cookieHeader) != "" {
		request.Header.Set("Cookie", cookieHeader)
	}
	if strings.TrimSpace(timezoneName) != "" {
		request.Header.Set(timezoneHeaderName, timezoneName)
	}

	return mustAppResponse(t, app, request)
}
