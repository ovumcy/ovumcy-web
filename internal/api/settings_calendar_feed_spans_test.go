package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// WEB-381: the period-span and fertile-window opt-ins are chosen per link on
// generate/rotate, each off unless its own box is ticked, and the egress ledger
// names only what the stored link sends.

const (
	periodSpanFeedLedgerCopy    = "Estimated periods as multi-day events"
	fertileWindowFeedLedgerCopy = "Estimated fertile windows as multi-day events"
)

func TestCalendarFeedGenerateStoresNoSpanUnlessItsBoxIsTicked(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-spans-default@example.com")

	generated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	assertStatusCode(t, generated, http.StatusSeeOther)
	user := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if user.CalendarFeedPeriodSpan || user.CalendarFeedFertileWindow {
		t.Fatal("a generate without the boxes ticked stored a span opt-in")
	}
	page := renderSettingsPageForTest(t, ctx.app, ctx.authCookie)
	if strings.Contains(page, periodSpanFeedLedgerCopy) || strings.Contains(page, fertileWindowFeedLedgerCopy) {
		t.Fatal("the egress ledger names a span the stored link does not send")
	}
	for _, hook := range []string{
		"data-settings-calendar-feed-period-span", "data-settings-calendar-feed-period-span-warning",
		"data-settings-calendar-feed-fertile-window", "data-settings-calendar-feed-fertile-window-warning",
	} {
		if !strings.Contains(page, hook+">") && !strings.Contains(page, hook+" ") {
			t.Fatalf("the settings card renders no %s", hook)
		}
	}
	if !strings.Contains(page, "not contraception guidance") {
		t.Fatal("the fertile-window choice does not say it is not contraception guidance")
	}

	fertileOnly := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed",
		url.Values{"calendar_feed_fertile_window": {"true"}}, nil)
	assertStatusCode(t, fertileOnly, http.StatusSeeOther)
	user = reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if user.CalendarFeedPeriodSpan || !user.CalendarFeedFertileWindow {
		t.Fatalf("one ticked box stored period_span=%v fertile_window=%v, want false/true", user.CalendarFeedPeriodSpan, user.CalendarFeedFertileWindow)
	}
}

func TestCalendarFeedRotateCarriesTheSpanChoicesOfTheNewLink(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-spans-rotate@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	optIn := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate",
		url.Values{"calendar_feed_period_span": {"true"}, "calendar_feed_fertile_window": {"true"}}, nil)
	assertStatusCode(t, optIn, http.StatusSeeOther)
	user := reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if !user.CalendarFeedPeriodSpan || !user.CalendarFeedFertileWindow {
		t.Fatal("a rotate with both boxes ticked did not store both opt-ins")
	}
	page := renderSettingsPageForTest(t, ctx.app, ctx.authCookie)
	if !strings.Contains(page, periodSpanFeedLedgerCopy) || !strings.Contains(page, fertileWindowFeedLedgerCopy) {
		t.Fatal("the egress ledger does not name the spans the stored link sends")
	}
	for _, prefilled := range []string{
		`id="calendar-feed-period-span-rotate" name="calendar_feed_period_span" value="true" aria-describedby="calendar-feed-period-span-rotate-warning" checked`,
		`id="calendar-feed-fertile-window-rotate" name="calendar_feed_fertile_window" value="true" aria-describedby="calendar-feed-fertile-window-rotate-warning" checked`,
	} {
		if !strings.Contains(page, prefilled) {
			t.Fatalf("the rotate form does not pre-check the stored choice: %s", prefilled)
		}
	}

	optOut := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate", url.Values{}, nil)
	assertStatusCode(t, optOut, http.StatusSeeOther)
	user = reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID)
	if user.CalendarFeedPeriodSpan || user.CalendarFeedFertileWindow {
		t.Fatal("a rotate with the boxes unticked kept the previous link's span opt-ins")
	}
}
