package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestDayReadEndpointsUseRequestTimezoneForLocalCalendarDay(t *testing.T) {
	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user := createOnboardingTestUser(t, database, "days-read-timezone@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	nowUTC := time.Now().UTC()
	timezoneName, location := timezoneWithDifferentCalendarDay(t, nowUTC)
	localDay := services.DateAtLocation(nowUTC.In(location), location)
	localDayRaw := localDay.Format("2006-01-02")

	seed := models.DailyLog{
		UserID:   user.ID,
		Date:     localDay,
		IsPeriod: true,
		Flow:     models.FlowLight,
		Notes:    "timezone read note",
	}
	if err := database.Create(&seed).Error; err != nil {
		t.Fatalf("seed timezone daily log: %v", err)
	}

	cookieHeader := joinCookieHeader(authCookie, timezoneCookieName+"="+timezoneName)

	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/days?from="+localDayRaw+"&to="+localDayRaw, nil)
	listRequest.Header.Set("Accept", "application/json")
	listRequest.Header.Set("Cookie", cookieHeader)
	listRequest.Header.Set(timezoneHeaderName, timezoneName)

	listResponse := mustAppResponse(t, app, listRequest)
	assertStatusCode(t, listResponse, http.StatusOK)

	// The /api/v1/days transport DTO emits `date` as a calendar date-only
	// string (docs/openapi.yaml format: date), so decode into the response
	// shape rather than models.DailyLog (whose Date is RFC3339 time.Time).
	var listPayload []dayResponse
	if err := json.NewDecoder(listResponse.Body).Decode(&listPayload); err != nil {
		t.Fatalf("decode day list payload: %v", err)
	}
	if len(listPayload) != 1 {
		t.Fatalf("expected one log for request-local day %s, got %#v", localDayRaw, listPayload)
	}
	if listPayload[0].Notes != seed.Notes {
		t.Fatalf("expected listed notes %q, got %q", seed.Notes, listPayload[0].Notes)
	}
	if listPayload[0].Date != localDayRaw {
		t.Fatalf("expected listed date %q for request-local day, got %q", localDayRaw, listPayload[0].Date)
	}

	dayRequest := httptest.NewRequest(http.MethodGet, "/api/v1/days/"+localDayRaw, nil)
	dayRequest.Header.Set("Accept", "application/json")
	dayRequest.Header.Set("Cookie", cookieHeader)
	dayRequest.Header.Set(timezoneHeaderName, timezoneName)

	dayHTTPResponse := mustAppResponse(t, app, dayRequest)
	assertStatusCode(t, dayHTTPResponse, http.StatusOK)

	dayPayload := dayResponse{}
	if err := json.NewDecoder(dayHTTPResponse.Body).Decode(&dayPayload); err != nil {
		t.Fatalf("decode day payload: %v", err)
	}
	if dayPayload.Notes != seed.Notes {
		t.Fatalf("expected fetched day notes %q, got %q", seed.Notes, dayPayload.Notes)
	}
	if dayPayload.Date != localDayRaw {
		t.Fatalf("expected fetched date %q for request-local day, got %q", localDayRaw, dayPayload.Date)
	}

	existsRequest := httptest.NewRequest(http.MethodHead, "/api/v1/days/"+localDayRaw, nil)
	existsRequest.Header.Set("Cookie", cookieHeader)
	existsRequest.Header.Set(timezoneHeaderName, timezoneName)

	existsResponse := mustAppResponse(t, app, existsRequest)
	assertStatusCode(t, existsResponse, http.StatusOK)
}
