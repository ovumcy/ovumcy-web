package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/net/html"
)

// lateCycleNoticeOwnerPages are the two owner pages besides the dashboard that
// carry the late-cycle notice off the shared cycle context.
var lateCycleNoticeOwnerPages = []string{"/calendar", "/stats"}

// seedLateCycleNoticeOwner builds a regular account with three completed
// cycles of 26, 30 and 28 days — reference length 28 (the mean), observed
// maximum 30 — and a running cycle on the given cycle day. Between L+1 and the
// observed maximum the notice can only be the fertility-paused wording: the
// cycle is out of date, not overdue (that needs a day past 35), and not yet
// beyond the owner's own range.
func seedLateCycleNoticeOwner(t *testing.T, email string, cycleDay int) (*fiber.App, string) {
	t.Helper()

	app, database, _ := newOnboardingTestAppWithLocation(t, time.UTC)
	user, authCookie, today := newStatsOverviewOwner(t, app, database, email)
	current := cycleDay - 1
	seedStatsOverviewCycleHistory(t, database, user, today, current+84, current+58, current+28, current)
	updateStatsOverviewUser(t, database, user, map[string]any{
		"last_period_start": services.AddCalendarDays(today, -current, time.UTC),
	})
	return app, authCookie
}

func fetchLateCycleNoticePage(t *testing.T, app *fiber.App, authCookie string, path string) *html.Node {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("Cookie", joinCookieHeader(authCookie, timezoneCookieName+"=UTC"))
	request.Header.Set(timezoneHeaderName, "UTC")

	response := mustAppResponse(t, app, request)
	assertStatusCode(t, response, http.StatusOK)
	return mustParseHTMLDocument(t, mustReadBodyString(t, response.Body))
}

// TestCalendarAndStatsRenderTheLateCycleNoticeFromTheFirstOutOfDateDay: from
// L+1 the fertility half is withheld on every surface, and the withheld value
// is explained in plain words on the calendar and the stats page too — the
// same notice the dashboard renders, under the wording that keeps the
// next-period estimate (still published before the overdue gate) out of what
// it calls paused.
func TestCalendarAndStatsRenderTheLateCycleNoticeFromTheFirstOutOfDateDay(t *testing.T) {
	app, authCookie := seedLateCycleNoticeOwner(t, "late-notice-l-plus-one@example.com", 29)

	for _, path := range lateCycleNoticeOwnerPages {
		document := fetchLateCycleNoticePage(t, app, authCookie, path)
		notice := findHTMLNodeWithAttr(document, "data-late-cycle-key")
		if notice == nil {
			t.Fatalf("%s rendered no late-cycle notice on cycle day 29 of a 28-day reference — the fertility half is withheld from L+1 with nothing saying why", path)
		}
		if got := htmlAttr(notice, "data-late-cycle-key"); got != services.LateCycleFertilityPausedKey {
			t.Fatalf("%s late-cycle key = %q, want %q: out of date, not overdue, inside the observed 30-day maximum", path, got, services.LateCycleFertilityPausedKey)
		}
		if got := htmlAttr(notice, "data-late-cycle-tone"); got != services.LateCycleToneNeutral {
			t.Fatalf("%s late-cycle tone = %q, want %q", path, got, services.LateCycleToneNeutral)
		}
	}
}

// TestCalendarAndStatsRenderNoLateCycleNoticeOnTheReferenceDay is the boundary
// below it: on day L the running cycle has not passed its reference length,
// nothing is withheld, and neither page says the cycle is late.
func TestCalendarAndStatsRenderNoLateCycleNoticeOnTheReferenceDay(t *testing.T) {
	app, authCookie := seedLateCycleNoticeOwner(t, "late-notice-day-l@example.com", 28)

	for _, path := range lateCycleNoticeOwnerPages {
		document := fetchLateCycleNoticePage(t, app, authCookie, path)
		if notice := findHTMLNodeWithAttr(document, "data-late-cycle-key"); notice != nil {
			t.Fatalf("%s rendered the late-cycle notice %q on cycle day 28 of a 28-day reference — the cycle is not late yet", path, htmlAttr(notice, "data-late-cycle-key"))
		}
	}
}
