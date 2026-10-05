package api

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"gorm.io/gorm"
)

// dayFormAccountWireName is the field name as the templates and the client
// send it, spelled out rather than read off the production constant so the
// wire contract is pinned by the test.
const dayFormAccountWireName = "day_form_account"

var dayFormAccountInputPattern = regexp.MustCompile(`name="day_form_account" value="([^"]*)"`)

// renderedDayFormAccount fetches a page that renders a day form and returns
// the account binding its day forms carry. Every day write on the page (the
// day form, the delete form, each cycle-start form) carries the field, and all
// of them must carry the same value.
func renderedDayFormAccount(t *testing.T, app *fiber.App, cookie string, path string) string {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Cookie", cookie)
	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	body := mustReadBodyString(t, response.Body)

	matches := dayFormAccountInputPattern.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatalf("%s: expected a day_form_account field in the day form, found none", path)
	}
	value := html.UnescapeString(matches[0][1])
	if value == "" {
		t.Fatalf("%s: the day form rendered an empty account binding", path)
	}
	for _, match := range matches[1:] {
		if other := html.UnescapeString(match[1]); other != value {
			t.Fatalf("%s: the page's day writes carry different bindings, %q and %q", path, value, other)
		}
	}
	return value
}

// bindDayWriteForTest makes request carry the account binding a page rendered
// for userID would send, in the header the dashboard script uses. A test that
// drives a day write as a browser page does (HTMX, or a form post accepting
// text/html) needs it: such a write without a binding is refused.
func bindDayWriteForTest(t *testing.T, request *http.Request, userID uint) {
	t.Helper()
	binding, err := security.DayFormAccountBinding([]byte(testAppSecretKey), userID)
	if err != nil {
		t.Fatalf("bind day write to account %d: %v", userID, err)
	}
	request.Header.Set("X-Ovumcy-Day-Form-Account", binding)
}

func putDayForm(t *testing.T, app *fiber.App, cookie string, day time.Time, form url.Values, htmx bool) *http.Response {
	t.Helper()

	request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+day.Format("2006-01-02"), strings.NewReader(form.Encode()))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Cookie", cookie)
	if htmx {
		request.Header.Set("HX-Request", "true")
	} else {
		request.Header.Set("Accept", "application/json")
	}
	return mustAppResponse(t, app, request)
}

func seedDayForTest(t *testing.T, database *gorm.DB, userID uint, day time.Time, mood int, notes string) {
	t.Helper()
	seeded := models.DailyLog{UserID: userID, Date: day, Mood: mood, Notes: notes}
	if err := database.Create(&seeded).Error; err != nil {
		t.Fatalf("seed day for user %d: %v", userID, err)
	}
}

func assertStoredDay(t *testing.T, database *gorm.DB, userID uint, day time.Time, mood int, notes string) {
	t.Helper()
	entry, err := fetchLogByDateForTest(database, userID, day, time.UTC)
	if err != nil {
		t.Fatalf("load day for user %d: %v", userID, err)
	}
	if entry.Mood != mood || entry.Notes != notes {
		t.Fatalf("user %d on %s: expected mood=%d notes=%q, got mood=%d notes=%q", userID, day.Format("2006-01-02"), mood, notes, entry.Mood, entry.Notes)
	}
}

// TestDayFormRefusesASaveRetriedUnderAnotherAccount drives the defect the
// account binding closes: a day form rendered for one account, left open while
// a DIFFERENT account signs in in another tab of the same browser, must not
// write its entry into that second account. The browser-wide session cookie
// and the account-agnostic CSRF token both pass, so the binding is the only
// thing that can tell the two accounts apart.
func TestDayFormRefusesASaveRetriedUnderAnotherAccount(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	first := createOnboardingTestUser(t, database, "day-form-account-first@example.com", "StrongPass1", true)
	second := createOnboardingTestUser(t, database, "day-form-account-second@example.com", "StrongPass1", true)
	firstCookie := loginAndExtractAuthCookie(t, app, first.Email, "StrongPass1")
	secondCookie := loginAndExtractAuthCookie(t, app, second.Email, "StrongPass1")

	day := time.Date(2026, time.June, 10, 0, 0, 0, 0, time.UTC)
	firstBinding := renderedDayFormAccount(t, app, firstCookie, "/dashboard")
	if calendarBinding := renderedDayFormAccount(t, app, firstCookie, "/calendar/day/"+day.Format("2006-01-02")+"?mode=edit"); calendarBinding != firstBinding {
		t.Fatalf("both day forms must carry the same binding for one account, dashboard=%q calendar=%q", firstBinding, calendarBinding)
	}
	secondBinding := renderedDayFormAccount(t, app, secondCookie, "/dashboard")
	if firstBinding == secondBinding {
		t.Fatal("two accounts must not render the same binding")
	}
	if firstBinding == strconv.FormatUint(uint64(first.ID), 10) || strings.Contains(firstBinding, first.Email) {
		t.Fatalf("the binding %q must be opaque, never the raw account id or email", firstBinding)
	}

	t.Run("the account that rendered the form saves", func(t *testing.T) {
		saveDay := day.AddDate(0, 0, 1)
		form := url.Values{"flow": {models.FlowNone}, "mood": {"4"}, "notes": {"own entry"}, dayFormAccountWireName: {firstBinding}}
		response := putDayForm(t, app, firstCookie, saveDay, form, false)
		assertStatusCode(t, response, http.StatusOK)
		assertStoredDay(t, database, first.ID, saveDay, 4, "own entry")
	})

	t.Run("another account's session is refused and its day is untouched", func(t *testing.T) {
		seedDayForTest(t, database, second.ID, day, 2, "second account's own note")

		form := url.Values{"flow": {models.FlowNone}, "mood": {"5"}, "notes": {"first account's entry"}, dayFormAccountWireName: {firstBinding}}
		response := putDayForm(t, app, secondCookie, day, form, false)
		assertStatusCode(t, response, http.StatusConflict)
		if got := readAPIError(t, response.Body); got != "day form account changed" {
			t.Fatalf("expected the account-changed refusal, got %q", got)
		}
		assertStoredDay(t, database, second.ID, day, 2, "second account's own note")
		assertStoredDay(t, database, first.ID, day, 0, "")
	})

	t.Run("the HTMX refusal names the localized key the client reads", func(t *testing.T) {
		htmxDay := day.AddDate(0, 0, 2)
		form := url.Values{"flow": {models.FlowNone}, "mood": {"5"}, dayFormAccountWireName: {firstBinding}}
		response := putDayForm(t, app, secondCookie, htmxDay, form, true)
		assertStatusCode(t, response, http.StatusConflict)
		body := mustReadBodyString(t, response.Body)
		if !strings.Contains(body, `data-flash-key="daylog.save_account_changed"`) {
			t.Fatalf("the HTMX refusal must carry the daylog.save_account_changed key, got %q", body)
		}
		if !strings.Contains(html.UnescapeString(body), "You're now signed in to a different account. This entry was not saved.") {
			t.Fatalf("the HTMX refusal must render the localized copy, got %q", body)
		}
		assertStoredDay(t, database, second.ID, htmxDay, 0, "")
	})

	t.Run("a repeated field cannot outvote a mismatched copy", func(t *testing.T) {
		repeatDay := day.AddDate(0, 0, 3)
		form := url.Values{"flow": {models.FlowNone}, "mood": {"5"}, dayFormAccountWireName: {secondBinding, firstBinding}}
		response := putDayForm(t, app, secondCookie, repeatDay, form, false)
		assertStatusCode(t, response, http.StatusConflict)
		assertStoredDay(t, database, second.ID, repeatDay, 0, "")
	})

	t.Run("an empty binding is a mismatch, not an absence", func(t *testing.T) {
		emptyDay := day.AddDate(0, 0, 4)
		form := url.Values{"flow": {models.FlowNone}, "mood": {"5"}, dayFormAccountWireName: {""}}
		response := putDayForm(t, app, secondCookie, emptyDay, form, false)
		assertStatusCode(t, response, http.StatusConflict)
		assertStoredDay(t, database, second.ID, emptyDay, 0, "")
	})

	t.Run("a form without the field keeps its behaviour", func(t *testing.T) {
		absentDay := day.AddDate(0, 0, 5)
		form := url.Values{"flow": {models.FlowNone}, "mood": {"3"}, "notes": {"no binding"}}
		response := putDayForm(t, app, secondCookie, absentDay, form, false)
		assertStatusCode(t, response, http.StatusOK)
		assertStoredDay(t, database, second.ID, absentDay, 3, "no binding")
	})

	t.Run("a JSON client without the field keeps its behaviour", func(t *testing.T) {
		jsonDay := day.AddDate(0, 0, 6)
		body := `{"is_period":false,"flow":"` + models.FlowNone + `","symptom_ids":[],"mood":3,"notes":"json"}`
		request := httptest.NewRequest(http.MethodPut, "/api/v1/days/"+jsonDay.Format("2006-01-02"), strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Cookie", secondCookie)
		response := mustAppResponse(t, app, request)
		assertStatusCode(t, response, http.StatusOK)
		assertStoredDay(t, database, second.ID, jsonDay, 3, "json")
	})
}
