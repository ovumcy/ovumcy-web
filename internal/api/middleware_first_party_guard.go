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

// Why a guarded route refused a request. The guard knows which check fired and
// the refusal handler does not, so the reason travels with the call rather than
// being guessed at the far end — an audit line that named a cross-origin
// initiator for a browser's own prefetch sends an operator after an attacker who
// was never there.
const (
	firstPartyRefusedOffOriginInitiator = "off_origin_initiator"
	firstPartyRefusedUnsupportedDest    = "unsupported_destination"
	firstPartyRefusedSpeculativeLoad    = "speculative_load"
)

// requireFirstPartyRequest guards a GET route that consumes something
// one-time — a reveal mark, a pickup nonce — and therefore mutates state on a
// method the CSRF middleware never validates, because a safe method is not
// supposed to mutate anything. It asks whether the request is FIRST-PARTY, not
// whether it is a navigation: a client following the published next_path fetches,
// and refusing it would narrow the API for nothing (see the destination rule
// below). Moving the mutation to POST is the answer the
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
// The guard is deliberately MONOTONE, and the rule is applied PER HEADER: each
// check refuses only on a value the browser actually stated, and an absent header
// decides nothing. A browser older than Chrome 76 / Firefox 90 / Safari 16.4, or
// a non-browser client carrying no ambient cookies to forge with, sends none of
// the family and lands exactly where it landed before this guard existed — on Lax
// plus the one-time CAS and the sealed owner-bound cookie. A proxy that forwards
// part of the family and strips the rest is the same case one header at a time,
// which is why no check may read an absent header as a refusal: doing so would
// trade a residual for an owner who can no longer pick up her recovery code at
// all, the worse failure on a self-hosted instance. The absence is never
// attacker-controlled either — page script cannot strip a `Sec-` header from a
// victim's browser, so what an attacker's request states is what it is judged on.
//
// `refuse` is the guarded route's OWN neutral exit rather than a new error
// surface, so a refused request is indistinguishable from the replay each route
// already answers, and stays audited in that route's existing vocabulary. It
// receives the reason the check that fired states, because only the guard knows
// which one did.
func requireFirstPartyRequest(refuse func(fiber.Ctx, string) error) fiber.Handler {
	return func(c fiber.Ctx) error {
		// "none" is a load with no initiator document: a typed URL, a bookmark,
		// a restored tab. "same-site" is refused along with "cross-site" —
		// the CSRF middleware compares scheme+host exactly, so a sibling
		// subdomain is another origin here too.
		site := strings.TrimSpace(c.Get(headerSecFetchSite))
		if site != "" && !strings.EqualFold(site, secFetchSiteSameOrigin) && !strings.EqualFold(site, secFetchSiteNone) {
			return refuse(c, firstPartyRefusedOffOriginInitiator)
		}
		// A page load is `document`; a client following the published next_path is
		// `empty`. Every other destination — image, iframe, object, script, and
		// the rest — is an embed no flow into these routes produces, and is the
		// shape that would spend the one-time value with nothing on screen.
		dest := strings.TrimSpace(c.Get(headerSecFetchDest))
		if dest != "" && !strings.EqualFold(dest, secFetchDestDocument) && !strings.EqualFold(dest, secFetchDestEmpty) {
			return refuse(c, firstPartyRefusedUnsupportedDest)
		}
		// A speculative load wears the navigation's own shape — same-origin,
		// document — and is the one request reaching here that nobody decided to
		// make. Nothing links to either guarded route, so this costs no real flow;
		// it is here because a browser or an extension that starts prefetching one
		// would otherwise spend the owner's one-time value on her behalf, and the
		// two checks above cannot tell.
		if strings.Contains(strings.ToLower(c.Get(headerSecPurpose)), secPurposePrefetch) {
			return refuse(c, firstPartyRefusedSpeculativeLoad)
		}
		return c.Next()
	}
}
