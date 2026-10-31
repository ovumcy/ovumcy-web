package api

import "github.com/gofiber/fiber/v3"

type clientKeyLocalsKey struct{}

// ClientKeyMiddleware records, once per request, the client address the edge
// rate limiters key on, so the per-client auth-attempt buckets in
// internal/services key on the same attacker-proof value. c.IP() is not that
// value behind a trusted proxy with PROXY_HEADER=X-Forwarded-For: it returns
// the leftmost, client-supplied entry, so a stranger could name any victim's
// address there and spend that address's login budget for it.
func ClientKeyMiddleware(keyGenerator func(fiber.Ctx) string) fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Locals(clientKeyLocalsKey{}, keyGenerator(c))
		return c.Next()
	}
}

// RequestClientKey exposes requestClientKey to the composition root, whose
// wiring test proves the middleware publishes the edge limiters' key.
func RequestClientKey(c fiber.Ctx) string {
	return requestClientKey(c)
}

// requestClientKey is the key every per-client attempt bucket uses. A request
// that never passed ClientKeyMiddleware (a test app wiring only the routes)
// falls back to c.IP(), which is exact whenever no proxy header is trusted.
func requestClientKey(c fiber.Ctx) string {
	if key, ok := c.Locals(clientKeyLocalsKey{}).(string); ok && key != "" {
		return key
	}
	return c.IP()
}
