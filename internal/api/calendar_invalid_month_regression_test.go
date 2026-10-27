package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// WEB-230: an unreadable ?month= renders the current month with a notice that
// says so, where it used to redirect to /calendar without a word.
func TestCalendarInvalidMonthHTMLExplainsAndRendersCurrentMonth(t *testing.T) {
	now := time.Now().UTC()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
	user := createOnboardingTestUser(t, database, "calendar-invalid-month@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=9999-99", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if location := response.Header.Get("Location"); location != "" {
		t.Fatalf("expected no redirect, got Location %q", location)
	}
	html := mustReadBodyString(t, response.Body)
	if !strings.Contains(html, `data-calendar-month-invalid`) {
		t.Fatalf("expected the invalid-month notice, got:\n%s", html)
	}
	if !strings.Contains(html, `hx-get="/calendar?month=`+now.Format("2006-01")+`"`) {
		t.Fatalf("expected the current month's grid, got:\n%s", html)
	}
}

func TestCalendarInvalidMonthHTMXExplainsAndRendersCurrentMonth(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-invalid-month-htmx@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=0000-00", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("HX-Request", "true")
	// The grid refresh is an HTMX read from inside the calendar page, which
	// names the account it was rendered for on every request.
	bindDayWriteForTest(t, request, user.ID)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar htmx request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}
	if redirect := response.Header.Get("HX-Redirect"); redirect != "" {
		t.Fatalf("expected no HX-Redirect, got %q", redirect)
	}
	if html := mustReadBodyString(t, response.Body); !strings.Contains(html, `data-calendar-month-invalid`) {
		t.Fatalf("expected the invalid-month notice, got:\n%s", html)
	}
}

// TestCalendarFarFutureMonthRendersClampedAndBounded is the WEB-14 SEC-H5
// sibling of the invalid-month cases above: ?month=9999-12 is a syntactically
// VALID month (unlike "9999-99"), so it must render 200, not redirect — but
// the rendered month is clamped to services.CalendarMaximumNavigableMonth
// (three years out), never the requested far-future value, and the "next"
// month link is disabled there exactly as "prev" already is at the lower
// bound. This is the render-bounded evidence for the cost fix; the iteration
// cap itself is pinned at the service layer
// (TestAppendPredictedCyclesCapsIterationCountRegardlessOfGridEnd).
func TestCalendarFarFutureMonthRendersClampedAndBounded(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-far-future-month@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	expectedMonth := services.CalendarMaximumNavigableMonth(time.Now().UTC(), time.UTC).Format("2006-01")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=9999-12", nil)
	request.Header.Set("Cookie", authCookie)

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", response.StatusCode)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	html := string(body)

	if !strings.Contains(html, `hx-get="/calendar?month=`+expectedMonth+`"`) {
		t.Fatalf("expected the clamped month %q in the grid-refresh hx-get target, got body:\n%s", expectedMonth, html)
	}
	// Scoped to the page's own content, not the whole document: the shared
	// layout's footer echoes the request's own path verbatim into the
	// `/privacy?back=…` link on every page (handlers_not_found.go documents why
	// that substitution is owed only on the 404 page, whose path is
	// caller-chosen — the calendar route's query is its own, already the
	// visitor's, and clamped again identically if followed back). What must not
	// happen is the far-future value driving the calendar content itself: the
	// month label, the grid, and the nav links this test otherwise checks.
	mainStart := strings.Index(html, `<main id="main-content"`)
	mainEnd := strings.Index(html, "</main>")
	if mainStart < 0 || mainEnd < 0 || mainEnd < mainStart {
		t.Fatalf("could not find the page's <main> content to scope the far-future check, got body:\n%s", html)
	}
	if mainContent := html[mainStart:mainEnd]; strings.Contains(mainContent, "9999-12") {
		t.Fatalf("the requested far-future month must not reach the rendered calendar content, got:\n%s", mainContent)
	}
	if !strings.Contains(html, `data-calendar-today`) {
		t.Fatalf("expected the calendar nav to render, got body:\n%s", html)
	}
	// The "next" link is disabled at the upper bound, mirroring "prev" at the
	// lower bound (TestCalendarMinimumNavigableMonth's disabled-prev case).
	if !strings.Contains(html, `aria-disabled="true"`) {
		t.Fatalf("expected the next-month link to render disabled at the upper bound, got body:\n%s", html)
	}
}

func TestCalendarInvalidMonthJSONKeepsValidationError(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-invalid-month-json@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=not-a-month", nil)
	request.Header.Set("Cookie", authCookie)
	request.Header.Set("Accept", "application/json")

	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("calendar json request failed: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", response.StatusCode)
	}
	if got := readAPIError(t, response.Body); got != "invalid month" {
		t.Fatalf("expected invalid month error, got %q", got)
	}
}
