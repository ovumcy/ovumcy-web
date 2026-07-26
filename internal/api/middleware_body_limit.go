package api

import "github.com/gofiber/fiber/v3"

// requestBodyLimitGuard turns an over-limit *compressed* request body into the
// mapped 413, before any handler reads it.
//
// Why this is needed. The configured BodyLimit bounds the wire body, and
// fasthttp rejects an over-limit wire body while reading the request — that
// rejection surfaces as a fiber error and is answered by the top-level
// ErrorHandler through RespondRequestEntityTooLarge. But a body carrying
// Content-Encoding is decoded lazily, inside fiber's body accessor, and the
// SAME limit is applied to the DECODED size (fiber v3 req.go tryDecodeBodyInOrder
// → fasthttp Request.BodyGunzipWithLimit → copyZeroAllocWithLimit, which returns
// fasthttp.ErrBodyTooLarge once the decoded stream passes the limit). By then the
// request is already routed and the handler is running.
//
// The seam provides no error return: fiber's Body() swallows the decode error,
// stamps StatusRequestEntityTooLarge on the response via SendStatus, and hands
// the caller []byte(err.Error()) — the literal text "body size exceeds the given
// limit" — in place of the payload. So the observable overflow signal is the
// status fiber stamps on the response while the body accessor runs. Two things
// went wrong downstream of that substitution:
//   - a handler that parses the body reports a domain error about the
//     substituted text (the JSON restore answered "400 invalid import file",
//     blaming the file for what is a size rejection) and its status overwrites
//     the stamped 413;
//   - a handler that reads the body without writing its own response leaves
//     SendStatus's output in place, so the client gets fiber's bare-text
//     "Request Entity Too Large" instead of the app-wide error envelope.
//
// Running the probe once, at the transport edge, fixes both at the same point
// and keeps every body-reading endpoint consistent: the request is rejected with
// the same mapped envelope as the wire-level 413, and no handler — and therefore
// no service — ever receives the substituted bytes.
//
// Requests without Content-Encoding skip the probe entirely: their wire size and
// decoded size are the same number, already bounded before routing. A compressed
// request that passes the probe is decoded once more by whatever reads its body,
// which is a deliberate trade: the request is left byte-for-byte as it arrived
// (no rewritten body, no dropped header) and only a client that opts into
// compressing its upload pays for it.
//
// Requests whose method never reaches a body-reading handler skip it as well —
// see requestMethodCanCarryAReadBody. The probe is the decompression, so running
// it where nothing would ever have decompressed turns a small compressed body
// into a large decompressed one for free: a highly compressible payload sized to
// stay just inside the cap costs BodyLimit bytes of allocation on a route that
// reads nothing, on the cheapest unauthenticated surfaces the app has.
func requestBodyLimitGuard(c fiber.Ctx) error {
	if !requestMethodCanCarryAReadBody(c.Method()) {
		return c.Next()
	}
	if len(c.Request().Header.ContentEncoding()) == 0 {
		return c.Next()
	}

	statusBeforeProbe := c.Response().StatusCode()
	_ = c.Body()
	statusAfterProbe := c.Response().StatusCode()
	// The overflow test comes first, and is an equality against 413 rather than
	// "the status changed": were it second, a 413 already standing on the
	// response would make statusAfterProbe == statusBeforeProbe and hand the
	// over-limit body — fiber's substituted error string — to the handler. No
	// middleware upstream of this one stamps 413 today, so the guard would be
	// resting on that staying true. Ordered this way it fails closed instead: a
	// 413 on the response once the probe returns is answered as one, whether the
	// probe or something upstream put it there, because nothing here can tell
	// the two apart.
	if statusAfterProbe == fiber.StatusRequestEntityTooLarge {
		return RespondRequestEntityTooLarge(c)
	}
	if statusAfterProbe == statusBeforeProbe {
		return c.Next()
	}

	// fiber also stamps a status when it refuses to decode at all (415 for an
	// unknown encoding, 501 for "compress"). Those are a different condition,
	// answered as before by the handler that reads the body; undo the stamp so
	// the probe leaves no trace on the response the handler goes on to write.
	c.Response().ResetBody()
	c.Status(statusBeforeProbe)
	return c.Next()
}

// requestMethodCanCarryAReadBody reports whether a request with this method can
// reach a handler that reads its body, and therefore whether
// requestBodyLimitGuard has to pay for the decode probe.
//
// The answer is derived from the METHOD, never from a list of paths, and the
// listed exclusions are the closed set of methods whose request body no route in
// this app reads: their bodies have no defined semantics (RFC 9110 §9.3) and
// every handler registered under them takes its input from the path, the query
// string, or a cookie. Everything else is covered, so a route added later is
// guarded the moment it is registered — nothing has to be added here, and there
// is no per-route list that can silently fall out of date.
//
// DELETE is deliberately absent from the exclusions even though its body is just
// as undefined: account deletion and 2FA disable both read a password out of it.
// That is why this is a list of exclusions rather than "the safe methods", and
// why a new exclusion is only ever correct once every route registered under
// that method is known to read nothing.
func requestMethodCanCarryAReadBody(method string) bool {
	switch method {
	case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions, fiber.MethodTrace, fiber.MethodConnect:
		return false
	default:
		return true
	}
}
