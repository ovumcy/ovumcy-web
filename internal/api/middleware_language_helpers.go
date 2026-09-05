package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
)

func (handler *Handler) LanguageMiddleware(c fiber.Ctx) error {
	if IsCalendarFeedRequestPath(c.Path()) {
		// The cookieless calendar feed is authenticated by its path token alone
		// and reads no request language: ServeCalendarFeed resolves "today"
		// from the owner's stored users.timezone
		// (CalendarFeedService.ResolveFeed), and a real calendar client sends
		// none of the language cookie, the timezone header, or the timezone
		// cookie this middleware reads. Running it here would only mint a
		// cookie no reader of the response needs: setTimezoneCookie persists
		// ovumcy_tz for ANY caller naming a zone, cookieless poller included,
		// which is exactly the Set-Cookie the feed's own contract forbids on
		// every outcome (docs/SECURITY_INVARIANTS.md → Calendar feed
		// subscription).
		//
		// Skipping the whole middleware, not only the cookie write, is the
		// deliberate part. It also drops the request-chain fallback (header,
		// then cookie) that an owner whose users.timezone was never captured
		// used to get for the day boundary; the feed now falls through to the
		// instance zone for that owner. Keeping the resolution would keep this
		// route's output dependent on the poller's cookies — the same
		// capability URL rendering a different day in the owner's browser than
		// in the calendar client — on a route whose contract is to read none.
		return c.Next()
	}
	requestLocation, timezoneCookieValue := resolveRequestLocation(
		c.Get(timezoneHeaderName),
		c.Cookies(timezoneCookieName),
		handler.location,
	)
	if timezoneCookieValue != "" && strings.TrimSpace(c.Cookies(timezoneCookieName)) != timezoneCookieValue {
		handler.setTimezoneCookie(c, timezoneCookieValue)
	}

	language := handler.resolveRequestLanguage(c)

	c.Locals(contextLanguageKey, language)
	c.Locals(contextMessagesKey, handler.i18n.Messages(language))
	c.Locals(contextLocationKey, requestLocation)
	return c.Next()
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
