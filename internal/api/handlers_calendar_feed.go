package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

// Calendar (.ics) feed endpoint (issue #126-replacement / .ics, slice 3).
//
// Route: GET /calendar/feed/:token.ics — a PAGE route (registerPageRoutes), NOT
// under /api/v1 and NOT behind AuthRequired/OwnerOnly: a calendar client sends
// no cookie, so the request is authenticated by the bearer TOKEN alone. The
// route is shaped so the token is a clean :token parameter (Fiber treats '.' as
// a parameter delimiter, so ":token.ics" binds param "token" with ".ics" as a
// literal constant suffix). This matters for log redaction: SafeRequestLogPath
// prefers the matched route template ("/calendar/feed/:token.ics"), whose last
// segment already begins with ':' and is therefore emitted verbatim as
// ":token.ics" — the token VALUE never reaches a log line. The raw-path
// fallback is additionally hardened in request_logging.go to mask an
// "<opaque-token>.ics" segment, closing the flagged gap even if the route
// template were ever unavailable.
//
// Security envelope (all enforced here + in CalendarFeedService):
//   - Token IS the authorization. CalendarFeedService.ResolveFeed splits it,
//     resolves the owner by non-secret selector, constant-time-verifies the
//     secret verifier, and scopes every read to the resolved user id.
//   - 404-no-oracle: a malformed token, unknown selector, wrong verifier, and
//     disabled feed ALL return an identical bare 404 and no Set-Cookie, and are
//     timing-equalized (a dummy bcrypt runs on the selector-miss path inside the
//     service). Bare here means what c.SendStatus actually sends: Fiber's fixed
//     status text under text/plain, the same bytes whatever the cause was — not
//     an empty body.
//   - Rate-limited per-IP by the edge /api-style limiter wired in main.go for
//     this exact path; a 429 returns the limiter's own response (no UI needed).
//   - Response headers pin text/calendar, a private 1-hour cache, and
//     X-Robots-Tag: noindex.

const (
	// CalendarFeedRateLimitPrefix is the path prefix the composition root
	// (cmd/ovumcy/main.go) mounts the per-IP feed rate limiter on. It is
	// exported so the limiter wiring and the route share one source of truth;
	// every concrete feed URL (…/calendar/feed/<token>.ics) sits under it.
	CalendarFeedRateLimitPrefix = "/calendar/feed"

	// calendarFeedRouteSuffix is the literal fiber binds after the ":token"
	// parameter. Shared by the route pattern below, the predicate's route-form
	// check, and the settings page's subscribe-URL builder
	// (calendarFeedSubscribeURL) so the three can never drift apart.
	calendarFeedRouteSuffix = ".ics"

	// calendarFeedRoutePath is the registered page route. Fiber treats '.' as a
	// parameter delimiter, so ":token.ics" binds param "token" (without ".ics")
	// and ".ics" is a literal constant suffix.
	calendarFeedRoutePath = CalendarFeedRateLimitPrefix + "/:token" + calendarFeedRouteSuffix
)

// IsCalendarFeedRequest reports whether method and path together select
// fiber's dispatch of the cookieless feed's own route — GET or HEAD (app.Get
// registers both) at a concrete feed URL — as opposed to a neighbour that
// merely shares its leading characters, a nested or empty-token path under the
// same prefix, or a mutating verb. The two app-wide middlewares that skip this
// route (csrfMiddlewareConfig.Next in cmd/ovumcy/server.go, the early return
// in LanguageMiddleware) and the feed's own rate limiter (server.go) all key on
// this one predicate, so the three can never disagree about which requests are
// "the feed".
//
// path is compared against fiber's OWN routing normalization, not the raw
// wire path c.Path() returns: the shipped fiberConfig (cmd/ovumcy/server.go)
// leaves CaseSensitive and StrictRouting both off, so the router folds ASCII
// case and trailing slashes away before matching a route, and a predicate
// comparing the untouched path claims fewer spellings than the router actually
// dispatches here. Verified experimentally against a probe registering this
// exact route pattern under the shipped fiberConfig, not assumed from fiber's
// docs (see cmd/ovumcy's route-shape definition test for calendarFeedRoutePath):
// the token may contain any character but "/" (fiber treats only the FINAL
// ".ics" as the literal delimiter, so "a.b.ics" binds token "a.b"), must be
// non-empty, and the prefix/suffix match case-insensitively.
func IsCalendarFeedRequest(method, path string) bool {
	return (method == fiber.MethodGet || method == fiber.MethodHead) && isCalendarFeedRequestPath(path)
}

// isCalendarFeedRequestPath is IsCalendarFeedRequest's route-form half, kept
// separate (and unexported) because the mutating-verb refusal above is a
// property of the METHOD, not the path.
func isCalendarFeedRequestPath(path string) bool {
	normalized := normalizeCalendarFeedRequestPath(path)
	prefixWithSeparator := CalendarFeedRateLimitPrefix + "/"
	if !strings.HasPrefix(normalized, prefixWithSeparator) || !strings.HasSuffix(normalized, calendarFeedRouteSuffix) {
		return false
	}
	token := normalized[len(prefixWithSeparator) : len(normalized)-len(calendarFeedRouteSuffix)]
	return token != "" && !strings.Contains(token, "/")
}

// normalizeCalendarFeedRequestPath mirrors fiber's own routing normalization
// under the shipped fiberConfig (cmd/ovumcy/server.go leaves CaseSensitive and
// StrictRouting both off): ASCII letters lowercased, then trailing slashes
// trimmed. strings.ToLower is the wrong primitive here — it is Unicode-aware
// and folds code points (e.g. U+212A KELVIN SIGN) that fiber's byte-only table
// leaves alone — so this is a byte-for-byte copy of the same two operations
// cmd/ovumcy/ratelimit.go's asciiLowerPath/routingNormalizedPath apply for
// its own edge limiters; cmd/ovumcy is the composition root that imports this
// package, so importing back from here would cycle, and this package keeps a
// second copy of the same two operations instead.
func normalizeCalendarFeedRequestPath(path string) string {
	lowered := []byte(path)
	changed := false
	for i := range lowered {
		if lowered[i] >= 'A' && lowered[i] <= 'Z' {
			lowered[i] += 'a' - 'A'
			changed = true
		}
	}
	normalized := path
	if changed {
		normalized = string(lowered)
	}
	if len(normalized) > 1 {
		normalized = strings.TrimRight(normalized, "/")
	}
	return normalized
}

// ServeCalendarFeed serves an owner's read-only .ics feed, authenticated by the
// path token alone. It never sets a cookie and never renders an HTML error: the
// only outcomes are 200 + calendar body, a bare 404 (every not-found/invalid
// case, answered with the same status text, so no oracle), or a generic 500 on
// an infrastructure failure. That promise also holds one layer up: the two
// app-wide middlewares mounted ahead of this handler (CSRF, LanguageMiddleware)
// are excluded, through IsCalendarFeedRequest, from the safe-method side
// effect that would otherwise mint and set one of their own cookies on any GET
// or HEAD lacking a matching cookie — see csrfMiddlewareConfig.Next in
// cmd/ovumcy/server.go and the early return in LanguageMiddleware. One
// consequence is deliberate: with LanguageMiddleware skipped, requestLocation
// below is the instance zone, never a zone the polling request named. The
// owner's stored users.timezone decides the calendar day regardless
// (CalendarFeedService.ResolveFeed); only an owner who never had one captured
// is affected, and for them the capability URL now renders the same day
// whoever polls it instead of following the poller's cookie.
func (handler *Handler) ServeCalendarFeed(c fiber.Ctx) error {
	token := c.Params("token")
	location := handler.requestLocation(c)
	now := time.Now().In(location)

	body, ok, err := handler.calendarFeedService.ResolveFeed(c.Context(), token, now, location)
	if err != nil {
		// Infrastructure failure (DB / log read). Return a generic 500 via the
		// top-level error handler — never the raw error, and never a body that
		// distinguishes this from the 404 path in a token-revealing way. The
		// handler answers it through the app-wide mapped envelope (stable key
		// "internal_error"), which is a constant: it is derived from the status
		// alone and says nothing about the token, the owner, or the failure.
		return fiber.ErrInternalServerError
	}
	if !ok {
		// Uniform bare 404 for malformed token / unknown selector / wrong
		// verifier / disabled feed: SendStatus answers with Fiber's fixed status
		// text, identical in every case. No Set-Cookie, timing-equalized in the
		// service.
		return c.SendStatus(fiber.StatusNotFound)
	}

	c.Set(fiber.HeaderContentType, "text/calendar; charset=utf-8")
	// Overwrites the middleware's default no-store: the feed is a private,
	// owner-scoped resource a calendar client may cache briefly (1h) but that
	// must not be stored by shared/proxy caches beyond the owner.
	c.Set(fiber.HeaderCacheControl, "private, max-age=3600")
	c.Set("X-Robots-Tag", "noindex")
	return c.Send(body)
}
