package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
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

	logoutForms := regexp.MustCompile(`(?is)<form\b[^>]*action="/logout"[^>]*>`).FindAllString(rendered, -1)
	if len(logoutForms) < 2 {
		t.Fatalf("expected dashboard navigation to render both desktop and mobile logout forms")
	}
	for _, form := range logoutForms {
		if !strings.Contains(form, `method="post"`) {
			t.Fatalf("expected logout form to use POST, got %q", form)
		}
		if !strings.Contains(form, `data-confirm="`) {
			t.Fatalf("expected logout form to carry confirmation wiring, got %q", form)
		}
	}
	if strings.Contains(rendered, `action="/api/auth/logout"`) {
		t.Fatalf("did not expect dashboard navigation to post to the raw API logout route")
	}
	if got := len(regexp.MustCompile(`(?is)<input\b[^>]*name="csrf_token"[^>]*>`).FindAllString(rendered, -1)); got < len(logoutForms) {
		t.Fatalf("expected csrf token hidden fields for each logout form, got %d for %d forms", got, len(logoutForms))
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
	if strings.Count(rendered, `data-current-user-identity`) != 2 {
		t.Fatalf("expected both dashboard nav identity chips to render the saved display name, got %q", rendered)
	}
	if strings.Contains(rendered, `nav-user-chip-empty`) {
		t.Fatalf("did not expect empty identity chip styling when display name exists, got %q", rendered)
	}
	for _, id := range []string{`id="nav-user-chip-desktop"`, `id="nav-user-chip-mobile"`} {
		if !strings.Contains(rendered, id) {
			t.Fatalf("expected dashboard navigation chip %s in response", id)
		}
	}
}

func TestDashboardNavigationShowsProfileHintWhenDisplayNameEmpty(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "identity-empty@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := mustRenderDashboard(t, app, authCookie, "en")
	if strings.Contains(rendered, "identity-empty@example.com") || strings.Contains(rendered, "identity-empty") {
		t.Fatalf("did not expect email fallback in navigation when display name is empty")
	}
	if strings.Count(rendered, `nav-user-chip-empty`) < 2 {
		t.Fatalf("expected both dashboard nav identity chips to use the empty-state styling, got %q", rendered)
	}
	if strings.Contains(rendered, `data-current-user-identity`) {
		t.Fatalf("did not expect display-name identity spans when display name is empty, got %q", rendered)
	}
	for _, id := range []string{`id="nav-user-chip-desktop"`, `id="nav-user-chip-mobile"`} {
		if !strings.Contains(rendered, id) {
			t.Fatalf("expected dashboard navigation chip %s in response", id)
		}
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

func TestDashboardYesterdayLinkTargetsCalendarEditModeForSelectedDay(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "yesterday-link@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := mustRenderDashboard(t, app, authCookie, "en")
	yesterdayLinkPattern := regexp.MustCompile(`href="/calendar\?month=\d{4}-\d{2}&day=\d{4}-\d{2}-\d{2}&edit=1"`)
	if !yesterdayLinkPattern.MatchString(rendered) {
		t.Fatalf("expected yesterday link to target calendar selected day edit mode, got %q", rendered)
	}
	if strings.Contains(rendered, `selected=`) {
		t.Fatalf("did not expect legacy selected query parameter in dashboard links")
	}
}

func TestCalendarSelectedDayLoadsEditModeWhenRequested(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-edit-selected@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=2026-03&day=2026-03-12&edit=1", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	rendered := mustReadBodyString(t, response.Body)
	if !strings.Contains(rendered, `data-selected-date="2026-03-12"`) {
		t.Fatalf("expected calendar page to keep the selected day in the view state, got %q", rendered)
	}
	if !strings.Contains(rendered, `id="day-editor"`) || !strings.Contains(rendered, `hx-get="/calendar/day/2026-03-12?mode=edit"`) {
		t.Fatalf("expected selected calendar day to lazy-load edit mode in the day editor, got %q", rendered)
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
