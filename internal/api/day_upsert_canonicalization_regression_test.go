package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestUpsertDayCanonicalizesStoredDateToUTCMidnightForRequestTimezone is the
// HTTP-level lock for issue #49. A POST /api/v1/days/{ISODate} arriving with
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
			request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+postedDayRaw, bytes.NewReader(body))
			request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
			request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+location.String()))
			request.Header.Set(timezoneHeaderName, location.String())

			response, err := app.Test(request, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("upsert request failed: %v", err)
			}
			defer func() { _ = response.Body.Close() }()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("expected upsert status 200, got %d", response.StatusCode)
			}

			var rawDate string
			if err := database.Raw("SELECT date FROM daily_logs WHERE user_id = ? ORDER BY date ASC LIMIT 1", user.ID).Row().Scan(&rawDate); err != nil {
				t.Fatalf("raw SELECT date: %v", err)
			}
			assertUpsertUTCDate(t, rawDate, postedDayRaw)

			roundTripRequest := httptest.NewRequest(http.MethodGet, "/api/v1/days/"+postedDayRaw, nil)
			roundTripRequest.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+location.String()))
			roundTripRequest.Header.Set(timezoneHeaderName, location.String())

			roundTripResponse, err := app.Test(roundTripRequest, testConfigNoTimeout)
			if err != nil {
				t.Fatalf("round-trip GET failed: %v", err)
			}
			defer func() { _ = roundTripResponse.Body.Close() }()
			if roundTripResponse.StatusCode != http.StatusOK {
				t.Fatalf("expected round-trip status 200, got %d", roundTripResponse.StatusCode)
			}

			// The /api/v1/days transport DTO emits `date` as a calendar
			// date-only string (docs/openapi.yaml format: date), so decode
			// into the response shape rather than models.DailyLog.
			var loaded dayResponse
			if err := json.NewDecoder(roundTripResponse.Body).Decode(&loaded); err != nil {
				t.Fatalf("decode round-trip body: %v", err)
			}
			if !loaded.IsPeriod {
				t.Fatalf("expected round-trip entry to retain is_period=true; DayRange bounds may be drifting past the canonical row in %s", tt.timezoneName)
			}
			if loaded.Flow != models.FlowMedium {
				t.Fatalf("expected flow %q, got %q", models.FlowMedium, loaded.Flow)
			}
			if loaded.Date != postedDayRaw {
				t.Fatalf("expected calendar day %s preserved through round-trip, got %s", postedDayRaw, loaded.Date)
			}
		})
	}
}

// TestUpsertDayRefusesACalendarDayTheRequestZoneNeverHad is the transport-level
// lock for the whole-day-skipped case. Pacific/Apia crossed the date line at the
// end of 2011 and never had a 2011-12-30 at all; parsing that path parameter used
// to yield 2011-12-29 with no error, so the save landed on a day the request had
// not named and the client had no way to tell. The route must answer a mapped
// validation refusal — the same one an empty or malformed date already gets —
// and must not write a row.
func TestUpsertDayRefusesACalendarDayTheRequestZoneNeverHad(t *testing.T) {
	const zoneName = "Pacific/Apia"

	if _, err := time.LoadLocation(zoneName); err != nil {
		t.Fatalf("zoneinfo for %s unavailable: %v", zoneName, err)
	}

	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "upsert-nonexistent-day@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	body, err := json.Marshal(map[string]any{
		"is_period":   true,
		"flow":        models.FlowMedium,
		"symptom_ids": []uint{},
		"notes":       "",
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/2011-12-30", bytes.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"="+zoneName))
	request.Header.Set(timezoneHeaderName, zoneName)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("upsert request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400 for a day %s never had, got %d", zoneName, response.StatusCode)
	}

	var envelope struct {
		Error       string `json:"error"`
		ErrorDetail struct {
			Key      string `json:"key"`
			Category string `json:"category"`
		} `json:"error_detail"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode error envelope: %v", err)
	}
	if envelope.ErrorDetail.Category != string(APIErrorCategoryValidation) {
		t.Fatalf("expected a validation refusal the client can act on, got category %q (key %q)",
			envelope.ErrorDetail.Category, envelope.ErrorDetail.Key)
	}
	if envelope.Error == "" {
		t.Fatal("expected a mapped error message in the envelope")
	}

	var stored int64
	if err := database.Raw("SELECT COUNT(*) FROM daily_logs WHERE user_id = ?", user.ID).Row().Scan(&stored); err != nil {
		t.Fatalf("count daily_logs: %v", err)
	}
	if stored != 0 {
		t.Fatalf("expected no day written for a refused date, found %d row(s) — the save shifted to a day the request never named", stored)
	}
}

func assertUpsertUTCDate(t *testing.T, rawDate, expectedPrefix string) {
	t.Helper()
	if !strings.HasPrefix(rawDate, expectedPrefix) {
		t.Fatalf("expected on-disk date prefix %q, got %q (calendar day must reflect the request locale day)", expectedPrefix, rawDate)
	}
	if strings.Contains(rawDate, "-05:") || strings.Contains(rawDate, "+09:") {
		t.Fatalf("expected canonical UTC offset on disk, got non-UTC offset in %q", rawDate)
	}
}
