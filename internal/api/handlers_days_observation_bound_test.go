package api

import (
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
// write — PUT, PATCH and the day form posted without JavaScript — refuses one
// recorded past the bound a cycle start may be marked on (today+2), with the
// cycle-start refusal and its localized copy. The bound is the services
// layer's; these pin that every transport answers it the same way.

const invalidCycleStartDayErrorKey = "invalid cycle start day"

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
		name   string
		method string
		body   string
	}{
		{name: "put period", method: http.MethodPut, body: `{"is_period":true,"flow":"none","symptom_ids":[],"notes":""}`},
		{name: "put pregnancy test", method: http.MethodPut, body: `{"is_period":false,"flow":"none","pregnancy_test":"positive","symptom_ids":[],"notes":""}`},
		{name: "patch period", method: http.MethodPatch, body: `{"is_period":true}`},
		{name: "patch pregnancy test", method: http.MethodPatch, body: `{"pregnancy_test":"negative"}`},
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
			if got := readAPIError(t, refused.Body); got != invalidCycleStartDayErrorKey {
				t.Fatalf("expected the cycle-start refusal %q, got %q", invalidCycleStartDayErrorKey, got)
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

// TestNoJSDayFormRefusesAnObservationPastTheBoundWithTheCycleStartCopy posts
// the calendar day form as a browser without JavaScript does: the refusal is
// the 422 page carrying the localized cycle-start copy.
func TestNoJSDayFormRefusesAnObservationPastTheBoundWithTheCycleStartCopy(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	_, firstRefused := observationBoundDays(now)
	iso := firstRefused.Format("2006-01-02")
	message := englishCopy(t, "dashboard.error.invalid_cycle_start_date")

	for name, typed := range map[string]url.Values{
		"period":         {"is_period": {"true"}},
		"pregnancy test": {"pregnancy_test": {"positive"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := newSettingsSecurityTestContextWithOptions(t, "observation-bound-form-"+strings.ReplaceAll(name, " ", "-")+"@example.com", onboardingTestAppOptions{
				enableCSRF:              true,
				envelopeTransportErrors: true,
				now:                     func() time.Time { return now },
			})
			form := renderNoJSForm(t, ctx.app, "/calendar/day/"+iso+"?mode=edit", authCookieMap(t, ctx.authCookie), formWithFlag("data-day-editor-form"))

			response := form.submit(t, ctx.app, typed)
			assertRefusalPageCarrying(t, response, http.StatusUnprocessableEntity, message, calendarLanding(iso))
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
