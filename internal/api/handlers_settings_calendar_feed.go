package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// Calendar (.ics) feed subscription — settings lifecycle handlers (slice 4).
//
// Three OwnerOnly + CSRF-protected endpoints let the owner manage the feed:
//   - POST   /api/v1/users/current/calendar-feed         → GenerateCalendarFeed
//   - POST   /api/v1/users/current/calendar-feed/rotate  → RotateCalendarFeed
//   - DELETE /api/v1/users/current/calendar-feed         → RevokeCalendarFeed
//
// All three are transport-only: CalendarFeedSettingsService owns token minting,
// persistence, and the status projection. Each is scoped strictly to the
// authenticated session's user id (currentUser), declares handler.OwnerOnly on
// its route, and is CSRF-protected by the global middleware (none is in the CSRF
// exemption list). None bumps auth_session_version — a per-surface feed
// capability is not an account credential; the recovery/compromise force-clear
// hooks live in the DB layer's atomic session-invalidation updates instead.
//
// SECRET HANDLING. The subscribe URL embeds the bearer token, so generate/rotate
// NEVER return it in the JSON/HTML body or a redirect query. Instead they seal
// the full URL into a one-time cookie (calendar_feed_reveal_cookie.go) and
// redirect to a dedicated reveal page that shows it exactly once, mirroring the
// recovery-code reveal. Revoke carries no secret. Nothing here logs the token.
//
// "Exactly once" is enforced by users.calendar_feed_revealed_at (migration 036),
// not by the cookie retraction: the mint NULLs the mark in the same write that
// persists the token, and the reveal page claims it with a compare-and-set. The
// cookie decides WHICH URL may be shown and the mark decides WHETHER it still
// may be, so a client that kept the sealed value gains nothing by presenting it
// again.

var (
	calendarFeedGenerateMutation = healthMutationKind{action: "settings.calendar_feed_generate", target: "calendar_feed"}
	calendarFeedRotateMutation   = healthMutationKind{action: "settings.calendar_feed_rotate", target: "calendar_feed"}
	calendarFeedRevokeMutation   = healthMutationKind{action: "settings.calendar_feed_revoke", target: "calendar_feed"}
)

// calendarFeedRevealEgress tags the one-time reveal of the subscribe URL. The
// URL is a standing read-only capability over the owner's predicted cycle, so
// the audited moment is the one where it reaches a person — the later polls by
// a calendar client are deliberately not audited (a bearer-token route that
// answers 404 with no oracle would have to log its own refusals to be useful,
// which is an oracle moved into the log; see docs/security/known-disclosures.md).
// The event records the fact of the reveal; the URL never enters the line.
var calendarFeedRevealEgress = healthEgressKind{action: "settings.calendar_feed_reveal", target: "calendar_feed"}

// calendarFeedRevealPath is the dedicated one-time reveal page the generate and
// rotate handlers redirect to after sealing the subscribe URL.
const calendarFeedRevealPath = "/settings/calendar-feed"

// GenerateCalendarFeed mints a fresh feed token for the owner (initial enable),
// seals the resulting subscribe URL for a one-time reveal, and redirects to the
// reveal page. If a feed is already configured this simply rotates it (the old
// URL dies immediately) — the UI only offers Generate when none is configured.
func (handler *Handler) GenerateCalendarFeed(c fiber.Ctx) error {
	return handler.issueCalendarFeedToken(c, calendarFeedGenerateMutation, false)
}

// RotateCalendarFeed mints a NEW feed token, invalidating the previous one
// (its selector no longer resolves and its verifier no longer matches), then
// reveals the new subscribe URL once. Used when the owner suspects the current
// URL leaked but wants to keep the feed enabled.
func (handler *Handler) RotateCalendarFeed(c fiber.Ctx) error {
	return handler.issueCalendarFeedToken(c, calendarFeedRotateMutation, true)
}

// issueCalendarFeedToken is the shared generate/rotate body: mint + persist the
// token via the service, build the absolute subscribe URL from the request base
// URL, seal it into the one-time reveal cookie, and redirect to the reveal page.
// rotated selects the reveal-page heading and the JSON status.
func (handler *Handler) issueCalendarFeedToken(c fiber.Ctx, mutation healthMutationKind, rotated bool) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, mutation, unauthorizedErrorSpec())
	}

	token, err := handler.calendarFeedSettings.GenerateFeedToken(c.Context(), user.ID)
	if err != nil {
		return handler.failMutation(c, mutation, settingsCalendarFeedUpdateErrorSpec())
	}

	feedURL := calendarFeedSubscribeURL(c, token)
	if err := handler.setCalendarFeedRevealCookie(c, user.ID, feedURL, rotated); err != nil {
		return handler.failMutation(c, mutation, settingsCalendarFeedUpdateErrorSpec())
	}

	handler.logMutationSuccess(c, mutation)

	status := services.SettingsCalendarFeedGeneratedStatus
	if rotated {
		status = services.SettingsCalendarFeedRotatedStatus
	}
	if acceptsJSON(c) {
		// JSON clients get the next path to the one-time reveal, NEVER the URL
		// itself — the secret only ever leaves via the sealed reveal cookie.
		return c.JSON(fiber.Map{
			"ok":        true,
			"status":    status,
			"next_step": "calendar_feed_reveal",
			"next_path": calendarFeedRevealPath,
		})
	}
	return redirectToPath(c, calendarFeedRevealPath)
}

// RevokeCalendarFeed disables the owner's feed by clearing both token columns.
// Any previously-issued subscribe URL 404s immediately. It carries no secret and
// returns to /settings with a flash (or JSON status for API clients).
func (handler *Handler) RevokeCalendarFeed(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return handler.failMutation(c, calendarFeedRevokeMutation, unauthorizedErrorSpec())
	}

	if err := handler.calendarFeedSettings.RevokeFeedToken(c.Context(), user.ID); err != nil {
		return handler.failMutation(c, calendarFeedRevokeMutation, settingsCalendarFeedUpdateErrorSpec())
	}

	status := services.SettingsCalendarFeedRevokedStatus
	handler.logMutationSuccess(c, calendarFeedRevokeMutation)

	if acceptsJSON(c) {
		return c.JSON(fiber.Map{"ok": true, "status": status})
	}
	if isHTMX(c) {
		return c.SendString(htmxSettingsSuccessMarkup(c, status, "Calendar feed turned off."))
	}
	handler.setFlashCookie(c, FlashPayload{SettingsSuccess: status})
	return redirectOrJSON(c, "/settings")
}

// ShowCalendarFeedRevealPage renders the subscribe URL exactly once. It reads
// the sealed one-time reveal cookie, CLAIMS the owner's server-side reveal mark,
// clears the cookie, and shows the URL. A refresh, a later visit, or a replay of
// the original sealed value finds the mark already claimed and redirects back to
// /settings, so the URL is never shown again until a generate or rotate mints a
// new token and re-arms the mark in the same write.
func (handler *Handler) ShowCalendarFeedRevealPage(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	state := handler.readCalendarFeedRevealState(c, user.ID)
	if state.FeedURL == "" {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
	// Shown-once is decided HERE, by the server-side consumption mark, not by the
	// cookie retraction below: a client that kept the sealed value can present it
	// again on this same session, and a retraction it never obeyed costs it
	// nothing. The claim is a compare-and-set, so a replay and a concurrent
	// second reveal both lose and land on /settings — the same place an absent
	// cookie lands. A claim that errors is refused for the same reason: the mark
	// is what makes the reveal single-use, and a reveal that cannot record itself
	// is not single-use. The cost of refusing is one rotation from settings.
	claimed, err := handler.calendarFeedSettings.ClaimFeedReveal(c.Context(), user.ID, time.Now())
	if err != nil || !claimed {
		handler.clearCalendarFeedRevealCookie(c)
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
	// Retract the cookie too: the mark refuses the replay, and this keeps an
	// unusable value from lingering in the browser.
	handler.clearCalendarFeedRevealCookie(c)
	handler.logEgressSuccess(c, calendarFeedRevealEgress)

	return handler.render(c, "calendar_feed_reveal", fiber.Map{
		"Title":               localizedPageTitle(currentMessages(c), "meta.title.calendar_feed", "Ovumcy | Calendar feed"),
		"CalendarFeedURL":     state.FeedURL,
		"CalendarFeedRotated": state.Rotated,
		"HideNavigation":      true,
	})
}

// calendarFeedSubscribeURL builds the absolute subscribe URL for a freshly
// minted token from the request's own base URL (scheme + host) so a self-hosted
// instance reveals a URL that works from the owner's network without any
// server-side base-URL configuration. The token is a clean path segment
// (SplitCalendarFeedToken already validated its shape upstream at resolve time);
// here it comes straight from GenerateCalendarFeedToken, so it is URL-path-safe.
func calendarFeedSubscribeURL(c fiber.Ctx, token string) string {
	base := strings.TrimRight(c.BaseURL(), "/")
	return base + "/calendar/feed/" + token + ".ics"
}
