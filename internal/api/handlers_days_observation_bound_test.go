package api

import (
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-246: a period and a pregnancy-test result are observations, so every day
// write — PUT, PATCH, the HTMX day form and the day form posted without
// JavaScript — refuses one recorded past the bound a cycle start may be marked
// on (today+2). A period is refused with the cycle-start refusal and its
// localized copy; a pregnancy-test result with a refusal and copy of its own,
// since the write names no cycle start. The bound and the choice of refusal
// are the services layer's; these pin that every transport answers it the
// same way.

const (
	invalidCycleStartDayErrorKey       = "invalid cycle start day"
	invalidPregnancyTestDayErrorKey    = "invalid pregnancy test day"
	invalidCycleStartDateMessageKey    = "dashboard.error.invalid_cycle_start_date"
	invalidPregnancyTestDateMessageKey = "dashboard.error.invalid_pregnancy_test_date"
)

func observationBoundDays(now time.Time) (time.Time, time.Time) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	return today.AddDate(0, 0, 2), today.AddDate(0, 0, 3)
}

func sendObservationDayWrite(t *testing.T, app *fiber.App, authCookie string, method string, day time.Time, body string) *http.Response {
	t.Helper()
	request := httptest.NewRequest(method, "/api/v1/days/"+day.Format("2006-01-02"), strings.NewReader(body))
	request.Header.Set("Content-Type", fiber.MIMEApplicationJSON)
	request.Header.Set("Accept", fiber.MIMEApplicationJSON)
	request.Header.Set("Cookie", authCookie)
	return mustAppResponse(t, app, request)
}

func TestDayWritesRefuseAnObservationPastTheCycleStartBound(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	clock := func() time.Time { return now }
	lastAccepted, firstRefused := observationBoundDays(now)

	cases := []struct {
		name    string
		method  string
		body    string
		wantKey string
	}{
		{name: "put period", method: http.MethodPut, body: `{"is_period":true,"flow":"none","symptom_ids":[],"notes":""}`, wantKey: invalidCycleStartDayErrorKey},
		{name: "put pregnancy test", method: http.MethodPut, body: `{"is_period":false,"flow":"none","pregnancy_test":"positive","symptom_ids":[],"notes":""}`, wantKey: invalidPregnancyTestDayErrorKey},
		{name: "put period and pregnancy test", method: http.MethodPut, body: `{"is_period":true,"flow":"none","pregnancy_test":"positive","symptom_ids":[],"notes":""}`, wantKey: invalidCycleStartDayErrorKey},
		{name: "patch period", method: http.MethodPatch, body: `{"is_period":true}`, wantKey: invalidCycleStartDayErrorKey},
		{name: "patch pregnancy test", method: http.MethodPatch, body: `{"pregnancy_test":"negative"}`, wantKey: invalidPregnancyTestDayErrorKey},
	}
	for index, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: clock})
			user := createOnboardingTestUser(t, database, "observation-bound-"+string(rune('a'+index))+"@example.com", "StrongPass1", true)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			accepted := sendObservationDayWrite(t, app, authCookie, c.method, lastAccepted, c.body)
			assertStatusCode(t, accepted, http.StatusOK)
			_ = accepted.Body.Close()

			refused := sendObservationDayWrite(t, app, authCookie, c.method, firstRefused, c.body)
			assertStatusCode(t, refused, http.StatusBadRequest)
			if got := readAPIError(t, refused.Body); got != c.wantKey {
				t.Fatalf("expected the refusal %q, got %q", c.wantKey, got)
			}
			entry, err := fetchLogByDateForTest(database, user.ID, firstRefused, time.UTC)
			if err != nil {
				t.Fatalf("load day: %v", err)
			}
			if entry.ID != 0 {
				t.Fatalf("the refused write stored an entry: %+v", entry)
			}
		})
	}
}

func TestDayWriteWithoutAnObservationPassesPastTheBound(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	_, firstRefused := observationBoundDays(now)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
	user := createOnboardingTestUser(t, database, "observation-bound-none@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	response := sendObservationDayWrite(t, app, authCookie, http.MethodPut, firstRefused, `{"is_period":false,"flow":"none","symptom_ids":[],"notes":"plan"}`)
	assertStatusCode(t, response, http.StatusOK)
	_ = response.Body.Close()

	// An entry stored ahead of the bound before it existed stays as stored, and
	// a write that does not name the period edits it.
	stored := firstRefused.AddDate(0, 0, 7)
	if err := database.Create(&models.DailyLog{UserID: user.ID, Date: stored, IsPeriod: true, Flow: models.FlowNone, PregnancyTest: models.PregnancyTestPositive}).Error; err != nil {
		t.Fatalf("seed stored future day: %v", err)
	}
	response = sendObservationDayWrite(t, app, authCookie, http.MethodPatch, stored, `{"notes":"edited"}`)
	assertStatusCode(t, response, http.StatusOK)
	_ = response.Body.Close()
	entry, err := fetchLogByDateForTest(database, user.ID, stored, time.UTC)
	if err != nil {
		t.Fatalf("load day: %v", err)
	}
	if !entry.IsPeriod || entry.PregnancyTest != models.PregnancyTestPositive || entry.Notes != "edited" {
		t.Fatalf("expected the stored period and test kept and the note edited, got %+v", entry)
	}
}

// observationRefusalCases are the day-form refusals past the bound, each with
// the catalogue key whose copy it must carry.
func observationRefusalCases() map[string]struct {
	typed      url.Values
	messageKey string
} {
	return map[string]struct {
		typed      url.Values
		messageKey string
	}{
		"period":         {typed: url.Values{"is_period": {"true"}}, messageKey: invalidCycleStartDateMessageKey},
		"pregnancy test": {typed: url.Values{"pregnancy_test": {"positive"}}, messageKey: invalidPregnancyTestDateMessageKey},
	}
}

// TestHTMXDayFormRefusesAnObservationPastTheBoundWithItsOwnCopy sends the day
// form as the editor does with JavaScript: the refusal is the status fragment,
// and its data-flash-key names the catalogue key of what the write recorded.
func TestHTMXDayFormRefusesAnObservationPastTheBoundWithItsOwnCopy(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	_, firstRefused := observationBoundDays(now)

	for name, c := range observationRefusalCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
			user := createOnboardingTestUser(t, database, "observation-bound-htmx-"+strings.ReplaceAll(name, " ", "-")+"@example.com", "StrongPass1", true)
			authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

			form := url.Values{"flow": {models.FlowNone}}
			for field, values := range c.typed {
				form[field] = values
			}
			request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+firstRefused.Format("2006-01-02"), strings.NewReader(form.Encode()))
			request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			request.Header.Set("HX-Request", "true")
			request.Header.Set("Accept-Language", "en")
			request.Header.Set("Cookie", authCookie)

			response := mustAppResponse(t, app, request)
			assertStatusCode(t, response, http.StatusBadRequest)
			body := mustReadBodyString(t, response.Body)
			root := mustParseHTMLDocument(t, body)
			if htmlFlashByKey(root, c.messageKey) == nil {
				t.Fatalf("expected the HTMX refusal to carry flash key %q, got %s", c.messageKey, body)
			}
			if message := englishCopy(t, c.messageKey); !strings.Contains(body, template.HTMLEscapeString(message)) {
				t.Fatalf("expected the HTMX refusal to carry the copy %q, got %s", message, body)
			}
			entry, err := fetchLogByDateForTest(database, user.ID, firstRefused, time.UTC)
			if err != nil {
				t.Fatalf("load day: %v", err)
			}
			if entry.ID != 0 {
				t.Fatalf("the refused write stored an entry: %+v", entry)
			}
		})
	}
}

// TestNoJSDayFormRefusesAnObservationPastTheBoundWithItsOwnCopy posts the
// calendar day form as a browser without JavaScript does: the refusal is the
// 422 page carrying the localized copy of what the write recorded — the
// cycle-start copy for a period, the pregnancy-test copy for a test result.
func TestNoJSDayFormRefusesAnObservationPastTheBoundWithItsOwnCopy(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	_, firstRefused := observationBoundDays(now)
	iso := firstRefused.Format("2006-01-02")

	for name, c := range observationRefusalCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newSettingsSecurityTestContextWithOptions(t, "observation-bound-form-"+strings.ReplaceAll(name, " ", "-")+"@example.com", onboardingTestAppOptions{
				enableCSRF:              true,
				envelopeTransportErrors: true,
				now:                     func() time.Time { return now },
			})
			form := renderNoJSForm(t, ctx.app, "/calendar/day/"+iso+"?mode=edit", authCookieMap(t, ctx.authCookie), formWithFlag("data-day-editor-form"))

			response := form.submit(t, ctx.app, c.typed)
			assertRefusalPageCarrying(t, response, http.StatusUnprocessableEntity, englishCopy(t, c.messageKey), calendarLanding(iso))
			entry, err := fetchLogByDateForTest(ctx.database, ctx.user.ID, firstRefused, time.UTC)
			if err != nil {
				t.Fatalf("load day: %v", err)
			}
			if entry.ID != 0 {
				t.Fatalf("the refused form stored an entry: %+v", entry)
			}
		})
	}
}
