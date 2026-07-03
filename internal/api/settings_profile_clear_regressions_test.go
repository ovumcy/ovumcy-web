package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestProfileUpdateShowsMessageWhenDisplayNameCleared(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "profile-clear@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("display_name", "Maya").Error; err != nil {
		t.Fatalf("seed display name: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{
		"display_name": {""},
	}
	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("profile clear request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d", response.StatusCode)
	}

	flashValue := responseCookieValue(response.Cookies(), flashCookieName)
	if flashValue == "" {
		t.Fatalf("expected flash cookie for profile clear")
	}

	updatedUser := models.User{}
	if err := database.First(&updatedUser, user.ID).Error; err != nil {
		t.Fatalf("load updated user: %v", err)
	}
	if updatedUser.DisplayName != "" {
		t.Fatalf("expected display name to be cleared, got %q", updatedUser.DisplayName)
	}

	settingsRequest := httptest.NewRequest(http.MethodGet, "/settings", nil)
	settingsRequest.Header.Set("Accept-Language", "en")
	settingsRequest.Header.Set("Cookie", authCookie+"; "+flashCookieName+"="+flashValue)
	settingsResponse, err := app.Test(settingsRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("settings request failed: %v", err)
	}
	defer settingsResponse.Body.Close()

	settingsDocument := mustParseHTMLDocument(t, mustReadBodyString(t, settingsResponse.Body))
	if htmlFlashByKey(settingsDocument, "settings.success.profile_name_cleared") == nil {
		t.Fatalf("expected profile_name_cleared success flash key")
	}

	dashboardRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	dashboardRequest.Header.Set("Accept-Language", "en")
	dashboardRequest.Header.Set("Cookie", authCookie)
	dashboardResponse, err := app.Test(dashboardRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer dashboardResponse.Body.Close()

	dashboardBody, err := io.ReadAll(dashboardResponse.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	if strings.Contains(string(dashboardBody), ">Maya</span>") {
		t.Fatalf("did not expect stale display name in navigation after clear")
	}
	if strings.Contains(string(dashboardBody), "profile-clear@example.com") {
		t.Fatalf("did not expect full email in navigation fallback after display name clear")
	}
	if strings.Contains(string(dashboardBody), "profile-clear") {
		t.Fatalf("did not expect local-part identity fallback after display name clear")
	}
}
