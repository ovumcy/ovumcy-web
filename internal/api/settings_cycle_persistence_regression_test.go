package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestSettingsCycleUpdatePersistsAndRendersAfterReload(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-cycle-persist@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"cycle_length":     15,
		"period_length":    5,
		"auto_period_fill": false,
	}).Error; err != nil {
		t.Fatalf("set initial cycle values: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	form := url.Values{
		"cycle_length":      {"28"},
		"period_length":     {"6"},
		"auto_period_fill":  {"true"},
		"last_period_start": {"2026-02-10"},
	}
	updateBody := submitSettingsCycleUpdate(t, app, authCookie, form)
	assertBodyContainsAll(t, updateBody,
		bodyStringMatch{fragment: "status-ok", message: "expected htmx success status markup"},
		bodyStringMatch{fragment: "data-dismiss-status", message: "expected dismiss button marker in htmx success markup"},
	)
	assertBodyNotContainsAll(t, updateBody,
		bodyStringMatch{fragment: "status-transient", message: "did not expect transient status class in htmx success markup"},
	)

	persisted := models.User{}
	if err := database.Select("cycle_length", "period_length", "auto_period_fill", "last_period_start").First(&persisted, user.ID).Error; err != nil {
		t.Fatalf("load persisted user cycle values: %v", err)
	}
	if persisted.CycleLength != 28 {
		t.Fatalf("expected persisted cycle_length=28, got %d", persisted.CycleLength)
	}
	if persisted.PeriodLength != 6 {
		t.Fatalf("expected persisted period_length=6, got %d", persisted.PeriodLength)
	}
	if !persisted.AutoPeriodFill {
		t.Fatalf("expected persisted auto_period_fill=true")
	}
	if persisted.LastPeriodStart == nil || persisted.LastPeriodStart.Format("2006-01-02") != "2026-02-10" {
		t.Fatalf("expected persisted last_period_start=2026-02-10, got %v", persisted.LastPeriodStart)
	}

	rendered := renderSettingsPageForTest(t, app, authCookie)
	cycleInputPattern := regexp.MustCompile(`(?s)name="cycle_length".*?value="28"`)
	if !cycleInputPattern.MatchString(rendered) {
		t.Fatalf("expected cycle slider value 28 in rendered settings page")
	}
	periodInputPattern := regexp.MustCompile(`(?s)name="period_length".*?value="6"`)
	if !periodInputPattern.MatchString(rendered) {
		t.Fatalf("expected period slider value 6 in rendered settings page")
	}
	lastPeriodInputPattern := regexp.MustCompile(`(?s)name="last_period_start".*?value="2026-02-10"`)
	if !lastPeriodInputPattern.MatchString(rendered) {
		t.Fatalf("expected last_period_start date input to render persisted value")
	}
}

func submitSettingsCycleUpdate(t *testing.T, app *fiber.App, authCookie string, form url.Values) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/settings/cycle", strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("HX-Request", "true")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}
