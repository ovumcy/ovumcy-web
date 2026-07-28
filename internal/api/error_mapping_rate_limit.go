package api

import (
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/httpx"
)

// LanguageSwitchPath is the language-switch form's route. Exported because the
// composition root wires this route's own rate limiter (the /api limiter is
// mounted on a prefix that does not reach it) and because a refusal here is
// rendered as a page rather than as an API answer — both need the same literal,
// and a second copy of it is how one of the two silently stops matching.
const LanguageSwitchPath = "/lang"

func authRateLimitErrorSpec(key string) APIErrorSpec {
	normalized := strings.TrimSpace(key)
	if normalized == "" {
		normalized = "too many requests"
	}
	return authFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, normalized)
}

func settingsRateLimitErrorSpec() APIErrorSpec {
	return settingsFormErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many requests")
}

func globalRateLimitErrorSpec() APIErrorSpec {
	return globalErrorSpec(fiber.StatusTooManyRequests, APIErrorCategoryRateLimited, "too many requests")
}

func (handler *Handler) RespondAuthRateLimited(c fiber.Ctx, errorKey string) error {
	return handler.respondRateLimitedMappedError(c, authRateLimitErrorSpec(errorKey))
}

func (handler *Handler) RespondAPIRateLimited(c fiber.Ctx) error {
	switch {
	case isV1AuthFormPath(c.Path()):
		return handler.respondRateLimitedMappedError(c, authRateLimitErrorSpec("too many requests"))
	case strings.HasPrefix(c.Path(), "/api/v1/users/current"):
		return handler.respondRateLimitedMappedError(c, settingsRateLimitErrorSpec())
	case c.Path() == LanguageSwitchPath:
		return handler.respondRateLimitedPageForm(c, globalRateLimitErrorSpec())
	default:
		return handler.respondRateLimitedMappedError(c, globalRateLimitErrorSpec())
	}
}

// RespondCalendarFeedRateLimited answers the per-IP calendar-feed limiter. The
// feed is a machine subscription surface with no page behind it, so it never
// takes the page-form arm — but it is a 429 like every other, and leaving one
// surface answering a bodyless status was the same "one status, two forms"
// split this envelope exists to close. Nothing about the request is echoed: the
// spec is the shared global rate-limit spec, so the token in the path cannot
// reach the body.
func (handler *Handler) RespondCalendarFeedRateLimited(c fiber.Ctx) error {
	return handler.respondRateLimitedMappedError(c, globalRateLimitErrorSpec())
}

// respondRateLimitedMappedError answers a limiter refusal through the SAME
// negotiation as every other mapped error, plus one extension member.
//
// The JSON arm used to build a body of its own — the stable key and
// retry_after_seconds, without error_detail — so the identical refusal arrived
// in two shapes depending on which layer produced it: the edge limiter's
// stripped body on POST /api/v1/sessions, the full envelope from the
// service-level attempt budget behind it. A client had to parse both. It now
// answers the shared envelope with retry_after_seconds added to it, which is
// what an extension member is for: the key and the structured detail are where
// every other rejection puts them, and the seconds ride alongside.
//
// retry_after_seconds is derived from the Retry-After header the limiter has
// already stamped, so it inherits that header's bound — integer seconds, never
// larger than the configured window — rather than exposing a second, separately
// computed view of the same timer.
func (handler *Handler) respondRateLimitedMappedError(c fiber.Ctx, spec APIErrorSpec) error {
	if acceptsJSON(c) {
		return c.Status(spec.Status).JSON(rateLimitedErrorEnvelope(c, spec))
	}
	// The limiters answer before LanguageMiddleware has run, so the catalogue
	// the shared status fragment translates against has to be resolved here or
	// the refusal renders its own machine key as the visible message.
	handler.ensureRequestMessages(c)
	return handler.respondMappedError(c, spec)
}

// respondRateLimitedPageForm answers a refusal on a route whose client is a
// browser rendering a page, not an API client.
//
// The JSON and HTMX arms are identical to every other limiter; only the plain
// HTML arm differs, and it has to. POST /lang is the application's one public
// form with no HTMX and no JavaScript behind it, so a refused language switch
// is a full-page navigation: answering it with the JSON envelope paints the raw
// envelope into the browser window. It renders the same localized status
// fragment an HTMX client receives instead — the copy the owner can read, with
// the stable key beside it. Content type is set explicitly because SendString
// would otherwise label the markup text/plain and the browser would show the
// tags.
func (handler *Handler) respondRateLimitedPageForm(c fiber.Ctx, spec APIErrorSpec) error {
	if responseFormat(c) != httpx.ResponseFormatHTML {
		return handler.respondRateLimitedMappedError(c, spec)
	}
	handler.ensureRequestMessages(c)
	c.Type("html", "utf-8")
	return c.Status(spec.Status).SendString(localizedStatusErrorMarkup(c, spec))
}

func rateLimitedErrorEnvelope(c fiber.Ctx, spec APIErrorSpec) fiber.Map {
	payload := apiErrorEnvelope(spec)
	if retryAfter := retryAfterSeconds(c); retryAfter > 0 {
		payload["retry_after_seconds"] = retryAfter
	}
	return payload
}

func retryAfterSeconds(c fiber.Ctx) int {
	value := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if value == "" {
		return 0
	}
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 1 {
		return 0
	}
	return seconds
}
