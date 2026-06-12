package api

import (
	"time"

	"github.com/gofiber/fiber/v2"
)

// sealedCookieSpec describes the transport attributes of one sealed-cookie
// kind. Each cookie file keeps its own payload shape, serialization, and
// validity rules and declares a spec next to its name constant; the
// write/clear helpers below own the attribute boilerplate, so a
// cross-cutting cookie-attribute change (SameSite policy, Partitioned,
// Domain) is a one-place edit instead of a hand-sweep over every producer.
type sealedCookieSpec struct {
	name string
	path string
	// sameSite defaults to "Lax" when empty.
	sameSite string
	// forceSecure marks cross-site cookies (SameSite=None) that must be
	// written Secure regardless of COOKIE_SECURE — browsers reject
	// SameSite=None without Secure. Clears still follow handler.cookieSecure,
	// matching the long-standing behavior of the OIDC transport cookies.
	forceSecure bool
}

func (spec sealedCookieSpec) sameSiteOrLax() string {
	if spec.sameSite == "" {
		return "Lax"
	}
	return spec.sameSite
}

// writeSealedCookie seals plaintext for the spec's cookie and writes it with
// the canonical attribute set (HttpOnly, SameSite, Secure per deployment).
// A zero expires writes a session-scoped cookie (payloads carry their own
// TTL in that case).
func (handler *Handler) writeSealedCookie(c *fiber.Ctx, spec sealedCookieSpec, plaintext []byte, expires time.Time) error {
	encoded, err := handler.sealCookieValue(spec.name, plaintext)
	if err != nil {
		return err
	}

	cookie := &fiber.Cookie{
		Name:     spec.name,
		Value:    encoded,
		Path:     spec.path,
		HTTPOnly: true,
		Secure:   handler.cookieSecure || spec.forceSecure,
		SameSite: spec.sameSiteOrLax(),
	}
	if !expires.IsZero() {
		cookie.Expires = expires
	}
	c.Cookie(cookie)
	return nil
}

// clearSealedCookie expires the spec's cookie with attributes matching the
// write path, so the browser reliably drops it.
func (handler *Handler) clearSealedCookie(c *fiber.Ctx, spec sealedCookieSpec) {
	c.Cookie(&fiber.Cookie{
		Name:     spec.name,
		Value:    "",
		Path:     spec.path,
		HTTPOnly: true,
		Secure:   handler.cookieSecure,
		SameSite: spec.sameSiteOrLax(),
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}

func (handler *Handler) sealCookieValue(cookieName string, plaintext []byte) (string, error) {
	codec, err := handler.cookieCodec()
	if err != nil {
		return "", err
	}
	return codec.seal(cookieName, plaintext)
}

func (handler *Handler) openCookieValue(cookieName string, raw string) ([]byte, error) {
	codec, err := handler.cookieCodec()
	if err != nil {
		return nil, err
	}
	return codec.open(cookieName, raw)
}
