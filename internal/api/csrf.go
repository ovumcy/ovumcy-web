package api

import (
	"errors"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/extractors"
	"github.com/gofiber/fiber/v3/middleware/csrf"
)

// CSRFTokenExtractor builds the Fiber v3 extractor that pulls the CSRF token
// from a request. It accepts the legacy `csrf_token` form field (used by
// browser HTMX submissions) and the `X-CSRF-Token` request header (used by
// non-browser API clients). The form field is checked first because it
// dominates browser traffic; the header is the fallback path for programmatic
// clients that do not send form bodies. Neither source blank yields
// `csrf.ErrTokenNotFound` so the middleware's ErrorHandler can classify the
// rejection as a missing token (Fiber v3's extractors.Chain would instead
// surface extractors.ErrNotFound, which our reason mapping does not recognize).
//
// Fiber v3 replaced v2's `KeyLookup: "form:csrf_token"` + func extractor with a
// typed extractors.Extractor; expressing the chain via extractors.FromCustom
// keeps the exact form-then-header order and the ErrTokenNotFound sentinel.
func CSRFTokenExtractor() extractors.Extractor {
	return extractors.FromCustom("csrf_token", func(c fiber.Ctx) (string, error) {
		// The token is bound from a urlencoded or multipart form body and from
		// no other body type. The query string never carries it, and every
		// other body is skipped: this extractor runs ahead of the body-size
		// guard, and the body binder decodes and content-decodes JSON, XML,
		// CBOR and MsgPack (a vendor "+json" type included), so binding one
		// here would inflate a compressed body before that guard could refuse
		// it and would accept a token from a body type the API never
		// declared. A non-form client sends the header.
		if hasFormBody(c) {
			input := struct {
				Token string `form:"csrf_token"`
			}{}
			if bindRequestBody(c, &input) == nil {
				if token := strings.TrimSpace(input.Token); token != "" {
					return token, nil
				}
			}
		}
		if token := strings.TrimSpace(c.Get("X-CSRF-Token")); token != "" {
			return token, nil
		}
		return "", csrf.ErrTokenNotFound
	})
}

// hasFormBody reports whether the request declares a urlencoded or multipart
// form body. The media type is compared without parameters and without regard
// to case, the way the body binder itself dispatches, so a mixed-case
// declaration that the binder would read as a form is read as one here too.
func hasFormBody(c fiber.Ctx) bool {
	mediaType, _, _ := strings.Cut(c.Get(fiber.HeaderContentType), ";")
	mediaType = strings.TrimSpace(mediaType)
	return strings.EqualFold(mediaType, fiber.MIMEApplicationForm) ||
		strings.EqualFold(mediaType, fiber.MIMEMultipartForm)
}

// CSRFFailureReason maps the error returned by the CSRF middleware into a
// stable, log-safe reason key. Only the documented sentinel errors carry a
// meaningful classification; anything else is collapsed to "csrf rejected"
// so we never leak transport details into the audit log. Fiber v3 renamed the
// referer sentinels (ErrNoReferer/ErrBadReferer -> ErrRefererNotFound/
// ErrRefererInvalid/ErrRefererNoMatch) and added Origin-header sentinels;
// both header families collapse to "invalid referer" to preserve the existing
// log vocabulary.
func CSRFFailureReason(err error) string {
	switch {
	case errors.Is(err, csrf.ErrTokenInvalid):
		return "invalid token"
	case errors.Is(err, csrf.ErrTokenNotFound):
		return "missing token"
	case errors.Is(err, csrf.ErrRefererNotFound),
		errors.Is(err, csrf.ErrRefererInvalid),
		errors.Is(err, csrf.ErrRefererNoMatch),
		errors.Is(err, csrf.ErrOriginInvalid),
		errors.Is(err, csrf.ErrOriginNoMatch):
		return "invalid referer"
	default:
		return "csrf rejected"
	}
}
