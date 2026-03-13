package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
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
}

func TestDashboardNavigationShowsDisplayNameWithoutEmailFallback(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "identity-owner@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("display_name", "Maya").Error; err != nil {
		t.Fatalf("seed display name: %v", err)
	}
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
	if strings.Contains(rendered, "identity-owner") {
		t.Fatalf("did not expect local-part identity in navigation")
	}
	if strings.Contains(rendered, "identity-owner@example.com") {
		t.Fatalf("did not expect email identity in navigation")
	}
	if !strings.Contains(rendered, `data-current-user-identity`) || !strings.Contains(rendered, ">Maya<") {
		t.Fatalf("expected dashboard navigation to render the saved display name, got %q", rendered)
	}
}

func TestDashboardNavigationShowsProfileHintWhenDisplayNameEmpty(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "identity-empty@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := mustRenderDashboard(t, app, authCookie, "en")
	if !strings.Contains(rendered, `title="Profile settings"`) {
		t.Fatalf("expected empty display-name navigation tooltip, got %q", rendered)
	}
	if strings.Contains(rendered, ">Add profile name<") {
		t.Fatalf("did not expect empty display-name placeholder as visible nav label, got %q", rendered)
	}
	if strings.Contains(rendered, "identity-empty@example.com") || strings.Contains(rendered, "identity-empty") {
		t.Fatalf("did not expect email fallback in navigation when display name is empty")
	}
}

func TestDashboardHeaderOmitsLanguageSwitch(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "lang-switch-labels@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := mustRenderDashboard(t, app, authCookie, "ru")
	for _, label := range []string{"RU", "EN", "ES"} {
		if strings.Contains(rendered, ">"+label+"</a>") {
			t.Fatalf("did not expect %s language shortcut in dashboard header", label)
		}
	}
}

func mustRenderDashboard(t *testing.T, app *fiber.App, authCookie string, languageCookie string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	request.Header.Set("Accept-Language", "en")
	if strings.TrimSpace(languageCookie) == "" {
		request.Header.Set("Cookie", authCookie)
	} else {
		request.Header.Set("Cookie", authCookie+"; ovumcy_lang="+languageCookie)
	}

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
	return string(body)
}
