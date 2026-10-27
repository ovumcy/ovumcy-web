package api

import (
	"strings"
	"testing"
	"time"
)

// Month navigation keeps the selected day open (WEB-265): Prev and Next carry
// ?day=, and Today carries it when a day other than today is selected. A bare
// /calendar selects today, so Today stays bare when today or nothing else is
// selected. 56-calendar.js rebuilds the same links client-side; its unit test
// pins that half.
func TestCalendarMonthNavigationCarriesTheSelectedDay(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-nav-keeps-day@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")

	now := time.Now().UTC()
	thisMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	month := func(offset int) string { return thisMonth.AddDate(0, offset, 0).Format("2006-01") }
	today := now.Format("2006-01-02")
	lastMonthFirst := thisMonth.AddDate(0, -1, 0).Format("2006-01-02")

	for _, tc := range []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "another day selected",
			query: "?month=" + month(0) + "&day=" + lastMonthFirst,
			want: []string{
				`href="/calendar?month=` + month(-1) + `&amp;day=` + lastMonthFirst + `"`,
				`href="/calendar?month=` + month(1) + `&amp;day=` + lastMonthFirst + `"`,
				`href="/calendar?month=` + month(0) + `&amp;day=` + lastMonthFirst + `" class="btn-secondary btn-compact" data-calendar-today`,
			},
		},
		{
			name:  "today selected by default",
			query: "",
			want: []string{
				`href="/calendar?month=` + month(-1) + `&amp;day=` + today + `"`,
				`href="/calendar" class="btn-secondary btn-compact" data-calendar-today`,
			},
		},
		{
			name:  "nothing selected",
			query: "?month=" + month(0),
			want: []string{
				`href="/calendar?month=` + month(-1) + `" `,
				`href="/calendar?month=` + month(1) + `" `,
				`href="/calendar" class="btn-secondary btn-compact" data-calendar-today`,
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := fetchPageBody(t, app, "/calendar"+tc.query, authCookie)
			for _, fragment := range tc.want {
				if !strings.Contains(body, fragment) {
					t.Errorf("expected %s in the month navigation", fragment)
				}
			}
		})
	}
}

// A ?day= link that asked for the editor names that mode on the autoload, so
// the script can bring the panel into view (WEB-228); a plain ?day= does not.
func TestCalendarEditModeAutoloadIsMarked(t *testing.T) {
	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-edit-autoload@example.com", "StrongPass1", true)
	authCookie := loginAndExtractAuthCookie(t, app, user.Email, "StrongPass1")
	day := time.Now().UTC().Format("2006-01-02")

	if body := fetchPageBody(t, app, "/calendar?day="+day+"&edit=1", authCookie); !strings.Contains(body, `data-day-editor-autoload="edit"`) {
		t.Fatal(`expected data-day-editor-autoload="edit" for an edit-mode link`)
	}
	if body := fetchPageBody(t, app, "/calendar?day="+day, authCookie); !strings.Contains(body, `data-day-editor-autoload=""`) {
		t.Fatal(`expected an unmarked data-day-editor-autoload for a plain ?day= link`)
	}
}
