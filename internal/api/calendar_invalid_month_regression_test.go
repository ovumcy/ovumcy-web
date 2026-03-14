package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCalendarInvalidMonthHTMLRedirectsToCurrentMonth(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-invalid-month@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=9999-99", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "/calendar" {
		t.Fatalf("expected redirect to /calendar, got %q", location)
	}
}

func TestCalendarInvalidMonthHTMXRedirectsToCurrentMonth(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-invalid-month-htmx@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=0000-00", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("HX-Request", "true")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("calendar htmx request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); redirect != "/calendar" {
		t.Fatalf("expected HX-Redirect /calendar, got %q", redirect)
	}
}

func TestCalendarInvalidMonthJSONKeepsValidationError(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-invalid-month-json@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=not-a-month", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("calendar json request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid month" {
		t.Fatalf("expected invalid month error, got %q", got)
	}
}
