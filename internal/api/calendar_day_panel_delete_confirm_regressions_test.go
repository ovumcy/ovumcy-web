package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func TestCalendarDayPanelDeleteEntryUsesConfirmForm(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-confirm@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	logEntry := models.DailyLog{
		UserID:   user.ID,
		Date:     time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
		IsPeriod: true,
		Flow:     models.FlowMedium,
		Notes:    "entry",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/calendar/day/2026-02-17?mode=edit", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("calendar day panel request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read panel body: %v", err)
	}
	rendered := string(body)

	deleteFormPattern := regexp.MustCompile(`(?s)<form[^>]+hx-delete="/api/log/delete\?date=2026-02-17&source=calendar"[^>]+hx-target="#day-editor"[^>]+data-confirm="[^"]+"[^>]+data-confirm-accept="[^"]+"`)
	if !deleteFormPattern.MatchString(rendered) {
		t.Fatalf("expected confirmed delete form in calendar edit panel")
	}

	deleteButtonPattern := regexp.MustCompile(`(?s)<button[^>]+type="submit"[^>]+class="danger-link"[^>]*>[^<]+</button>`)
	if !deleteButtonPattern.MatchString(rendered) {
		t.Fatalf("expected visible destructive submit button in calendar edit panel")
	}
}
