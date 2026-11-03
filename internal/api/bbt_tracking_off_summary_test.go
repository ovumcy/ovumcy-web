package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestCalendarDaySummaryHidesTheBBTLineWhileTrackingIsOff pins WEB-451: the
// editor drops the BBT field while tracking is off, so the day summary no
// longer reads a stored reading back beside it.
func TestCalendarDaySummaryHidesTheBBTLineWhileTrackingIsOff(t *testing.T) {
	for _, tc := range []struct {
		name     string
		trackBBT bool
		wantBBT  bool
	}{
		{"tracking on", true, true},
		{"tracking off", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, database := newOnboardingTestApp(t)
			user := createOnboardingTestUser(t, database, "summary-bbt-"+strings.ReplaceAll(tc.name, " ", "-")+"@example.com", "StrongPass1", true)
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("track_bbt", tc.trackBBT).Error; err != nil {
				t.Fatalf("set BBT tracking: %v", err)
			}
			if err := database.Create(&models.DailyLog{
				UserID: user.ID,
				Date:   time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
				BBT:    new(36.4),
				Notes:  "tracked",
			}).Error; err != nil {
				t.Fatalf("create daily log: %v", err)
			}

			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
			request := httptest.NewRequest(http.MethodGet, "/calendar/day/2026-02-17", nil)
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", authCookie)
			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusOK)
			rendered := mustReadBodyString(t, response.Body)

			if got := strings.Contains(rendered, "36.4"); got != tc.wantBBT {
				t.Fatalf("expected the BBT reading in the summary=%v, got %v in %q", tc.wantBBT, got, rendered)
			}
		})
	}
}
