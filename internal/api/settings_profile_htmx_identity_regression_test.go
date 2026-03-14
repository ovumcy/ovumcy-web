package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProfileUpdateHTMXReturnsUnicodeSafeIdentityOOBMarkup(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "profile-htmx-owner@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{
		"display_name": {"Катя"},
	}
	request := httptest.NewRequest(http.MethodPost, "/api/settings/profile", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("profile update request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for htmx profile update, got %d", response.StatusCode)
	}

	if identity := strings.TrimSpace(response.Header.Get("X-Ovumcy-Profile-Identity")); identity != "" {
		t.Fatalf("did not expect legacy identity header, got %q", identity)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read htmx profile update body: %v", err)
	}
	rendered := string(body)
	if strings.Count(rendered, `data-current-user-identity`) != 2 {
		t.Fatalf("expected both nav identity chips in htmx response, got %q", rendered)
	}
	if strings.Contains(rendered, `nav-user-chip-empty`) {
		t.Fatalf("did not expect empty-state nav identity styling after setting a display name, got %q", rendered)
	}
	if strings.Contains(rendered, "profile-htmx-owner@example.com") {
		t.Fatalf("did not expect email fallback in identity response, got %q", rendered)
	}
	for _, id := range []string{`id="nav-user-chip-desktop"`, `id="nav-user-chip-mobile"`} {
		if !strings.Contains(rendered, id) {
			t.Fatalf("expected nav identity chip %s in htmx response", id)
		}
	}
}

func TestProfileUpdateHTMXReturnsFallbackIdentityWhenDisplayNameCleared(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "profile-htmx-clear@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	seedForm := url.Values{
		"display_name": {"Maya"},
	}
	seedRequest := httptest.NewRequest(http.MethodPost, "/api/settings/profile", strings.NewReader(seedForm.Encode()))
	seedRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	seedRequest.Header.Set("Cookie", authCookie)
	seedRequest.Header.Set("HX-Request", "true")

	seedResponse, err := app.Test(seedRequest, -1)
	if err != nil {
		t.Fatalf("seed profile update request failed: %v", err)
	}
	seedResponse.Body.Close()
	if seedResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for seed htmx profile update, got %d", seedResponse.StatusCode)
	}

	clearForm := url.Values{
		"display_name": {"   "},
	}
	clearRequest := httptest.NewRequest(http.MethodPost, "/api/settings/profile", strings.NewReader(clearForm.Encode()))
	clearRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	clearRequest.Header.Set("Cookie", authCookie)
	clearRequest.Header.Set("HX-Request", "true")
	clearRequest.Header.Set("Accept-Language", "en")

	clearResponse, err := app.Test(clearRequest, -1)
	if err != nil {
		t.Fatalf("clear profile update request failed: %v", err)
	}
	defer clearResponse.Body.Close()

	if clearResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for clear htmx profile update, got %d", clearResponse.StatusCode)
	}

	if identity := strings.TrimSpace(clearResponse.Header.Get("X-Ovumcy-Profile-Identity")); identity != "" {
		t.Fatalf("did not expect legacy identity header when display name is cleared, got %q", identity)
	}

	body, err := io.ReadAll(clearResponse.Body)
	if err != nil {
		t.Fatalf("read clear htmx profile body: %v", err)
	}
	rendered := string(body)
	if strings.Contains(rendered, "profile-htmx-clear@example.com") {
		t.Fatalf("did not expect email fallback in cleared identity response, got %q", rendered)
	}
	if strings.Count(rendered, `nav-user-chip-empty`) < 2 {
		t.Fatalf("expected cleared profile response to render empty-state nav chips, got %q", rendered)
	}
	if strings.Contains(rendered, `data-current-user-identity`) {
		t.Fatalf("did not expect display-name identity spans after clearing the display name, got %q", rendered)
	}
	for _, id := range []string{`id="nav-user-chip-desktop"`, `id="nav-user-chip-mobile"`} {
		if !strings.Contains(rendered, id) {
			t.Fatalf("expected nav identity chip %s in cleared htmx response", id)
		}
	}
}
