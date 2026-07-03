package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProfileUpdateHTMXStatusMarkupIsNonTransient(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "profile-htmx-status@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{
		"display_name": {"Nora"},
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("profile update htmx request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read htmx response body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, "status-ok") {
		t.Fatalf("expected htmx success status markup, got %q", rendered)
	}
	if strings.Contains(rendered, "status-transient") {
		t.Fatalf("did not expect transient status class in htmx success markup, got %q", rendered)
	}
}
