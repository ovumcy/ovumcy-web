package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUpsertDayCanonicalizesStoredDateToUTCMidnightForRequestTimezone is the
// HTTP-level lock for issue #49. A POST /api/days/{ISODate} arriving with
// a non-UTC request timezone (X-Ovumcy-Timezone header + ovumcy_tz cookie
// pair) must persist DailyLog.Date as UTC-midnight on disk. The same
// calendar day, fetched via GET in the same locale, must round-trip back
// through DayRange and find the row. Without the BeforeSave hook +
// DayRange UTC bounds, the upsert succeeds but a follow-up DELETE/UPSERT
// cycle would miss the row in UTC-minus zones, producing a unique-index
// conflict.
func TestUpsertDayCanonicalizesStoredDateToUTCMidnightForRequestTimezone(t *testing.T) {
	cases := []struct {
		name         string
		timezoneName string
		email        string
	}{
		{name: "America/Toronto UTC-5", timezoneName: "America/Toronto", email: "upsert-canonical-toronto@example.com"},
		{name: "Asia/Tokyo UTC+9", timezoneName: "Asia/Tokyo", email: "upsert-canonical-tokyo@example.com"},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			location, err := time.LoadLocation(tt.timezoneName)
			if err != nil {
				t.Skipf("zoneinfo for %s unavailable: %v", tt.timezoneName, err)
			}

			app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
			user := createOnboardingTestUser(t, database, tt.email, "StrongPass1", true)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			payload := map[string]any{
				"is_period":   true,
				"flow":        models.FlowMedium,
				"symptom_ids": []uint{},
				"notes":       "",
			}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatalf("marshal payload: %v", err)
			}

			postedDayRaw := "2026-02-10"
			request := httptest.NewRequest(http.MethodPost, "/api/days/"+postedDayRaw, bytes.NewReader(body))
			request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
			request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+location.String()))
			request.Header.Set(timezoneHeaderName, location.String())

			response, err := app.Test(request, -1)
			if err != nil {
				t.Fatalf("upsert request failed: %v", err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("expected upsert status 200, got %d", response.StatusCode)
			}

			var rawDate string
			if err := database.Raw("SELECT date FROM daily_logs WHERE user_id = ? ORDER BY date ASC LIMIT 1", user.ID).Row().Scan(&rawDate); err != nil {
				t.Fatalf("raw SELECT date: %v", err)
			}
			if !strings.HasPrefix(rawDate, postedDayRaw) {
				t.Fatalf("expected on-disk date prefix %q, got %q (calendar day must reflect the request locale day)", postedDayRaw, rawDate)
			}
			if strings.Contains(rawDate, "-05:") || strings.Contains(rawDate, "+09:") {
				t.Fatalf("expected canonical UTC offset on disk, got non-UTC offset in %q", rawDate)
			}

			roundTripRequest := httptest.NewRequest(http.MethodGet, "/api/days/"+postedDayRaw, nil)
			roundTripRequest.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+location.String()))
			roundTripRequest.Header.Set(timezoneHeaderName, location.String())

			roundTripResponse, err := app.Test(roundTripRequest, -1)
			if err != nil {
				t.Fatalf("round-trip GET failed: %v", err)
			}
			defer roundTripResponse.Body.Close()
			if roundTripResponse.StatusCode != http.StatusOK {
				t.Fatalf("expected round-trip status 200, got %d", roundTripResponse.StatusCode)
			}

			var loaded models.DailyLog
			if err := json.NewDecoder(roundTripResponse.Body).Decode(&loaded); err != nil {
				t.Fatalf("decode round-trip body: %v", err)
			}
			if !loaded.IsPeriod {
				t.Fatalf("expected round-trip entry to retain is_period=true; DayRange bounds may be drifting past the canonical row in %s", tt.timezoneName)
			}
			if loaded.Flow != models.FlowMedium {
				t.Fatalf("expected flow %q, got %q", models.FlowMedium, loaded.Flow)
			}
			if loaded.Date.Format("2006-01-02") != postedDayRaw {
				t.Fatalf("expected calendar day %s preserved through round-trip, got %s", postedDayRaw, loaded.Date.Format("2006-01-02"))
			}
		})
	}
}
