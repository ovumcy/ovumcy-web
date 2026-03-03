package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardLogoutFormsRequireConfirmation(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "logout-confirm@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)
	if strings.Count(rendered, `action="/api/auth/logout"`) < 2 {
		t.Fatalf("expected desktop and mobile logout forms")
	}
	if strings.Count(rendered, `action="/api/auth/logout" method="post"`) < 2 {
		t.Fatalf("expected logout forms to use POST method")
	}
	if strings.Count(rendered, `name="csrf_token" value="`) < 2 {
		t.Fatalf("expected csrf token hidden fields on both logout forms")
	}
	if strings.Count(rendered, `data-confirm="Log out of your account now?"`) < 2 {
		t.Fatalf("expected confirmation attribute for both logout forms")
	}
}

func TestDashboardNavigationShowsCurrentUserIdentity(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "identity-owner@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, `aria-label="Current user"`) {
		t.Fatalf("expected current user label in navigation")
	}
	if !strings.Contains(rendered, "identity-owner") {
		t.Fatalf("expected local-part identity in navigation when display name is empty")
	}
	if strings.Contains(rendered, "identity-owner@example.com") {
		t.Fatalf("did not expect full email identity in navigation fallback")
	}
}

func TestDashboardLanguageSwitchShowsVisibleRUAndENLabels(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "lang-switch-labels@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("dashboard request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read dashboard body: %v", err)
	}
	rendered := string(body)
	if !strings.Contains(rendered, ">RU</a>") {
		t.Fatalf("expected RU language label in switcher")
	}
	if !strings.Contains(rendered, ">EN</a>") {
		t.Fatalf("expected EN language label in switcher")
	}
	if !strings.Contains(rendered, `aria-current="page">EN</a>`) {
		t.Fatalf("expected active EN language link to expose aria-current marker")
	}

	russianRequest := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	russianRequest.Header.Set("Accept-Language", "en")
	russianRequest.Header.Set("Cookie", authCookie+"; ovumcy_lang=ru")

	russianResponse, err := app.Test(russianRequest, -1)
	if err != nil {
		t.Fatalf("dashboard request with russian cookie failed: %v", err)
	}
	defer russianResponse.Body.Close()

	if russianResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200 for russian dashboard, got %d", russianResponse.StatusCode)
	}

	russianBody, err := io.ReadAll(russianResponse.Body)
	if err != nil {
		t.Fatalf("read russian dashboard body: %v", err)
	}
	renderedRussian := string(russianBody)
	if !strings.Contains(renderedRussian, `aria-current="page">RU</a>`) {
		t.Fatalf("expected active RU language link to expose aria-current marker")
	}
	if strings.Contains(renderedRussian, `aria-current="page">EN</a>`) {
		t.Fatalf("did not expect EN link to stay active when russian language cookie is set")
	}
}
