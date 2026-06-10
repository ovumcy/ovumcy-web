package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func TestTrustedProxyMatcher(t *testing.T) {
	// "  " trims to empty and is skipped; "bogus/cidr/x" looks like a CIDR but
	// fails to parse and is dropped rather than panicking.
	matcher := newTrustedProxyMatcher([]string{"203.0.113.7", "10.0.0.0/8", "2001:db8::/32", "  ", "bogus/cidr/x"})

	cases := []struct {
		ip   string
		want bool
	}{
		{"203.0.113.7", true},   // exact IPv4 match
		{"203.0.113.8", false},  // not listed
		{"10.255.1.1", true},    // inside the IPv4 CIDR
		{"11.0.0.1", false},     // outside the IPv4 CIDR
		{"2001:db8::99", true},  // inside the IPv6 CIDR
		{"2001:dead::1", false}, // outside the IPv6 CIDR
	}
	for _, tc := range cases {
		if got := matcher.contains(net.ParseIP(tc.ip)); got != tc.want {
			t.Errorf("contains(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
	if matcher.contains(nil) {
		t.Error("contains(nil) must be false")
	}
}

func TestRightmostUntrustedIP(t *testing.T) {
	// Trust boundary: a single front proxy (203.0.113.7) plus a 10.0.0.0/8
	// internal range. The direct socket peer is NOT part of the X-Forwarded-For
	// list, so the chain holds only forwarded hops, leftmost = client-supplied.
	trusted := newTrustedProxyMatcher([]string{"203.0.113.7", "10.0.0.0/8"})

	cases := []struct {
		name      string
		forwarded []string
		want      string
	}{
		{"rightmost untrusted after a trusted hop", []string{"198.51.100.1", "192.0.2.55", "203.0.113.7"}, "192.0.2.55"},
		{"skips multiple trusted hops", []string{"192.0.2.55", "203.0.113.7", "10.1.2.3"}, "192.0.2.55"},
		{"spoofed trusted-looking prefix is ignored", []string{"203.0.113.7", "192.0.2.55", "203.0.113.7"}, "192.0.2.55"},
		{"all hops trusted yields empty", []string{"203.0.113.7", "10.9.9.9"}, ""},
		{"empty list yields empty", nil, ""},
		{"invalid rightmost token is skipped", []string{"192.0.2.55", "not-an-ip"}, "192.0.2.55"},
		{"untrusted IPv6 hop", []string{"2001:db8::1"}, "2001:db8::1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rightmostUntrustedIP(tc.forwarded, trusted); got != tc.want {
				t.Fatalf("rightmostUntrustedIP(%#v) = %q, want %q", tc.forwarded, got, tc.want)
			}
		})
	}
}

// TestRateLimitKeyGeneratorBucketing drives the generator through the real fiber
// limiter to assert how requests share (or split) per-IP buckets. fiber's
// app.Test connection always reports a 0.0.0.0 socket peer, so cases that need a
// trusted peer list "0.0.0.0" in TRUSTED_PROXIES; cases that need an untrusted
// peer list something else. With Max=2 the third request reveals the bucketing:
// a shared key trips (429), distinct keys do not.
func TestRateLimitKeyGeneratorBucketing(t *testing.T) {
	const maxReqs = 2

	cases := []struct {
		name       string
		proxy      proxySettings
		headerName string
		values     []string // one header value per sequential request
		wantTrip   bool     // expect the final request to be 429
		explain    string
	}{
		{
			name:       "spoofed X-Forwarded-For prefix shares one bucket",
			proxy:      proxySettings{Enabled: true, Header: "X-Forwarded-For", TrustedProxies: []string{"0.0.0.0"}},
			headerName: "X-Forwarded-For",
			// Rotating spoofed leftmost entries with a fixed real client (the
			// rightmost hop a trusted proxy would have appended).
			values:   []string{"198.51.100.1, 203.0.113.7", "198.51.100.2, 203.0.113.7", "198.51.100.3, 203.0.113.7"},
			wantTrip: true,
			explain:  "rotating a spoofed X-Forwarded-For prefix must not mint fresh buckets",
		},
		{
			name:       "distinct real clients get distinct buckets",
			proxy:      proxySettings{Enabled: true, Header: "X-Forwarded-For", TrustedProxies: []string{"0.0.0.0"}},
			headerName: "X-Forwarded-For",
			// Fixed spoofed prefix, rotating the rightmost (real) client.
			values:   []string{"198.51.100.9, 203.0.113.1", "198.51.100.9, 203.0.113.2", "198.51.100.9, 203.0.113.3"},
			wantTrip: false,
			explain:  "the key must follow the rightmost untrusted (real) client, so distinct clients do not collide",
		},
		{
			name:       "untrusted direct peer ignores X-Forwarded-For",
			proxy:      proxySettings{Enabled: true, Header: "X-Forwarded-For", TrustedProxies: []string{"10.10.10.10"}},
			headerName: "X-Forwarded-For",
			// Peer (0.0.0.0) is not trusted, so the forwarded header is ignored
			// and every request keys on the socket peer.
			values:   []string{"203.0.113.1, 203.0.113.7", "203.0.113.2, 203.0.113.7", "203.0.113.3, 203.0.113.7"},
			wantTrip: true,
			explain:  "an untrusted direct peer must key on the socket peer and ignore X-Forwarded-For",
		},
		{
			name:       "proxy support disabled ignores X-Forwarded-For",
			proxy:      proxySettings{Enabled: false, Header: "X-Forwarded-For", TrustedProxies: []string{"0.0.0.0"}},
			headerName: "X-Forwarded-For",
			values:     []string{"203.0.113.1", "203.0.113.2", "203.0.113.3"},
			wantTrip:   true,
			explain:    "with proxy support off every request must key on the socket peer",
		},
		{
			name:       "trusted single-value header keys on its value",
			proxy:      proxySettings{Enabled: true, Header: "X-Real-IP", TrustedProxies: []string{"0.0.0.0"}},
			headerName: "X-Real-IP",
			values:     []string{"203.0.113.5", "203.0.113.5", "203.0.113.5"},
			wantTrip:   true,
			explain:    "a trusted single-value header (proxy-overwritten) keys on its value",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New(fiberConfig(tc.proxy))
			app.Use(limiter.New(limiter.Config{
				Max:          maxReqs,
				Expiration:   time.Minute,
				KeyGenerator: rateLimitKeyGenerator(tc.proxy),
			}))
			app.Get("/probe", func(c *fiber.Ctx) error {
				return c.SendStatus(fiber.StatusNoContent)
			})

			lastStatus := 0
			for i, value := range tc.values {
				req := httptest.NewRequest(http.MethodGet, "/probe", nil)
				req.Header.Set(tc.headerName, value)
				resp, err := app.Test(req, -1)
				if err != nil {
					t.Fatalf("request %d failed: %v", i, err)
				}
				lastStatus = resp.StatusCode
				_ = resp.Body.Close()
			}

			tripped := lastStatus == http.StatusTooManyRequests
			if tripped != tc.wantTrip {
				t.Fatalf("%s: final request tripped=%v (status %d), want tripped=%v", tc.explain, tripped, lastStatus, tc.wantTrip)
			}
		})
	}
}
