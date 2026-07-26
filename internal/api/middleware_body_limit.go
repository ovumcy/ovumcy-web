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
func requestBodyLimitGuard(c fiber.Ctx) error {
	if len(c.Request().Header.ContentEncoding()) == 0 {
		return c.Next()
	}

	statusBeforeProbe := c.Response().StatusCode()
	_ = c.Body()
	statusAfterProbe := c.Response().StatusCode()
	if statusAfterProbe == statusBeforeProbe {
		return c.Next()
	}
	if statusAfterProbe == fiber.StatusRequestEntityTooLarge {
		return RespondRequestEntityTooLarge(c)
	}

	// fiber also stamps a status when it refuses to decode at all (415 for an
	// unknown encoding, 501 for "compress"). Those are a different condition,
	// answered as before by the handler that reads the body; undo the stamp so
	// the probe leaves no trace on the response the handler goes on to write.
	c.Response().ResetBody()
	c.Status(statusBeforeProbe)
	return c.Next()
}
