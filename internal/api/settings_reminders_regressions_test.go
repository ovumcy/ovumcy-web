package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// remindersJSONRequestWithCSRF posts a raw JSON body carrying the auth + csrf
// cookies and the CSRF token in the X-CSRF-Token header (a JSON body has no
// csrf_token form field, so the header is the token source the extractor uses).
func remindersJSONRequestWithCSRF(t *testing.T, ctx settingsSecurityTestContext, body string, headers map[string]string) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodPatch, "/api/v1/users/current/reminders", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", ctx.csrfToken)
	request.Header.Set("Cookie", settingsCookieHeader(ctx.authCookie, ctx.csrfCookie))
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	response, err := ctx.app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("reminders json request failed: %v", err)
	}
	return response
}

// TestSettingsRemindersUpdatePersistsWithHTMXAndCSRF drives the standalone
// reminder-lead-days control end to end through the real app (CSRF middleware
// on) and asserts the value persists to the session owner's row and the HTMX
// response carries success status markup.
func TestSettingsRemindersUpdatePersistsWithHTMXAndCSRF(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-reminders-persist@example.com")

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/reminders", url.Values{
		"reminder_lead_days": {"7"},
	}, map[string]string{"HX-Request": "true", "Accept-Language": "en"})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)
	if htmlElementByTagAndClass(mustParseHTMLDocument(t, mustReadBodyString(t, response.Body)), "div", "status-ok") == nil {
		t.Fatal("expected htmx success status markup for reminder update")
	}

	persisted := models.User{}
	if err := ctx.database.Select("reminder_lead_days").First(&persisted, ctx.user.ID).Error; err != nil {
		t.Fatalf("load persisted reminder_lead_days: %v", err)
	}
	if persisted.ReminderLeadDays != 7 {
		t.Fatalf("expected persisted reminder_lead_days=7, got %d", persisted.ReminderLeadDays)
	}
}

// TestSettingsRemindersUpdateClampsOutOfRange proves the endpoint clamps into
// the shared 0–14 bound rather than rejecting, for both the low and high edges.
func TestSettingsRemindersUpdateClampsOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		sent string
		want int
	}{
		{name: "above max clamps to 14", sent: "40", want: 14},
		{name: "negative clamps to 0", sent: "-3", want: 0},
		{name: "min boundary", sent: "0", want: 0},
		{name: "max boundary", sent: "14", want: 14},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := newSettingsSecurityTestContext(t, "settings-reminders-clamp-"+tc.sent+"@example.com")

			response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/reminders", url.Values{
				"reminder_lead_days": {tc.sent},
			}, map[string]string{"HX-Request": "true", "Accept-Language": "en"})
			defer func() { _ = response.Body.Close() }()

			assertStatusCode(t, response, http.StatusOK)

			persisted := models.User{}
			if err := ctx.database.Select("reminder_lead_days").First(&persisted, ctx.user.ID).Error; err != nil {
				t.Fatalf("load persisted reminder_lead_days: %v", err)
			}
			if persisted.ReminderLeadDays != tc.want {
				t.Fatalf("expected clamped reminder_lead_days=%d for input %q, got %d", tc.want, tc.sent, persisted.ReminderLeadDays)
			}
		})
	}
}

// TestSettingsRemindersUpdateRejectsMalformedValue asserts a non-integer body is
// a 400 validation error (not a clamp or a 500), exercising the parse-error
// branch of the handler.
func TestSettingsRemindersUpdateRejectsMalformedValue(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-reminders-bad@example.com")

	// JSON/non-HTMX path so the validation error surfaces as a real 400 status
	// (the HTMX path swaps a status-error fragment with a 200, by design).
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/reminders", url.Values{
		"reminder_lead_days": {"not-a-number"},
	}, map[string]string{"Accept": "application/json"})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusBadRequest)
}

// TestSettingsRemindersUpdateScopedToSessionOwner proves the write targets the
// authenticated session's user_id only: a second independent owner's row is
// never touched (household multi-owner isolation).
func TestSettingsRemindersUpdateScopedToSessionOwner(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-reminders-owner@example.com")

	other := createOnboardingTestUser(t, ctx.database, "settings-reminders-bystander@example.com", "StrongPass1", true)
	if err := ctx.database.Model(&models.User{}).Where("id = ?", other.ID).Update("reminder_lead_days", 2).Error; err != nil {
		t.Fatalf("seed bystander reminder_lead_days: %v", err)
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/reminders", url.Values{
		"reminder_lead_days": {"9"},
	}, map[string]string{"HX-Request": "true", "Accept-Language": "en"})
	defer func() { _ = response.Body.Close() }()
	assertStatusCode(t, response, http.StatusOK)

	sessionOwner := models.User{}
	if err := ctx.database.Select("reminder_lead_days").First(&sessionOwner, ctx.user.ID).Error; err != nil {
		t.Fatalf("load session owner reminder_lead_days: %v", err)
	}
	if sessionOwner.ReminderLeadDays != 9 {
		t.Fatalf("expected session owner reminder_lead_days=9, got %d", sessionOwner.ReminderLeadDays)
	}

	bystander := models.User{}
	if err := ctx.database.Select("reminder_lead_days").First(&bystander, other.ID).Error; err != nil {
		t.Fatalf("load bystander reminder_lead_days: %v", err)
	}
	if bystander.ReminderLeadDays != 2 {
		t.Fatalf("expected bystander reminder_lead_days untouched at 2, got %d", bystander.ReminderLeadDays)
	}
}

// TestSettingsPageRendersReminderLeadDaysControl pins the render-path contract
// for the new control: GET /settings exposes the reminder input hydrated from
// the persisted value (the view-flag regression discipline — assert the DOM
// hook + value, not copy).
func TestSettingsPageRendersReminderLeadDaysControl(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "settings-reminders-render@example.com", "StrongPass1", true)
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Update("reminder_lead_days", 8).Error; err != nil {
		t.Fatalf("set reminder_lead_days: %v", err)
	}
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	rendered := renderSettingsPageForTest(t, app, authCookie)
	document := mustParseHTMLDocument(t, rendered)

	input := htmlElementByID(document, "settings-reminder-lead-days")
	if input == nil {
		t.Fatal("expected reminder lead-days input to be rendered")
	}
	if got := htmlAttr(input, "name"); got != "reminder_lead_days" {
		t.Fatalf("expected input name=reminder_lead_days, got %q", got)
	}
	if got := htmlAttr(input, "value"); got != "8" {
		t.Fatalf("expected reminder lead-days input hydrated with value=8, got %q", got)
	}
	if got := htmlAttr(input, "max"); got != "14" {
		t.Fatalf("expected reminder lead-days input max=14, got %q", got)
	}
	if got := htmlAttr(input, "min"); got != "0" {
		t.Fatalf("expected reminder lead-days input min=0, got %q", got)
	}
}

// TestSettingsRemindersUpdateAcceptsJSONBody covers the JSON body-binding path
// (Content-Type: application/json) end to end, including the JSON success
// response block that echoes the clamped value.
func TestSettingsRemindersUpdateAcceptsJSONBody(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-reminders-json@example.com")

	response := remindersJSONRequestWithCSRF(t, ctx, `{"reminder_lead_days":6}`, map[string]string{
		"Accept": "application/json",
	})
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusOK)

	persisted := models.User{}
	if err := ctx.database.Select("reminder_lead_days").First(&persisted, ctx.user.ID).Error; err != nil {
		t.Fatalf("load persisted reminder_lead_days: %v", err)
	}
	if persisted.ReminderLeadDays != 6 {
		t.Fatalf("expected JSON-body reminder_lead_days=6 to persist, got %d", persisted.ReminderLeadDays)
	}
}

// TestSettingsRemindersUpdateBrowserRedirectsWithFlash covers the non-HTMX,
// non-JSON browser submit path: it persists, sets the settings-success flash
// cookie, and redirects (303) to /settings.
func TestSettingsRemindersUpdateBrowserRedirectsWithFlash(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "settings-reminders-redirect@example.com")

	// No HX-Request and no Accept: application/json -> the default browser
	// redirect branch (redirectOrJSON => 303 to /settings + flash cookie).
	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPatch, "/api/v1/users/current/reminders", url.Values{
		"reminder_lead_days": {"5"},
	}, nil)
	defer func() { _ = response.Body.Close() }()

	assertStatusCode(t, response, http.StatusSeeOther)
	if location := response.Header.Get("Location"); location != "/settings" {
		t.Fatalf("expected redirect to /settings, got %q", location)
	}
	if flashValue := responseCookieValue(response.Cookies(), flashCookieName); flashValue == "" {
		t.Fatal("expected settings-success flash cookie on the browser redirect path")
	}

	persisted := models.User{}
	if err := ctx.database.Select("reminder_lead_days").First(&persisted, ctx.user.ID).Error; err != nil {
		t.Fatalf("load persisted reminder_lead_days: %v", err)
	}
	if persisted.ReminderLeadDays != 5 {
		t.Fatalf("expected browser-path reminder_lead_days=5 to persist, got %d", persisted.ReminderLeadDays)
	}
}
