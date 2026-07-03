package api

import (
	"github.com/ovumcy/ovumcy-web/internal/services"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOnboardingStep1RejectsFutureAndTooOldDates(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "step1-validation@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	futureDate := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, 1).Format("2006-01-02")
	futureForm := url.Values{
		"last_period_start": {futureDate},
	}
	futureRequest := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/1", strings.NewReader(futureForm.Encode()))
	futureRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	futureRequest.Header.Set("HX-Request", "true")
	futureRequest.Header.Set("Cookie", authCookie)

	futureResponse, err := app.Test(futureRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("future date request failed: %v", err)
	}
	defer futureResponse.Body.Close()
	if futureResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected future date status 400, got %d", futureResponse.StatusCode)
	}

	oldDate := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -61).Format("2006-01-02")
	oldForm := url.Values{
		"last_period_start": {oldDate},
	}
	oldRequest := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/1", strings.NewReader(oldForm.Encode()))
	oldRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	oldRequest.Header.Set("HX-Request", "true")
	oldRequest.Header.Set("Cookie", authCookie)

	oldResponse, err := app.Test(oldRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("old date request failed: %v", err)
	}
	defer oldResponse.Body.Close()
	if oldResponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected old date status 400, got %d", oldResponse.StatusCode)
	}
}

func TestOnboardingStep1IgnoresUnexpectedPeriodEndInput(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "step1-extra-period-end@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	stepDate := services.DateAtLocation(time.Now().In(time.UTC), time.UTC).AddDate(0, 0, -4)
	form := url.Values{
		"last_period_start": {stepDate.Format("2006-01-02")},
		"period_end":        {stepDate.Format("2006-01-02")},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("step1 request failed: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", response.StatusCode)
	}

	var persistedUser struct {
		PeriodLength int
	}
	if err := database.Table("users").Select("period_length").Where("id = ?", user.ID).First(&persistedUser).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if persistedUser.PeriodLength != 5 {
		t.Fatalf("expected step1 to ignore period_end and keep period_length=5, got %d", persistedUser.PeriodLength)
	}
}

func TestOnboardingStep1RejectsFarHistoricalDate(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "step1-far-past-validation@example.com", "StrongPass1", false)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{
		"last_period_start": {"2024-01-01"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/onboarding/steps/1", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("far historical date request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected far historical date status 400, got %d", response.StatusCode)
	}
}
