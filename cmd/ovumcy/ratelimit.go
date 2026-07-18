package main

import (
	"log"
	"net"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
)

// trustedProxyMatcher classifies an address as a trusted proxy using the same
// rules fiber applies when building its own trusted set (App.handleTrustedProxy):
// a TRUSTED_PROXIES entry containing "/" is parsed as a CIDR range, anything
// else is matched by exact net.IP string. Keeping our own copy lets the rate-
// limit key generator reuse fiber's exact trust boundary, so an address we treat
// as "a proxy hop" is one fiber would also have trusted as the peer.
type trustedProxyMatcher struct {
	exact  map[string]struct{}
	ranges []*net.IPNet
}

func newTrustedProxyMatcher(entries []string) trustedProxyMatcher {
	matcher := trustedProxyMatcher{exact: make(map[string]struct{}, len(entries))}
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			if _, ipNet, err := net.ParseCIDR(entry); err == nil {
				matcher.ranges = append(matcher.ranges, ipNet)
			}
			continue
		}
		matcher.exact[entry] = struct{}{}
	}
	return matcher
}

func (m trustedProxyMatcher) contains(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if _, ok := m.exact[ip.String()]; ok {
		return true
	}
	for _, ipNet := range m.ranges {
		if ipNet.Contains(ip) {
			return true
		}
	}
	return false
}

// rightmostUntrustedIP walks an X-Forwarded-For chain from the right (the hop
// closest to us, appended by our own trusted proxy) toward the left and returns
// the first address that is not itself a trusted proxy: the real client as seen
// from the edge of our trusted chain. Entries further left are client-supplied
// and spoofable, so they are ignored. Returns "" when every hop is trusted or
// the list is empty, letting the caller fall back to the direct peer.
func rightmostUntrustedIP(forwarded []string, trusted trustedProxyMatcher) string {
	for i := len(forwarded) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(forwarded[i]))
		if ip != nil && !trusted.contains(ip) {
			return ip.String()
		}
	}
	return ""
}

// rateLimitKeyGenerator builds the per-request key for fiber's rate limiters.
//
// Fiber's default key is c.IP(). With ProxyHeader=X-Forwarded-For and a trusted
// peer, c.IP() returns the LEFTMOST X-Forwarded-For token — the value the
// original client supplied — so an attacker behind an appending proxy can rotate
// that token to mint a fresh rate-limit bucket per request and defeat the limit
// entirely. These edge limiters key on IP alone (unlike the per-identity auth
// limiters in internal/services, which also bucket on email/user id), so the IP
// key must be attacker-proof on its own.
//
// The key is derived from our trust boundary outward:
//   - Proxy support off, or the direct peer is not a trusted proxy: the only
//     attacker-proof address is the socket peer, so key on it and ignore every
//     forwarded header (all client-controlled in that position).
//   - Peer is a trusted proxy and the header is X-Forwarded-For: key on the
//     rightmost untrusted hop (see rightmostUntrustedIP); fall back to the peer
//     when every hop is trusted.
//   - Peer is a trusted proxy and the header is a single-value header the proxy
//     overwrites (for example X-Real-IP): defer to fiber's parsed c.IP().
func rateLimitKeyGenerator(proxy proxySettings) func(fiber.Ctx) string {
	trusted := newTrustedProxyMatcher(proxy.TrustedProxies)
	headerIsXForwardedFor := strings.EqualFold(strings.TrimSpace(proxy.Header), fiber.HeaderXForwardedFor)
	return func(c fiber.Ctx) string {
		peer := c.RequestCtx().RemoteIP()
		if !proxy.Enabled || !trusted.contains(peer) {
			return peer.String()
		}
		if !headerIsXForwardedFor {
			return c.IP()
		}
		if client := rightmostUntrustedIP(c.IPs(), trusted); client != "" {
			return client
		}
		return peer.String() // codecov:ignore -- main() IP-resolution fallback; exercised by e2e
	}
}

// proxyHeaderRateLimitWarning returns a non-empty operator note when trust-proxy
// is enabled but PROXY_HEADER resolves to X-Forwarded-For. The edge rate limiters
// no longer trust that header blindly — rateLimitKeyGenerator keys on the
// rightmost untrusted X-Forwarded-For hop, so a spoofed prefix cannot defeat
// them. The residual is that fiber's c.IP() still returns the client-supplied
// leftmost token; c.IP() only feeds the secondary per-client auth-attempt
// buckets in internal/services, which sit behind spoof-proof per-identity
// buckets, so brute-force protection holds. PROXY_HEADER should still name a
// header the proxy overwrites with the real client IP (for example X-Real-IP)
// for spoof-proof c.IP() values.
func proxyHeaderRateLimitWarning(proxy proxySettings) string {
	if proxy.Enabled && strings.EqualFold(strings.TrimSpace(proxy.Header), fiber.HeaderXForwardedFor) {
		return "NOTE: TRUST_PROXY_ENABLED=true with PROXY_HEADER=X-Forwarded-For — the rate limiters now key on the rightmost untrusted X-Forwarded-For hop and are not spoofable, but fiber's c.IP() still returns the client-supplied leftmost entry. c.IP() only feeds the secondary per-client auth-attempt buckets (the per-identity buckets that actually cap brute force are unaffected). Set PROXY_HEADER to a header your proxy overwrites with the real client IP (for example X-Real-IP) for spoof-proof c.IP() values."
	}
	return ""
}

type authRateLimitConfig struct {
	ErrorCode string
}

func newAuthRateLimitHandler(handler *api.Handler, config authRateLimitConfig) fiber.Handler {
	return func(c fiber.Ctx) error {
		logRateLimitHit(c)
		handler.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: "auth"},
			api.SecurityEventField{Key: "reason", Value: config.ErrorCode},
		)
		return handler.RespondAuthRateLimited(c, config.ErrorCode)
	}
}

func newAPIRateLimitHandler(handler *api.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		logRateLimitHit(c)
		handler.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: rateLimitScope(c)},
			api.SecurityEventField{Key: "reason", Value: "too many requests"},
		)
		return handler.RespondAPIRateLimited(c)
	}
}

// newCalendarFeedRateLimitHandler is the LimitReached handler for the per-IP
// calendar-feed limiter. The feed has no UI, so a 429 needs no HTML/JSON body:
// it returns a bare 429 (preserving the limiter-set Retry-After header) after
// logging the hit and a security event. logRateLimitHit masks the token in the
// path via SafeRequestLogPath, so the rate-limit log line never carries the
// token value.
func newCalendarFeedRateLimitHandler(handler *api.Handler) fiber.Handler {
	return func(c fiber.Ctx) error {
		logRateLimitHit(c)
		handler.LogSecurityEvent(c, "rate_limit", "blocked",
			api.SecurityEventField{Key: "scope", Value: "calendar_feed"},
			api.SecurityEventField{Key: "reason", Value: "too many requests"},
		)
		return c.SendStatus(fiber.StatusTooManyRequests)
	}
}

func rateLimitScope(c fiber.Ctx) string {
	path := c.Path()
	switch {
	case strings.HasPrefix(path, "/api/v1/users/current"):
		return "settings"
	case isV1AuthPath(path), strings.HasPrefix(path, "/auth/oidc"):
		return "auth"
	default:
		return "api"
	}
}

// isV1AuthPath returns true for the v1 auth surface (sessions, users
// creation, password-reset flow). Used by rateLimitScope to classify rate-
// limit events so the security log preserves the "auth" scope across the
// /api/v1/* migration.
func isV1AuthPath(path string) bool {
	switch path {
	case "/api/v1/users", "/api/v1/sessions", "/api/v1/sessions/current",
		"/api/v1/sessions/2fa-challenge", "/api/v1/password-resets",
		"/api/v1/password-resets/redeem":
		return true
	}
	return false
}

// rateLimitOnlyFor returns a Next predicate for fiber's limiter middleware
// that lets the limiter run only when the request's method and path match
// exactly. Fiber's Use() is path-prefix-matched and method-agnostic; without
// this filter a limiter wired to "/api/v1/sessions" would also fire on
// sibling routes such as POST /api/v1/sessions/2fa-challenge that share the
// prefix, silently broadening the rate-limit budget.
func rateLimitOnlyFor(method, path string) func(fiber.Ctx) bool {
	return func(c fiber.Ctx) bool {
		return c.Method() != method || c.Path() != path
	}
}

func logRateLimitHit(c fiber.Ctx) {
	retryAfter := strings.TrimSpace(string(c.Response().Header.Peek(fiber.HeaderRetryAfter)))
	if retryAfter == "" {
		retryAfter = "unknown"
	}

	log.Printf("rate limit reached: method=%s path=%s retry_after=%s", c.Method(), api.SafeRequestLogPath(c), retryAfter)
}
