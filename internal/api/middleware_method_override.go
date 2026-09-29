package api

import (
	"bytes"
	"strings"

	"github.com/gofiber/fiber/v3"
)

// methodOverrideAllowed is the closed set of verbs a form may ask for. Every
// entry is a method CSRF validates and requestMethodCanCarryAReadBody reports
// as body-reading, so an overridden request is never downgraded to a safe
// method that would skip the token check, mint a CSRF cookie or skip the
// decode probe.
var methodOverrideAllowed = map[string]bool{
	fiber.MethodPut:    true,
	fiber.MethodPatch:  true,
	fiber.MethodDelete: true,
}

// MethodOverride routes a browser form POST as the verb its hidden `_method`
// field names, so a settings form whose htmx attribute sends PUT or DELETE
// reaches the same handler when it is submitted without JavaScript.
//
// It acts only on a POST whose declared media type is
// application/x-www-form-urlencoded and that carries no Content-Encoding.
// Everything else passes through untouched: JSON and other bodies, multipart
// (no form in the app declares an enctype), every non-POST verb (htmx already
// sends the real one), and the query string, which is never read. The body is
// bound through the form binder before PostArgs is read: the binder folds
// a mixed-case Content-Type before fasthttp parses and caches the body, which a
// raw PostArgs call would cache as empty and hide csrf_token from CSRF. A body
// that provably names no such field is not bound at all; see
// methodOverrideBodyMayNameTheField.
//
// On a plain urlencoded body, a present field outside the allowlist, empty or
// repeated, is refused with a 400 instead of being ignored, so such a form never
// runs the POST action it did not ask for. That guarantee covers ONLY the plain
// urlencoded body. A multipart or Content-Encoded form POST is never inspected
// and runs as the POST it is, `_method` or not: reading either here would mean
// spooling multipart parts or decompressing ahead of the rate limiters and the
// body-limit guard, and refusing either on shape alone would refuse legitimate
// form POSTs that carry no `_method` (the auth handlers accept multipart, and
// requestBodyLimitGuard exists because compressed bodies are accepted). No form
// in the app can send either shape with an override: none declares an enctype,
// browsers never compress a form, and the templates guard fails on an
// overridden form that gains a multipart enctype.
//
// ORDER IS LOAD-BEARING. The composition root mounts this with app.Use ahead of
// the rate limiters and CSRF, after only other app-wide Use middleware:
//   - CSRF and the method-scoped limiters read c.Method(), so they see the verb
//     the router will run: the token stays mandatory, and a DELETE-scoped
//     budget cannot be dodged by POSTing the same request with _method=DELETE;
//   - fiber resumes routing at the next index of the NEW method's route stack,
//     which is the right place only while every route registered before this
//     one is an app-wide Use present in every stack.
//
// Pinned by the production-assembly tests in cmd/ovumcy.
func MethodOverride(handler *Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		if c.Method() != fiber.MethodPost ||
			requestMediaType(c) != fiber.MIMEApplicationForm ||
			len(c.Request().Header.ContentEncoding()) != 0 {
			return c.Next()
		}

		if !methodOverrideBodyMayNameTheField(c.Body()) {
			return c.Next()
		}

		// The bind only normalizes the Content-Type and parses the body into
		// PostArgs; the field is read from PostArgs itself. The binder collapses
		// keys that differ only in case or encoding (_method and _METHOD,
		// %5Fmethod) into one value, so trusting its result would let a form
		// hide a second, disallowed verb behind the first.
		if err := c.Bind().Form(&struct{}{}); err != nil {
			// An unbindable body is left as the POST it arrived as: the CSRF
			// extractor binds the same way and finds no form token in it either.
			return c.Next()
		}
		var requested []string
		for key, value := range c.Request().PostArgs().All() {
			if bytes.EqualFold(key, []byte(methodOverrideKey)) {
				requested = append(requested, string(value))
			}
		}
		if len(requested) == 0 {
			return c.Next()
		}
		if len(requested) != 1 {
			return refuseMethodOverride(c, handler, "ambiguous")
		}
		verb := strings.ToUpper(strings.TrimSpace(requested[0]))
		if !methodOverrideAllowed[verb] {
			return refuseMethodOverride(c, handler, "not_allowed")
		}
		if c.Method(verb) != verb {
			// Unreachable while the standard verbs stay registered; fail closed
			// rather than run the POST route the form did not ask for.
			return handler.RespondTransportError(c, fiber.StatusInternalServerError)
		}
		handler.LogSecurityEvent(c, "method_override", "applied", SecurityEventField{Key: "verb", Value: verb})
		return c.Next()
	}
}

// methodOverrideKey is the form field MethodOverride reads.
const methodOverrideKey = "_method"

// methodOverrideBodyMayNameTheField reports whether an urlencoded body could
// bind to the override field, so the common body without one skips the bind.
// It must never answer false for a body the binder would read the field from:
//   - the binder matches form keys to the tag case-insensitively (fiber binder
//     mapping.go lower-cases the field name, gofiber/schema cache.go lower-cases
//     the lookup), so the literal search folds ASCII case too;
//   - a percent-encoded key (%5Fmethod, _m%65thod) only decodes to the field
//     name inside the binder, so any '%' byte makes the answer true.
func methodOverrideBodyMayNameTheField(body []byte) bool {
	if bytes.IndexByte(body, '%') >= 0 {
		return true
	}
	for start := 0; start+len(methodOverrideKey) <= len(body); start++ {
		if bytes.EqualFold(body[start:start+len(methodOverrideKey)], []byte(methodOverrideKey)) {
			return true
		}
	}
	return false
}

func refuseMethodOverride(c fiber.Ctx, handler *Handler, reason string) error {
	handler.LogSecurityEvent(c, "method_override", "denied", SecurityEventField{Key: "reason", Value: reason})
	return handler.RespondTransportError(c, fiber.StatusBadRequest)
}
