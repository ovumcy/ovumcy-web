package api

import (
	"strings"

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

var (
	calendarFeedGenerateMutation = healthMutationKind{action: "settings.calendar_feed_generate", target: "calendar_feed"}
	calendarFeedRotateMutation   = healthMutationKind{action: "settings.calendar_feed_rotate", target: "calendar_feed"}
	calendarFeedRevokeMutation   = healthMutationKind{action: "settings.calendar_feed_revoke", target: "calendar_feed"}
)

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
// the sealed one-time reveal cookie, CLEARS it immediately, and shows the URL;
// on any refresh or later visit the cookie is gone, so it redirects back to
// /settings and the URL is never shown again.
func (handler *Handler) ShowCalendarFeedRevealPage(c fiber.Ctx) error {
	user, ok := currentUser(c)
	if !ok {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/login")
	}

	state := handler.readCalendarFeedRevealState(c, user.ID)
	if state.FeedURL == "" {
		return c.Redirect().Status(fiber.StatusSeeOther).To("/settings")
	}
	// Shown-once: drop the cookie now so a reload cannot re-reveal the URL.
	handler.clearCalendarFeedRevealCookie(c)

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
