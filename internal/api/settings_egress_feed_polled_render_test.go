package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// WEB-46: the settings card's last-polled mark.
//
// TestSettingsCardShowsTheLastPolledDateAfterASuccessfulPoll and
// TestSettingsCardOmitsTheLastPolledDateWhenNeverPolled pin the render half —
// present only after a poll actually happened. The three clear-site tests
// below pin that the card cannot go on showing a date for a link the owner
// just revoked, rotated, or wiped: db/user_repository_calendar_feed_last_polled_test.go
// already proves every clearing site at the repository
// layer; these three cover the ones reachable through this package's own
// HTTP handlers, end to end through the real render.

// TestSettingsCardShowsTheLastPolledDateAfterASuccessfulPoll proves the mark
// set by an actual feed poll reaches the rendered settings page, on its own
// element, in the "recorded" wording.
func TestSettingsCardShowsTheLastPolledDateAfterASuccessfulPoll(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-polled-render-shows@example.com")
	token := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	mustServeCalendarFeed(t, ctx.app, token, "before checking the settings card")

	after := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if after.CalendarFeedLastPolledOn == nil {
		t.Fatal("expected the poll to have written the last-polled mark")
	}

	body := fetchPageBody(t, ctx.app, "/settings", ctx.authCookie)
	if !strings.Contains(body, "data-egress-feed-polled-on") {
		t.Fatal("expected the settings card to render the last-polled date after a successful poll")
	}
	if !strings.Contains(body, "data-egress-feed-polled>Last checked by a calendar on <time") {
		t.Fatal("expected the recorded wording, followed by the date, on the data-egress-feed-polled element")
	}
}

// TestSettingsCardOmitsTheLastPolledDateWhenNeverPolled proves the card never
// invents a date: an armed feed nobody has polled yet must not carry the
// polled-on element, only the "none" wording.
func TestSettingsCardOmitsTheLastPolledDateWhenNeverPolled(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-polled-render-omits@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	body := fetchPageBody(t, ctx.app, "/settings", ctx.authCookie)
	if strings.Contains(body, "data-egress-feed-polled-on") {
		t.Fatal("expected no last-polled date element for a feed that was never polled")
	}
	if !strings.Contains(body, "data-egress-feed-polled>No calendar has polled this feed yet.</p>") {
		t.Fatal("expected the none wording, alone, on an unpolled feed's data-egress-feed-polled element")
	}
}

// TestCalendarFeedRevokeClearsTheLastPolledDate proves the mark does not
// outlive the link through the actual revoke handler.
func TestCalendarFeedRevokeClearsTheLastPolledDate(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-polled-clear-revoke@example.com")
	token := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
	mustServeCalendarFeed(t, ctx.app, token, "seeding the mark before revoke")
	if reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedLastPolledOn == nil {
		t.Fatal("the mark did not take: the clearing assertion below would pass vacuously")
	}

	revoke := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/calendar-feed", url.Values{}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, revoke, http.StatusOK)

	if after := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID); after.CalendarFeedLastPolledOn != nil {
		t.Fatalf("revoke left the last-polled mark standing: %v", after.CalendarFeedLastPolledOn)
	}
}

// TestCalendarFeedRotateClearsTheLastPolledDate proves a fresh link starts
// its own mark rather than inheriting the previous link's history.
func TestCalendarFeedRotateClearsTheLastPolledDate(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-polled-clear-rotate@example.com")
	token := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
	mustServeCalendarFeed(t, ctx.app, token, "seeding the mark before rotate")
	if reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedLastPolledOn == nil {
		t.Fatal("the mark did not take: the clearing assertion below would pass vacuously")
	}

	rotate := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate", url.Values{}, nil)
	if rotate.StatusCode != http.StatusSeeOther {
		t.Fatalf("expected 303 on rotate, got %d", rotate.StatusCode)
	}

	if after := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID); after.CalendarFeedLastPolledOn != nil {
		t.Fatalf("rotate left the last-polled mark standing for the fresh link: %v", after.CalendarFeedLastPolledOn)
	}
}

// TestClearDataClearsTheLastPolledDate proves the panic-clear wipes the mark
// along with the rest of the feed, through the real data-wipe handler.
func TestClearDataClearsTheLastPolledDate(t *testing.T) {
	scenario := setupClearDataScenario(t)
	token := armCalendarFeedForUser(t, scenario.database, scenario.user.ID)
	mustServeCalendarFeed(t, scenario.app, token, "seeding the mark before clear-data")

	var seeded struct{ CalendarFeedLastPolledOn *string }
	if err := scenario.database.Raw(`SELECT calendar_feed_last_polled_on FROM users WHERE id = ?`, scenario.user.ID).Scan(&seeded).Error; err != nil {
		t.Fatalf("read seeded mark: %v", err)
	}
	if seeded.CalendarFeedLastPolledOn == nil {
		t.Fatal("the mark did not take: the clearing assertion below would pass vacuously")
	}

	response := settingsFormRequestWithCSRF(t, settingsSecurityTestContext{
		app:        scenario.app,
		authCookie: scenario.authCookie,
		csrfCookie: scenario.csrfCookie,
		csrfToken:  scenario.csrfToken,
	}, http.MethodPost, "/api/v1/users/current/data-wipe", url.Values{
		"password": {"StrongPass1"},
	}, map[string]string{
		"Accept": "application/json",
	})
	assertStatusCode(t, response, http.StatusOK)

	var cleared struct{ CalendarFeedLastPolledOn *string }
	if err := scenario.database.Raw(`SELECT calendar_feed_last_polled_on FROM users WHERE id = ?`, scenario.user.ID).Scan(&cleared).Error; err != nil {
		t.Fatalf("read cleared mark: %v", err)
	}
	if cleared.CalendarFeedLastPolledOn != nil {
		t.Fatalf("clear-data left the last-polled mark standing: %v", *cleared.CalendarFeedLastPolledOn)
	}
}
