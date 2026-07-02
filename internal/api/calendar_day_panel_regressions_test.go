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

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestCalendarDayPanelReadonlySummaryShowsSavedBBT(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-bbt-summary@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"track_bbt":        true,
		"temperature_unit": services.TemperatureUnitCelsius,
	}).Error; err != nil {
		t.Fatalf("enable BBT tracking: %v", err)
	}

	logEntry := models.DailyLog{
		UserID: user.ID,
		Date:   time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
		BBT:    models.NewBBT(36.75),
		Notes:  "tracked",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}

	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodGet, "/calendar/day/2026-02-17", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)

	rendered := mustReadBodyString(t, response.Body)
	if !strings.Contains(rendered, "BBT") {
		t.Fatalf("expected BBT label in calendar day summary, got %q", rendered)
	}
	if !strings.Contains(rendered, "36.75 °C") {
		t.Fatalf("expected saved BBT value in calendar day summary, got %q", rendered)
	}
}

func TestCalendarDayPanelEditModeRendersDeleteActionForExistingEntry(t *testing.T) {
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

	assertBodyContainsAll(t, rendered,
		bodyStringMatch{fragment: `data-day-delete-form`, message: "expected delete form affordance for existing calendar entry"},
		bodyStringMatch{fragment: `data-day-delete-button`, message: "expected delete button affordance for existing calendar entry"},
	)
}

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
	saveRequest := httptest.NewRequest(http.MethodPut, "/api/v1/days/2026-02-17", strings.NewReader(form.Encode()))
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

// Delete-day handler regressions for DELETE /api/v1/days/:date. The route
// is wired through handler.OwnerOnly and removes the daily log row for the
// requesting user via service.DeleteDayEntry, which canonicalizes the
// calendar day to the UTC [dayStart, dayStart+24h) window used by writes.
// Coverage targets: auth gate, owner gate, date parsing, persistence,
// HTMX vs non-HTMX response shape, timezone-aware delete.

func TestDeleteDayWithoutAuthCookieReturnsUnauthorized(t *testing.T) {
	app, _ := newOnboardingTestApp(t)

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/2026-02-17", nil)
	request.Header.Set("Accept", "application/json")
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated JSON DELETE, got %d", response.StatusCode)
	}
}

func TestDeleteDayWithInvalidDateReturnsValidationError(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-day-bad-date@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/not-a-date", nil)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid date path param, got %d", response.StatusCode)
	}
}

func TestDeleteDayRemovesPersistedLogAndReturnsNoContent(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-day-ok@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	day := time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC)
	if err := database.Create(&models.DailyLog{
		UserID:   user.ID,
		Date:     day,
		IsPeriod: true,
		Flow:     models.FlowMedium,
		Notes:    "scheduled for delete",
	}).Error; err != nil {
		t.Fatalf("seed daily log: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/2026-02-17", nil)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 on successful delete, got %d", response.StatusCode)
	}

	var rowCount int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND date = ?", user.ID, day).Count(&rowCount).Error; err != nil {
		t.Fatalf("count remaining rows: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("expected DELETE to remove the row, %d still present", rowCount)
	}
}

// TestDeleteDayWithHTMXReturnsRefreshedDayEditorPartial locks the HTMX
// contract: a calendar-day editor wired through HTMX expects an immediate
// 200 with the refreshed (now empty) day editor partial, plus the
// calendar-day-updated trigger so peer panels re-render. A 204 would force
// the client to hand-roll a refresh request.
func TestDeleteDayWithHTMXReturnsRefreshedDayEditorPartial(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-day-htmx@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	day := time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC)
	if err := database.Create(&models.DailyLog{
		UserID: user.ID,
		Date:   day,
		Notes:  "to-be-deleted",
	}).Error; err != nil {
		t.Fatalf("seed daily log: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/2026-02-17", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("HX-Request", "true")
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 (HTMX partial) on HTMX delete, got %d", response.StatusCode)
	}
	if trigger := response.Header.Get("HX-Trigger"); trigger != "calendar-day-updated" {
		t.Fatalf("expected HX-Trigger calendar-day-updated, got %q", trigger)
	}
}

// TestDeleteDayForCalendarDayWithoutPersistedRowReturnsNoContent locks
// idempotent semantics. A DELETE on a day that has no row must still return
// 204 — the service silently no-ops via DeleteByUserAndDayRange. Without
// this lock a future tightening could surface a 404 and break the
// auto-clear flow the calendar editor depends on.
func TestDeleteDayForCalendarDayWithoutPersistedRowReturnsNoContent(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-day-idempotent@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/2026-02-17", nil)
	request.Header.Set("Cookie", authCookie)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 for idempotent DELETE on empty day, got %d", response.StatusCode)
	}
}

// TestDeleteDayDoesNotRemoveAnotherOwnersRow locks the owner-scoping
// invariant for the day-delete code path. Two owner accounts each hold a
// log on the same calendar day; when owner B sends DELETE for that day,
// only owner B's row may go — owner A's row must survive. The service
// scopes by user_id; this test catches any future scope drift that would
// turn the route into a cross-account delete vector.
func TestDeleteDayDoesNotRemoveAnotherOwnersRow(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	ownerA := createOnboardingTestUser(t, database, "delete-day-owner-a@example.com", "StrongPass1", true)
	ownerB := createOnboardingTestUser(t, database, "delete-day-owner-b@example.com", "StrongPass1", true)

	day := time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC)
	for _, ownerID := range []uint{ownerA.ID, ownerB.ID} {
		if err := database.Create(&models.DailyLog{
			UserID:   ownerID,
			Date:     day,
			IsPeriod: true,
			Flow:     models.FlowMedium,
		}).Error; err != nil {
			t.Fatalf("seed daily log for owner %d: %v", ownerID, err)
		}
	}

	authCookieB := loginAndExtractAuthCookie(t, app, ownerB.Email, "StrongPass1")
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/2026-02-17", nil)
	request.Header.Set("Cookie", authCookieB)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 on owner B's DELETE, got %d", response.StatusCode)
	}

	var ownerARowCount int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND date = ?", ownerA.ID, day).Count(&ownerARowCount).Error; err != nil {
		t.Fatalf("count owner A rows: %v", err)
	}
	if ownerARowCount != 1 {
		t.Fatalf("owner B's DELETE leaked across owners: expected owner A row intact, got count=%d", ownerARowCount)
	}

	var ownerBRowCount int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND date = ?", ownerB.ID, day).Count(&ownerBRowCount).Error; err != nil {
		t.Fatalf("count owner B rows: %v", err)
	}
	if ownerBRowCount != 0 {
		t.Fatalf("expected owner B's row to be removed, got count=%d", ownerBRowCount)
	}
}

// TestDeleteDayMissingCSRFRejectedByMiddleware closes the security.md
// invariant for the day-delete route: every state-mutating /api/v1/* endpoint
// MUST have a CSRF regression with real middleware enabled, confirming 403
// when the csrf_token form field is missing. The other DeleteDay regressions
// run on a no-CSRF app and only cover handler behavior.
func TestDeleteDayMissingCSRFRejectedByMiddleware(t *testing.T) {
	app, database := newOnboardingTestAppWithCSRF(t)
	user := createOnboardingTestUser(t, database, "delete-day-csrf@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookieWithCSRF(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/2026-02-17", strings.NewReader(url.Values{}.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("expected csrf middleware status 403 for DELETE /api/v1/days/:date without csrf_token, got %d", response.StatusCode)
	}
}

// TestDeleteDayInUTCMinusTimezoneRemovesCanonicalRow is the timezone parity
// counterpart of issue #64 for the delete path. The on-disk row sits at
// UTC-midnight; a DELETE request from a UTC-minus locale must still resolve
// to that row via DayRange's local-calendar-day projection. If the bounds
// drift back a day in UTC-minus zones, the row would survive and the user
// would see a stale entry on the next render.
func TestDeleteDayInUTCMinusTimezoneRemovesCanonicalRow(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "delete-day-tz-minus@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	day := time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC)
	if err := database.Create(&models.DailyLog{
		UserID:   user.ID,
		Date:     day,
		IsPeriod: true,
		Flow:     models.FlowMedium,
	}).Error; err != nil {
		t.Fatalf("seed daily log: %v", err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/v1/days/2026-02-17", nil)
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=America/Toronto"))
	request.Header.Set(timezoneHeaderName, "America/Toronto")
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 204 on TZ-minus delete, got %d", response.StatusCode)
	}

	var rowCount int64
	if err := database.Model(&models.DailyLog{}).Where("user_id = ? AND date = ?", user.ID, day).Count(&rowCount).Error; err != nil {
		t.Fatalf("count remaining rows: %v", err)
	}
	if rowCount != 0 {
		t.Fatalf("expected TZ-minus DELETE to remove the row, %d still present", rowCount)
	}
}
