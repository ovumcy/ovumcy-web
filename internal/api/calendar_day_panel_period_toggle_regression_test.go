package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestCalendarDayPanelEditModePreservesAndSavesPeriodToggle(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-period-toggle@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	day := time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC)
	logEntry := models.DailyLog{
		UserID:   user.ID,
		Date:     day,
		IsPeriod: true,
		Flow:     models.FlowLight,
		Notes:    "entry",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	panelRequest := httptest.NewRequest(http.MethodGet, "/calendar/day/2026-02-17?mode=edit", nil)
	panelRequest.Header.Set("Accept-Language", "en")
	panelRequest.Header.Set("Cookie", authCookie)

	panelResponse, err := app.Test(panelRequest, -1)
	if err != nil {
		t.Fatalf("calendar day panel request failed: %v", err)
	}
	defer panelResponse.Body.Close()

	if panelResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", panelResponse.StatusCode)
	}

	panelBody, err := io.ReadAll(panelResponse.Body)
	if err != nil {
		t.Fatalf("read panel body: %v", err)
	}
	checkedPattern := regexp.MustCompile(`(?s)name="is_period"[^>]*checked`)
	if !checkedPattern.Match(panelBody) {
		t.Fatalf("expected edit-mode period toggle to stay checked for persisted period log")
	}

	form := url.Values{
		"flow":  {models.FlowNone},
		"notes": {"updated"},
	}
	saveRequest := httptest.NewRequest(http.MethodPost, "/api/days/2026-02-17", strings.NewReader(form.Encode()))
	saveRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveRequest.Header.Set("HX-Request", "true")
	saveRequest.Header.Set("Accept-Language", "en")
	saveRequest.Header.Set("Cookie", authCookie)

	saveResponse, err := app.Test(saveRequest, -1)
	if err != nil {
		t.Fatalf("save request failed: %v", err)
	}
	defer saveResponse.Body.Close()

	if saveResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected save status 200, got %d", saveResponse.StatusCode)
	}

	var updated models.DailyLog
	if err := database.Where("user_id = ? AND date = ?", user.ID, day).First(&updated).Error; err != nil {
		t.Fatalf("load updated log: %v", err)
	}
	if updated.IsPeriod {
		t.Fatalf("expected unchecked edit-mode period toggle to persist as false")
	}
}
