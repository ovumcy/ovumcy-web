package api

import (
	"strings"

	"github.com/gofiber/fiber/v3"
)

// Fetch Metadata request headers. A browser sets these itself and refuses to let
// page script write them — the `Sec-` prefix is a forbidden header name — so
// what they say about a request's initiator cannot be forged from an attacker's
// document.
const (
	headerSecFetchSite = "Sec-Fetch-Site"
	headerSecFetchMode = "Sec-Fetch-Mode"
	headerSecFetchDest = "Sec-Fetch-Dest"

	secFetchSiteSameOrigin = "same-origin"
	secFetchSiteNone       = "none"
	secFetchModeNavigate   = "navigate"
	secFetchDestDocument   = "document"
)

// requireSameOriginNavigation guards a GET route that consumes something
// one-time — a reveal mark, a pickup nonce — and therefore mutates state on a
// method the CSRF middleware never validates, because a safe method is not
// supposed to mutate anything. Moving the mutation to POST is the answer the
// method contract wants, but both such routes are the landing pages of a
// redirect-after-POST whose shape is published in docs/openapi.yaml (the
// register 303 and the calendar-feed `next_path`), so that is a spec change and
// not a handler change. Until it happens this is the CSRF equivalent the
// "Endpoint defense-in-depth" invariant asks for.
//
// What it actually closes is the SameSite=Lax residual. Every cookie these two
// routes read is Lax, so a cross-site subresource load — <img>, <iframe>, fetch,
// prefetch — already arrives with no cookies and consumes nothing. The one case
// Lax permits is a cross-site TOP-LEVEL navigation, which carries the cookies in
// full; that is the request this guard refuses, and Sec-Fetch-Site names it
// exactly.
//
// The guard is deliberately MONOTONE. When the browser speaks Fetch Metadata we
// obey it; when the header family is absent entirely — a browser older than
// Chrome 76 / Firefox 90 / Safari 16.4, or a non-browser client, which carries
// no ambient cookies to forge with anyway — the request lands exactly where it
// landed before this guard existed, on Lax plus the one-time CAS and the sealed
// owner-bound cookie. So the guard can only remove reachable forgeries, never
// add one, and it can never lock out a client that worked yesterday. Refusing on
// absence would trade a residual against an owner who can no longer pick up her
// recovery code at all, which is the worse failure on a self-hosted instance.
// The absence is also not attacker-controlled: page script cannot strip a
// `Sec-` header from a victim's browser.
//
// `refuse` is the guarded route's OWN neutral exit rather than a new error
// surface, so a refused request is indistinguishable from the replay each route
// already answers, and stays audited in that route's existing vocabulary.
func requireSameOriginNavigation(refuse fiber.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		site := strings.TrimSpace(c.Get(headerSecFetchSite))
		if site == "" {
			return c.Next()
		}
		// "none" is a load with no initiator document: a typed URL, a bookmark,
		// a restored tab. "same-site" is refused along with "cross-site" —
		// the CSRF middleware compares scheme+host exactly, so a sibling
		// subdomain is another origin here too.
		if !strings.EqualFold(site, secFetchSiteSameOrigin) && !strings.EqualFold(site, secFetchSiteNone) {
			return refuse(c)
		}
		// Site alone would still admit a same-origin fetch() or <img>, which no
		// flow into these pages produces and which a compromised same-origin
		// surface could use to spend the one-time value out from under the owner.
		if !strings.EqualFold(strings.TrimSpace(c.Get(headerSecFetchMode)), secFetchModeNavigate) {
			return refuse(c)
		}
		if !strings.EqualFold(strings.TrimSpace(c.Get(headerSecFetchDest)), secFetchDestDocument) {
			return refuse(c)
		}
		return c.Next()
	}
}
