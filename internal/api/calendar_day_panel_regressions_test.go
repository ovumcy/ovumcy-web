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

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"gorm.io/gorm"
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

// mustMatchCalendarTag returns the single opening tag matching pattern, so an
// assertion about one control's attributes cannot be satisfied by a different
// element elsewhere in the page.
func mustMatchCalendarTag(t *testing.T, rendered string, pattern string, subject string) string {
	t.Helper()

	matches := regexp.MustCompile(pattern).FindAllString(rendered, -1)
	if len(matches) != 1 {
		t.Fatalf("expected exactly one %s matching %s, got %d", subject, pattern, len(matches))
	}
	return matches[0]
}

// The calendar screen exists so a day can be edited, so the day panel's edit
// action is the primary and "Today" is compact navigation beside the month
// arrows. Both halves are pinned structurally: the weight each control
// declares plus the button utility that paints it. Inverted (edit on
// btn-secondary, Today on btn-primary) both assertions fail.
func TestCalendarDayPanelEditActionCarriesThePrimaryWeight(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-edit-weight@example.com", "StrongPass1", true)

	if err := database.Create(&models.DailyLog{
		UserID:   user.ID,
		Date:     time.Date(2026, time.February, 17, 0, 0, 0, 0, time.UTC),
		IsPeriod: true,
		Flow:     models.FlowMedium,
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

	editAction := mustMatchCalendarTag(
		t,
		rendered,
		`<button[^>]*data-day-editor-open="2026-02-17"[^>]*>`,
		"calendar day panel edit action",
	)
	if !strings.Contains(editAction, `data-action-weight="primary"`) {
		t.Fatalf("expected the day panel edit action to declare the primary weight, got %q", editAction)
	}
	if !strings.Contains(editAction, `class="btn-primary"`) {
		t.Fatalf("expected the day panel edit action to carry the primary fill, got %q", editAction)
	}
}

func TestCalendarTodayControlCarriesTheCompactSecondaryWeight(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-today-weight@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", authCookie)

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	rendered := mustReadBodyString(t, response.Body)

	// Named first so an inverted hierarchy reports the control that took the
	// primary fill, not merely the absence of the navigation hook below.
	if regexp.MustCompile(`<a[^>]*href="/calendar"[^>]*class="btn-primary"`).MatchString(rendered) {
		t.Fatalf("expected no primary-filled month-navigation link on the calendar page")
	}

	todayControl := mustMatchCalendarTag(
		t,
		rendered,
		`<a[^>]*data-calendar-today[^>]*>`,
		"calendar today control",
	)
	if !strings.Contains(todayControl, `data-action-weight="secondary"`) {
		t.Fatalf("expected the today control to declare the secondary weight, got %q", todayControl)
	}
	if !strings.Contains(todayControl, "btn-secondary") || !strings.Contains(todayControl, "btn-compact") {
		t.Fatalf("expected the today control to render as compact secondary navigation, got %q", todayControl)
	}
	if strings.Contains(todayControl, "btn-primary") {
		t.Fatalf("expected the today control to give up the primary fill, got %q", todayControl)
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

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar day panel request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

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

	panelResponse, err := app.Test(panelRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar day panel request failed: %v", err)
	}
	defer func() { _ = panelResponse.Body.Close() }()

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

	saveResponse, err := app.Test(saveRequest, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("save request failed: %v", err)
	}
	defer func() { _ = saveResponse.Body.Close() }()

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

// Period day and cycle start are one event for the person logging it, so the
// day editor asks the question inline beside the period toggle instead of
// sending the owner to the separate manual control. The hook must appear
// exactly in the state the cycle-start policy suggests a new cycle in: seeded
// here with an explicit cycle start 28 days back (suggestion state) against a
// second owner whose previous start is 4 days back (not the suggestion state).
func seedCycleStartAnchorForDayEditor(t *testing.T, database *gorm.DB, userID uint, daysBack int) time.Time {
	t.Helper()

	today := services.DateAtLocation(time.Now().In(time.UTC), time.UTC)
	if err := database.Create(&models.DailyLog{
		UserID:     userID,
		Date:       today.AddDate(0, 0, -daysBack),
		IsPeriod:   true,
		Flow:       models.FlowMedium,
		CycleStart: true,
	}).Error; err != nil {
		t.Fatalf("seed cycle start anchor: %v", err)
	}
	return today
}

func fetchDayEditorMarkup(t *testing.T, app *fiber.App, authCookie string, day time.Time) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/calendar/day/"+day.Format("2006-01-02")+"?mode=edit", nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustReadBodyString(t, response.Body)
}

func TestDayEditorAsksTheCycleStartQuestionExactlyInTheSuggestionState(t *testing.T) {
	app, database := newOnboardingTestApp(t)

	asked := createOnboardingTestUser(t, database, "day-editor-cycle-start-question@example.com", "StrongPass1", true)
	today := seedCycleStartAnchorForDayEditor(t, database, asked.ID, 28)
	askedMarkup := fetchDayEditorMarkup(t, app, loginAndExtractAuthCookie(t, app, asked.Email, "StrongPass1"), today)

	if got := strings.Count(askedMarkup, "data-cycle-start-question"); got != 1 {
		t.Fatalf("expected exactly one inline cycle-start question in the suggestion state, got %d", got)
	}
	if !strings.Contains(askedMarkup, `name="cycle_start"`) {
		t.Fatalf("expected the inline question to ride the day form as a cycle_start control")
	}
	if !strings.Contains(askedMarkup, `data-cycle-start-answer="no"`) {
		t.Fatalf("expected the inline question to offer declining as its own control")
	}

	quiet := createOnboardingTestUser(t, database, "day-editor-cycle-start-quiet@example.com", "StrongPass1", true)
	seedCycleStartAnchorForDayEditor(t, database, quiet.ID, 4)
	quietMarkup := fetchDayEditorMarkup(t, app, loginAndExtractAuthCookie(t, app, quiet.Email, "StrongPass1"), today)

	// Positive anchor first: the form itself renders, so the absence below is
	// the policy staying quiet rather than a panel that failed to load.
	if !strings.Contains(quietMarkup, "data-period-toggle") {
		t.Fatalf("expected the day editor form to render for the second owner")
	}
	if strings.Contains(quietMarkup, "data-cycle-start-question") {
		t.Fatalf("expected no inline cycle-start question four days after the previous start")
	}
}

// The answer is carried by the save that records the bleeding, and only by an
// explicit yes: the same form without the field leaves a plain period day.
func TestDayEditorSaveCarriesTheInlineCycleStartAnswer(t *testing.T) {
	app, database := newOnboardingTestApp(t)

	saveDay := func(t *testing.T, email string, form url.Values) models.DailyLog {
		t.Helper()

		user := createOnboardingTestUser(t, database, email, "StrongPass1", true)
		today := seedCycleStartAnchorForDayEditor(t, database, user.ID, 28)
		authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

		request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+today.Format("2006-01-02"), strings.NewReader(form.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("HX-Request", "true")
		request.Header.Set("Accept-Language", "en")
		request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
		request.Header.Set(timezoneHeaderName, "UTC")

		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)

		var saved models.DailyLog
		if err := database.Where("user_id = ? AND date = ?", user.ID, today).First(&saved).Error; err != nil {
			t.Fatalf("load saved day: %v", err)
		}
		return saved
	}

	confirmed := saveDay(t, "day-save-cycle-start-yes@example.com", url.Values{
		"is_period":   {"true"},
		"flow":        {models.FlowMedium},
		"cycle_start": {"true"},
	})
	if !confirmed.CycleStart {
		t.Fatalf("expected the confirmed inline answer to mark the saved day as a cycle start")
	}
	if !confirmed.IsPeriod {
		t.Fatalf("expected the confirmed day to stay a period day")
	}

	untouched := saveDay(t, "day-save-cycle-start-untouched@example.com", url.Values{
		"is_period": {"true"},
		"flow":      {models.FlowMedium},
	})
	if untouched.CycleStart {
		t.Fatalf("expected an untouched inline question to write no cycle start")
	}

	declined := saveDay(t, "day-save-cycle-start-no@example.com", url.Values{
		"is_period":   {"true"},
		"flow":        {models.FlowMedium},
		"cycle_start": {"false"},
	})
	if declined.CycleStart {
		t.Fatalf("expected declining to leave a plain period day")
	}
	if !declined.IsPeriod {
		t.Fatalf("expected declining to keep the period day itself")
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
