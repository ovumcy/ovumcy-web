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
// CLAIMS the owner's server-side reveal mark, clears the cookie, and renders the
// URL. On any later settings load the cookie is gone, so the URL is never
// re-rendered into an HTML value — the settings section then shows only
// configured/not-configured status.
//
// The once-ness is the MARK, not the clear. Clearing a cookie asks a browser to
// forget a value it was handed and binds nothing that kept it, so this cookie
// says only WHICH URL may be shown; users.calendar_feed_revealed_at (migration
// 036) says whether it still may be. The mint NULLs it in the same write that
// persists the token, and the reveal page claims it with a compare-and-set.
//
// The mark closes a reveal that HAPPENED; a mint whose reveal was never
// consumed is bounded by the payload's own expiry instead. The two are
// independent, and both are server-side: the expiry answers "is this cookie
// still current", the mark answers "has this owner's reveal already been spent".
//
// The URL never appears in a query string, a redirect target, JSON, or a log:
// it lives only inside the AEAD-sealed cookie payload until the single reveal.

// calendarFeedRevealCookieTTL is the reveal's lifetime, written from one value
// into two places: the Set-Cookie `Expires` attribute and the `expires_at` the
// sealed payload carries. Only the second is a bound. The attribute is a hint
// the browser is free to ignore and the codec's open() has no notion of time,
// so a reveal cookie without a server-verified expiry stays replayable until
// the token is rotated, the feed revoked, or SECRET_KEY changes.
const calendarFeedRevealCookieTTL = 20 * time.Minute

type calendarFeedRevealPayload struct {
	UserID  uint   `json:"uid"`
	FeedURL string `json:"feed_url"`
	// Rotated distinguishes a rotate reveal from a first-time generate reveal so
	// the page can show the right heading/copy. It carries no secret.
	Rotated bool `json:"rotated,omitempty"`
	// ExpiresAt is the server-verified half of the reveal's lifetime, carried
	// the way the recovery-code reveal cookie carries its own beside its owner
	// id: the payload names the moment it stops being honored, and the reader
	// compares that against the clock instead of trusting the browser to have
	// dropped the cookie. A payload from before the field existed decodes to the
	// zero time, which is always already past, so an absent bound is invalid
	// input rather than permission to reveal for as long as the token lives.
	ExpiresAt time.Time `json:"expires_at"`
}

type calendarFeedRevealState struct {
	FeedURL string
	Rotated bool
}

var calendarFeedRevealCookieSpec = sealedCookieSpec{name: calendarFeedRevealCookieName, path: "/"}

// setCalendarFeedRevealCookie seals the full subscribe URL for a one-time
// reveal, scoped to userID so a stale cookie cannot leak one owner's URL onto
// another owner's reveal page. A zero userID or an empty URL is a programming
// error (the caller just minted a token for a resolved owner) and clears any
// prior cookie instead of sealing an unattributed or blank payload. Refusing
// the zero id here is what keeps the reveal scoped structurally: the read path
// has no owner to compare against once such a payload exists.
func (handler *Handler) setCalendarFeedRevealCookie(c fiber.Ctx, userID uint, feedURL string, rotated bool) error {
	if userID == 0 {
		handler.clearCalendarFeedRevealCookie(c)
		return errors.New("calendar feed reveal requires an owner id")
	}
	url := strings.TrimSpace(feedURL)
	if url == "" {
		handler.clearCalendarFeedRevealCookie(c)
		return errors.New("calendar feed url is required")
	}
	expiresAt := time.Now().Add(calendarFeedRevealCookieTTL)

	payload := calendarFeedRevealPayload{UserID: userID, FeedURL: url, Rotated: rotated, ExpiresAt: expiresAt}
	serialized, err := json.Marshal(payload)
	if err != nil {
		return err // codecov:ignore -- json.Marshal of a fixed struct does not fail
	}
	return handler.writeSealedCookie(c, calendarFeedRevealCookieSpec, serialized, expiresAt)
}

// readCalendarFeedRevealState opens the sealed one-time cookie and returns the
// revealed URL, or an empty state when the cookie is absent, malformed,
// unattributed, scoped to a different user, or past the expiry it carries —
// including a payload that carries none. It does NOT clear the cookie and
// does NOT decide single use — the caller claims the owner's reveal mark and
// then clears the cookie, so a value this reader accepts twice is still shown
// only once. Every failure path clears the cookie defensively so a corrupt value
// cannot linger.
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
	if !sealedPayloadBelongsToSession(payload.UserID, userID) {
		handler.clearCalendarFeedRevealCookie(c)
		return calendarFeedRevealState{}
	}
	// Refusing here takes away the display, never the feed: the page already
	// resolved an authenticated session, so the owner lands on /settings — where
	// an absent cookie lands — with the feed itself untouched, and rotates to
	// mint a new URL and re-arm the reveal in the same write.
	if time.Now().After(payload.ExpiresAt) {
		handler.clearCalendarFeedRevealCookie(c)
		return calendarFeedRevealState{}
	}

	return calendarFeedRevealState{FeedURL: url, Rotated: payload.Rotated}
}

func (handler *Handler) clearCalendarFeedRevealCookie(c fiber.Ctx) {
	handler.clearSealedCookie(c, calendarFeedRevealCookieSpec)
}
