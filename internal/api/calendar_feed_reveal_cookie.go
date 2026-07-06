package api

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Calendar (.ics) feed URL — shown-once reveal transport (slice 4).
//
// The feed subscribe URL embeds the bearer token, so it is a SECRET and is
// revealed to the owner EXACTLY ONCE — on generate/rotate — using the same
// sealed one-time cookie mechanism as the recovery code (recovery_code_page_
// cookie.go). The generate/rotate handler seals the full URL into this cookie
// and redirects to a dedicated reveal page; that page reads the cookie once,
// CLEARS it immediately, and renders the URL. On any later settings load the
// cookie is gone, so the URL is never re-rendered into an HTML value — the
// settings section then shows only configured/not-configured status.
//
// The URL never appears in a query string, a redirect target, JSON, or a log:
// it lives only inside the AEAD-sealed cookie payload until the single reveal.

const calendarFeedRevealCookieTTL = 20 * time.Minute

type calendarFeedRevealPayload struct {
	UserID  uint   `json:"uid"`
	FeedURL string `json:"feed_url"`
	// Rotated distinguishes a rotate reveal from a first-time generate reveal so
	// the page can show the right heading/copy. It carries no secret.
	Rotated bool `json:"rotated,omitempty"`
}

type calendarFeedRevealState struct {
	FeedURL string
	Rotated bool
}

var calendarFeedRevealCookieSpec = sealedCookieSpec{name: calendarFeedRevealCookieName, path: "/"}

// setCalendarFeedRevealCookie seals the full subscribe URL for a one-time
// reveal, scoped to userID so a stale cookie cannot leak one owner's URL onto
// another owner's reveal page. An empty URL is a programming error (the caller
// just minted a token) and clears any prior cookie instead of sealing a blank.
func (handler *Handler) setCalendarFeedRevealCookie(c fiber.Ctx, userID uint, feedURL string, rotated bool) error {
	url := strings.TrimSpace(feedURL)
	if url == "" {
		handler.clearCalendarFeedRevealCookie(c)
		return errors.New("calendar feed url is required")
	}
	payload := calendarFeedRevealPayload{UserID: userID, FeedURL: url, Rotated: rotated}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err // codecov:ignore -- json.Marshal of a fixed struct does not fail
	}
	return handler.writeSealedCookie(c, calendarFeedRevealCookieSpec, serialized, time.Now().Add(calendarFeedRevealCookieTTL))
}

// readCalendarFeedRevealState opens the sealed one-time cookie and returns the
// revealed URL, or an empty state when the cookie is absent, malformed, or
// scoped to a different user. It does NOT clear the cookie — the caller clears
// it right after a successful read so the URL is shown exactly once. Every
// failure path clears the cookie defensively so a corrupt value cannot linger.
func (handler *Handler) readCalendarFeedRevealState(c fiber.Ctx, userID uint) calendarFeedRevealState {
	raw := strings.TrimSpace(c.Cookies(calendarFeedRevealCookieName))
	if raw == "" {
		return calendarFeedRevealState{}
	}

	decoded, err := handler.openCookieValue(calendarFeedRevealCookieName, raw)
	if err != nil {
		handler.clearCalendarFeedRevealCookie(c)
		return calendarFeedRevealState{}
	}

	payload := calendarFeedRevealPayload{}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		handler.clearCalendarFeedRevealCookie(c)
		return calendarFeedRevealState{}
	}

	url := strings.TrimSpace(payload.FeedURL)
	if url == "" {
		handler.clearCalendarFeedRevealCookie(c)
		return calendarFeedRevealState{}
	}
	if payload.UserID != 0 && userID != 0 && payload.UserID != userID {
		handler.clearCalendarFeedRevealCookie(c)
		return calendarFeedRevealState{}
	}

	return calendarFeedRevealState{FeedURL: url, Rotated: payload.Rotated}
}

func (handler *Handler) clearCalendarFeedRevealCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, calendarFeedRevealCookieSpec)
}
