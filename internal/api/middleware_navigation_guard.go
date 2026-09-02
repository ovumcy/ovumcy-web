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
	headerSecFetchDest = "Sec-Fetch-Dest"
	headerSecPurpose   = "Sec-Purpose"

	secFetchSiteSameOrigin = "same-origin"
	secFetchSiteNone       = "none"
	secFetchDestDocument   = "document"
	secFetchDestEmpty      = "empty"
	secPurposePrefetch     = "prefetch"
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
// SAME-ORIGIN requests are judged by destination, not by whether they are a
// navigation. Both routes are the published `next_path` a JSON client is told to
// follow (docs/openapi.yaml), so a first-party client fetching one must not be
// turned away; `empty` is the destination such a fetch carries. Refusing it
// bought less than it looked: injected same-origin script that wanted the value
// would open the route in a window and read it out of the same-origin document,
// which wears `document` and passes. What is worth refusing is the destination no
// client of these routes ever has — `image`, `iframe`, `object` and their
// siblings — because those are the shapes that spend a one-time value with
// nothing rendered for anybody to notice.
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
		// A page load is `document`; a client following the published next_path is
		// `empty`. Every other destination — image, iframe, object, script, and
		// the rest — is an embed no flow into these routes produces, and is the
		// shape that would spend the one-time value with nothing on screen.
		dest := strings.TrimSpace(c.Get(headerSecFetchDest))
		if !strings.EqualFold(dest, secFetchDestDocument) && !strings.EqualFold(dest, secFetchDestEmpty) {
			return refuse(c)
		}
		// A speculative load carries the full navigation shape — same-origin,
		// document — and is the one request that reaches here without anybody
		// having decided to go. Nothing links to either guarded route, so this
		// costs no real flow; it is here because a browser or an extension that
		// starts prefetching one would otherwise spend the owner's one-time value
		// on her behalf, and the guard would have no way to tell.
		if strings.Contains(strings.ToLower(c.Get(headerSecPurpose)), secPurposePrefetch) {
			return refuse(c)
		}
		return c.Next()
	}
}
