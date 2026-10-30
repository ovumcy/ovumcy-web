package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-379: the detailed-titles opt-in is chosen per link on generate/rotate,
// off unless the box is ticked, and the egress ledger names the mode the stored
// link actually sends. Cited by SECURITY.md's Calendar Feed rows.

const (
	neutralFeedLedgerCopy  = "A fixed, neutral event title"
	detailedFeedLedgerCopy = "An event title naming the estimate type: period or ovulation"
)

func TestCalendarFeedGenerateIsNeutralUnlessTheOwnerOptsIn(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-titles-default@example.com")

	generated := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	assertStatusCode(t, generated, http.StatusSeeOther)
	if reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedDetailedTitles {
		t.Fatal("a generate without the box ticked stored the detailed-titles opt-in")
	}

	page := renderSettingsPageForTest(t, ctx.app, ctx.authCookie)
	if !strings.Contains(page, neutralFeedLedgerCopy) || strings.Contains(page, detailedFeedLedgerCopy) {
		t.Fatal("the egress ledger does not describe the neutral title of a neutral link")
	}
	for _, hook := range []string{"data-settings-calendar-feed-titles", "data-settings-calendar-feed-titles-warning"} {
		if !strings.Contains(page, hook+">") && !strings.Contains(page, hook+" ") {
			t.Fatalf("the settings card renders no %s", hook)
		}
	}
	if !strings.Contains(page, "will see them") {
		t.Fatal("the settings card does not warn who sees detailed titles")
	}
}

func TestCalendarFeedRotateCarriesTheChoiceOfTheNewLink(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-titles-rotate@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	optIn := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate",
		url.Values{"calendar_feed_detailed_titles": {"true"}}, nil)
	assertStatusCode(t, optIn, http.StatusSeeOther)
	if !reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedDetailedTitles {
		t.Fatal("a rotate with the box ticked did not store the opt-in")
	}
	page := renderSettingsPageForTest(t, ctx.app, ctx.authCookie)
	if !strings.Contains(page, detailedFeedLedgerCopy) || strings.Contains(page, neutralFeedLedgerCopy) {
		t.Fatal("the egress ledger does not describe the detailed titles the stored link sends")
	}
	if !strings.Contains(page, `id="calendar-feed-detailed-titles-rotate" name="calendar_feed_detailed_titles" value="true" aria-describedby="calendar-feed-detailed-titles-rotate-warning" checked`) {
		t.Fatal("the rotate form does not pre-check the stored choice")
	}

	optOut := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate", url.Values{}, nil)
	assertStatusCode(t, optOut, http.StatusSeeOther)
	if reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedDetailedTitles {
		t.Fatal("a rotate with the box unticked kept the previous link's opt-in")
	}
}

func TestCalendarFeedServesDetailedTitlesOnlyToAnOptedInLink(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-titles-serve@example.com")
	token := armCalendarFeedForUser(t, ctx.database, ctx.user.ID)
	_, neutral := mustServeCalendarFeed(t, ctx.app, token, "neutral link")
	if strings.Contains(neutral, "SUMMARY:Period") || strings.Contains(neutral, "SUMMARY:Ovulation") {
		t.Fatalf("a neutral link named the estimate type:\n%s", neutral)
	}

	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).
		Update("calendar_feed_detailed_titles", true).Error; err != nil {
		t.Fatalf("opt the link in: %v", err)
	}
	_, detailed := mustServeCalendarFeed(t, ctx.app, token, "detailed link")
	if !strings.Contains(detailed, "SUMMARY:Period (estimate)") {
		t.Fatalf("an opted-in link did not name the period estimate:\n%s", detailed)
	}
	if strings.Contains(detailed, "SUMMARY:Ovumcy: reminder (estimate)") {
		t.Fatalf("an opted-in link still carries the neutral title:\n%s", detailed)
	}
}
