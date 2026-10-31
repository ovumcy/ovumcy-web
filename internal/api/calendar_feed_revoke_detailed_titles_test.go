package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestCalendarFeedRevokeClearsTheDetailedTitlesOptInBeforeAPreV2Mint replays
// WEB-389: opt in, revoke, then a pre-v2 binary mints a new link by writing only
// the selector and the bcrypt hash (it has no MAC and no title column). Back on
// this release that link must serve neutral titles, because the owner never
// opted it in; a revoke that left the flag set handed it the old link's choice.
func TestCalendarFeedRevokeClearsTheDetailedTitlesOptInBeforeAPreV2Mint(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "feed-titles-revoke@example.com")
	armCalendarFeedForUser(t, ctx.database, ctx.user.ID)

	optIn := settingsFormRequestWithCSRF(t, ctx, http.MethodPost, "/api/v1/users/current/calendar-feed/rotate",
		url.Values{"calendar_feed_detailed_titles": {"true"}}, nil)
	assertStatusCode(t, optIn, http.StatusSeeOther)
	if !reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedDetailedTitles {
		t.Fatal("precondition lost: a rotate with the box ticked did not store the opt-in")
	}

	revoked := settingsFormRequestWithCSRF(t, ctx, http.MethodDelete, "/api/v1/users/current/calendar-feed", url.Values{}, nil)
	assertStatusCode(t, revoked, http.StatusSeeOther)
	if reloadUserForCalendarFeedAPI(t, ctx, ctx.user.ID).CalendarFeedDetailedTitles {
		t.Fatal("revoking the feed left the detailed-titles opt-in set")
	}

	token, columns, err := services.GenerateCalendarFeedToken([]byte(testAppSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	if err := ctx.database.Model(&models.User{}).Where("id = ?", ctx.user.ID).Updates(map[string]any{
		"calendar_feed_selector":      columns.Selector,
		"calendar_feed_verifier_hash": columns.VerifierHash,
	}).Error; err != nil {
		t.Fatalf("pre-v2 mint: %v", err)
	}

	_, body := mustServeCalendarFeed(t, ctx.app, token, "after a pre-v2 mint")
	if strings.Contains(body, "SUMMARY:Period") || strings.Contains(body, "SUMMARY:Ovulation") {
		t.Fatalf("a link minted after the revoke inherited the revoked link's detailed titles:\n%s", body)
	}
}
