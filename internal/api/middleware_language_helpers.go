package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

func (handler *Handler) LanguageMiddleware(c fiber.Ctx) error {
	if IsCalendarFeedRequest(c.Method(), c.Path()) {
		// The cookieless calendar feed is authenticated by its path token alone
		// and reads no request language: ServeCalendarFeed resolves "today"
		// from the owner's stored users.timezone
		// (CalendarFeedService.ResolveFeed), and a real calendar client sends
		// no language cookie. The route authenticates nobody either, so the
		// timezone header and cookie are never resolved for it
		// (resolveOwnerRequestTimezone): an owner whose users.timezone was
		// never captured gets the instance zone, not the poller's cookies —
		// the same capability URL must not render a different day in the
		// owner's browser than in the calendar client, and the feed sets no
		// cookie on any outcome (docs/SECURITY_INVARIANTS.md → Calendar feed
		// subscription).
		//
		// See IsCalendarFeedRequest for why a mutating verb at this same path
		// shape still falls through to the ordinary branch below instead of
		// inheriting this skip.
		return c.Next()
	}
	// The timezone half (X-Ovumcy-Timezone header, ovumcy_tz cookie) no longer
	// resolves here. See WEB-35: the header admits any short identifier, so
	// resolving it costs one zoneinfo read/parse, and this middleware runs on
	// every route, rate-limited or not. resolveOwnerRequestTimezone runs it
	// instead, called from authenticateRequest once a session has actually
	// verified, so an anonymous caller cannot buy that cost on a public page
	// or by forging a session cookie on a protected path. A request that never
	// authenticates leaves contextLocationKey unset here; requestLocation
	// (page_request_helpers.go) already falls back to the server-configured
	// zone when it is absent, so every anonymous page renders with that
	// fallback instead.
	language := handler.resolveRequestLanguage(c)

	c.Locals(contextLanguageKey, language)
	c.Locals(contextMessagesKey, handler.i18n.Messages(language))
	return c.Next()
}

// resolveOwnerRequestTimezone resolves the request-local timezone (header,
// then the ovumcy_tz cookie) and refreshes the cookie when the header names a
// new zone. Its sole caller is authenticateRequest, on that function's success
// path — see the WEB-35 note on LanguageMiddleware for why the header is not
// parsed any earlier, and keep it there rather than on any one middleware: a
// session verified outside AuthRequired is still a verified session.
func (handler *Handler) resolveOwnerRequestTimezone(c fiber.Ctx) {
	requestLocation, timezoneCookieValue := resolveRequestLocation(
		c.Get(timezoneHeaderName),
		c.Cookies(timezoneCookieName),
		handler.location,
	)
	if timezoneCookieValue != "" && strings.TrimSpace(c.Cookies(timezoneCookieName)) != timezoneCookieValue {
		handler.setTimezoneCookie(c, timezoneCookieValue)
	}
	c.Locals(contextLocationKey, requestLocation)
}

// resolveRequestLanguage picks the owner's language from the request alone:
// the explicit language cookie when present, otherwise the negotiated
// Accept-Language. Split out of LanguageMiddleware so a response rendered
// BEFORE that middleware runs can resolve the same language without also
// running the timezone half — see ensureRequestMessages.
func (handler *Handler) resolveRequestLanguage(c fiber.Ctx) string {
	if cookieLanguage := c.Cookies(languageCookieName); cookieLanguage != "" {
		return handler.i18n.NormalizeLanguage(cookieLanguage)
	}
	return handler.i18n.DetectFromAcceptLanguage(c.Get("Accept-Language"))
}

// ensureRequestMessages resolves the locale catalogue for a request that has
// not passed LanguageMiddleware.
//
// The edge rate limiters are registered AHEAD of that middleware on purpose: a
// cap has to count requests that never reach a handler, which is the shape of
// an unauthenticated flood. The consequence was that every refusal they
// rendered had no catalogue in its locals, so the shared status fragment fell
// back to the machine key and a rate-limited owner read "too_many_login_attempts"
// in all six languages.
//
// This deliberately does NOT run the whole middleware. Its timezone half can
// load a zoneinfo entry from a client-supplied name, which is precisely the
// per-request work a cap exists to bound — resolving it ahead of the limiter
// would hand a flood the cost the limiter was protecting. Language resolution
// is a cookie read plus a map lookup, and it runs only on a refusal.
func (handler *Handler) ensureRequestMessages(c fiber.Ctx) {
	if len(currentMessages(c)) > 0 {
		return
	}

	language := handler.resolveRequestLanguage(c)
	c.Locals(contextLanguageKey, language)
	c.Locals(contextMessagesKey, handler.i18n.Messages(language))
}

func (handler *Handler) setLanguageCookie(c fiber.Ctx, language string) {
	c.Cookie(&fiber.Cookie{
		Name:     languageCookieName,
		Value:    handler.i18n.NormalizeLanguage(language),
		Path:     "/",
		HTTPOnly: false,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().AddDate(1, 0, 0),
	})
}

// clearLanguageCookie retracts the language cookie with the same attributes
// setLanguageCookie writes, so the browser drops it instead of keeping a value
// whose Set-Cookie differed in path or scope. It is used where a session ends on
// purpose (see clearSessionEndCookies): the cookie is a pre-auth cache of the
// account's preference, and on a shared browser it otherwise tells the next
// visitor that this app was used and in which language.
func (handler *Handler) clearLanguageCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     languageCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: false,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}

// clearTimezoneCookie retracts the timezone cookie with the same attributes
// setTimezoneCookie writes, so the browser drops it rather than keeping a value
// whose Set-Cookie differed in path or scope. It runs beside clearLanguageCookie
// on the deliberate session ends (see clearSessionEndCookies) and for the same
// reason: neither cookie is sealed or session-scoped, and left behind on a
// shared browser ovumcy_tz tells the next visitor which region the previous
// owner lives in — a weaker disclosure than the language, in the same class.
func (handler *Handler) clearTimezoneCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     timezoneCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: false,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().Add(-1 * time.Hour),
	})
}

func (handler *Handler) setTimezoneCookie(c fiber.Ctx, timezone string) {
	c.Cookie(&fiber.Cookie{
		Name:     timezoneCookieName,
		Value:    timezone,
		Path:     "/",
		HTTPOnly: false,
		Secure:   handler.cookieSecure,
		SameSite: "Lax",
		Expires:  time.Now().AddDate(1, 0, 0),
	})
}
