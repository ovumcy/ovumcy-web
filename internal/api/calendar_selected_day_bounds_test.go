package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// WEB-131 item 5: the selected-day aside is drawn only for a SelectedDate the
// service kept. A ?selected= outside the navigable months is blanked there, so
// the page carries no hx-get to /calendar/day/<that date>; an in-range date
// still gets its aside.
func TestCalendarSelectedDayOutsideNavigableMonthsRendersNoAside(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
	user := createOnboardingTestUser(t, database, "calendar-selected-bounds@example.com", "StrongPass1", true)
	cookie := issueAuthCookieForUser(t, user)

	today := now.Format("2006-01-02")
	tests := []struct {
		name      string
		selected  string
		wantAside bool
	}{
		{name: "date past the forward horizon", selected: now.AddDate(10, 0, 0).Format("2006-01-02")},
		{name: "date before the look-back floor", selected: now.AddDate(-10, 0, 0).Format("2006-01-02")},
		{name: "year far past the horizon", selected: "9000-06-15"},
		{name: "year at the accepted minimum", selected: "1900-01-01"},
		{name: "in-range date", selected: today, wantAside: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodGet, "/calendar?selected="+tc.selected, nil)
			request.Header.Set("Cookie", cookie)
			response := mustAppResponse(t, app, request)
			html := mustReadBodyString(t, response.Body)
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET /calendar?selected=%s = %d:\n%s", tc.selected, response.StatusCode, html)
			}

			hasAside := strings.Contains(html, `hx-get="/calendar/day/`+tc.selected)
			if hasAside != tc.wantAside {
				t.Fatalf("selected=%s: day aside hx-get present %v, want %v", tc.selected, hasAside, tc.wantAside)
			}
			if !tc.wantAside && strings.Contains(html, `hx-trigger="load"`) {
				t.Fatalf("selected=%s: the page still loads a day panel on its own", tc.selected)
			}
		})
	}
}
