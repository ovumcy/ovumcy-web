package api

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

// WEB-125: a December 9999 grid cell whose date ParseDayDate refuses is drawn
// inert — disabled, with no hx-get — so a click cannot request
// /calendar/day/10000-01-01 and fail. Served through GET /calendar at the last
// accepted day, the only today whose navigation horizon reaches that month.
func TestDecember9999CalendarCellsPastDayDateMaxAreInert(t *testing.T) {
	t.Parallel()

	now := time.Date(9999, time.December, 30, 12, 0, 0, 0, time.UTC)
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
	user := createOnboardingTestUserAt(t, database, "calendar-9999@example.com", "StrongPass1", true, now.AddDate(-1, 0, 0))

	request := httptest.NewRequest(http.MethodGet, "/calendar?month=9999-12", nil)
	request.Header.Set("Cookie", issueAuthCookieForUser(t, user))
	response := mustAppResponse(t, app, request)
	html := mustReadBodyString(t, response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /calendar?month=9999-12 = %d:\n%s", response.StatusCode, html)
	}

	for key, wantSelectable := range map[string]bool{
		"9999-12-30":  true,
		"9999-12-31":  false,
		"10000-01-01": false,
	} {
		button := regexp.MustCompile(`<button\s[^>]*data-day="` + regexp.QuoteMeta(key) + `"[^>]*>`).FindString(html)
		if button == "" {
			t.Fatalf("the December 9999 grid rendered no cell for %s", key)
		}
		hasGet := strings.Contains(button, `hx-get="/calendar/day/`+key)
		disabled := regexp.MustCompile(`\sdisabled[\s>]`).MatchString(button)
		if hasGet != wantSelectable || disabled == wantSelectable {
			t.Errorf("%s: hx-get %v, disabled %v, want selectable %v: %s", key, hasGet, disabled, wantSelectable, button)
		}
	}
	if strings.Contains(html, "/calendar/day/10000-") {
		t.Fatal("the page still names a year-10000 day URL")
	}
}
