package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-230: the calendar's lower bound reaches the owner's earliest stored
// entry, so a month the export, the API and the day panel all serve is not
// clamped away on the calendar. The account is created now, so the old bound
// (creation − 3 years) lies years after the stored entry.
func TestCalendarReachesTheEarliestStoredEntry(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-history-earliest@example.com", "StrongPass1", true)
	stored := time.Date(2015, time.June, 10, 0, 0, 0, 0, time.UTC)
	if err := database.Create(&models.DailyLog{UserID: user.ID, Date: stored, Flow: models.FlowLight, IsPeriod: true}).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}
	cookie := issueAuthCookieForUser(t, user)

	t.Run("the stored month renders with its day kept", func(t *testing.T) {
		html := mustGetCalendarHTML(t, app, cookie, "/calendar?month=2015-06&day=2015-06-10")
		if !strings.Contains(html, `hx-get="/calendar?month=2015-06"`) {
			t.Fatalf("expected the grid for 2015-06, got:\n%s", html)
		}
		if !strings.Contains(html, `hx-get="/calendar/day/2015-06-10`) {
			t.Fatalf("expected the ?day= panel for 2015-06-10 to survive, got:\n%s", html)
		}
		if !strings.Contains(html, `href="/calendar?month=2015-07&day=2015-06-10"`) {
			t.Fatalf("expected the next link to carry ?day=, got:\n%s", html)
		}
	})

	t.Run("the month before the earliest entry clamps to it and explains the limit", func(t *testing.T) {
		html := mustGetCalendarHTML(t, app, cookie, "/calendar?month=2010-01")
		if !strings.Contains(html, `hx-get="/calendar?month=2015-06"`) {
			t.Fatalf("expected the clamp to land on the earliest stored month, got:\n%s", html)
		}
		if !strings.Contains(html, `data-calendar-history-limit`) {
			t.Fatalf("expected the history-limit hint beside the disabled prev, got:\n%s", html)
		}
		if !strings.Contains(html, `role="link" aria-disabled="true" aria-describedby="calendar-history-limit"`) {
			t.Fatalf("expected the disabled prev to be described by the hint, got:\n%s", html)
		}
	})

	t.Run("a month with an earlier one navigable offers prev and no hint", func(t *testing.T) {
		html := mustGetCalendarHTML(t, app, cookie, "/calendar?month=2015-07")
		if !strings.Contains(html, `href="/calendar?month=2015-06"`) {
			t.Fatalf("expected a prev link to 2015-06, got:\n%s", html)
		}
		if strings.Contains(html, `data-calendar-history-limit`) {
			t.Fatalf("the history-limit hint must render only where prev is disabled, got:\n%s", html)
		}
	})
}

// Another owner's older entry must not move this owner's bound: the earliest
// stored day is read for the session's own user only.
func TestCalendarHistoryBoundIgnoresAnotherOwnersEntries(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-history-owner@example.com", "StrongPass1", true)
	other := createOnboardingTestUser(t, database, "calendar-history-other@example.com", "StrongPass1", true)
	if err := database.Create(&models.DailyLog{UserID: other.ID, Date: time.Date(2012, time.March, 3, 0, 0, 0, 0, time.UTC), Flow: models.FlowNone}).Error; err != nil {
		t.Fatalf("create other owner's log: %v", err)
	}
	cookie := issueAuthCookieForUser(t, user)

	html := mustGetCalendarHTML(t, app, cookie, "/calendar?month=2012-03")
	if strings.Contains(html, `hx-get="/calendar?month=2012-03"`) {
		t.Fatalf("another owner's entry opened 2012-03 on this owner's calendar:\n%s", html)
	}
	if !strings.Contains(html, `data-calendar-history-limit`) {
		t.Fatalf("expected the clamp to this owner's own bound, with the hint, got:\n%s", html)
	}
}

// An unreadable ?month= is explained on the page instead of a silent 303, and
// ?day= survives it.
func TestCalendarInvalidMonthRendersTheNoticeAndKeepsTheDay(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	app, database := newOnboardingTestAppWithOptions(t, onboardingTestAppOptions{now: func() time.Time { return now }})
	user := createOnboardingTestUser(t, database, "calendar-invalid-month-day@example.com", "StrongPass1", true)
	cookie := issueAuthCookieForUser(t, user)
	day := now.AddDate(0, -1, 0).Format("2006-01-02")

	html := mustGetCalendarHTML(t, app, cookie, "/calendar?month=abc&day="+day)
	if !strings.Contains(html, `data-calendar-month-invalid`) {
		t.Fatalf("expected the invalid-month notice, got:\n%s", html)
	}
	if !strings.Contains(html, `hx-get="/calendar/day/`+day) {
		t.Fatalf("expected ?day=%s to survive the invalid month, got:\n%s", day, html)
	}
	if !strings.Contains(html, `hx-get="/calendar?month=`+day[:7]+`"`) {
		t.Fatalf("expected the grid to open on the day's month %s, got:\n%s", day[:7], html)
	}
}

// A failed earliest-entry read fails the page rather than falling back to an
// unbounded or creation-only range.
func TestCalendarFailsWhenTheHistoryBoundCannotBeRead(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-history-read-failure@example.com", "StrongPass1", true)
	cookie := issueAuthCookieForUser(t, user)
	if err := database.Migrator().DropTable(&models.DailyLog{}); err != nil {
		t.Fatalf("drop daily logs: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/calendar", nil)
	request.Header.Set("Cookie", cookie)
	response := mustAppResponse(t, app, request)
	if response.StatusCode != http.StatusInternalServerError {
		t.Fatalf("GET /calendar with an unreadable history = %d, want 500", response.StatusCode)
	}
}

// WEB-345: the disabled Next stand-in keeps the role its Prev twin carries, so
// a screen reader announces a disabled link rather than bare text at the upper
// bound too.
func TestCalendarDisabledNextIsADisabledLink(t *testing.T) {
	t.Parallel()

	app, database := newOnboardingTestApp(t)
	user := createOnboardingTestUser(t, database, "calendar-disabled-next@example.com", "StrongPass1", true)
	cookie := issueAuthCookieForUser(t, user)

	html := mustGetCalendarHTML(t, app, cookie, "/calendar?month=9999-12")
	end := strings.Index(html, ">Next</span>")
	if end < 0 {
		t.Fatalf("expected the Next control as a disabled span at the upper bound, got:\n%s", html)
	}
	tag := html[strings.LastIndex(html[:end], "<span"):end]
	for _, want := range []string{`role="link"`, `aria-disabled="true"`} {
		if !strings.Contains(tag, want) {
			t.Fatalf("the disabled Next span lacks %s: %s>", want, tag)
		}
	}
}

func mustGetCalendarHTML(t *testing.T, app *fiber.App, cookie string, target string) string {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.Header.Set("Cookie", cookie)
	response := mustAppResponse(t, app, request)
	html := mustReadBodyString(t, response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d:\n%s", target, response.StatusCode, html)
	}
	return html
}
