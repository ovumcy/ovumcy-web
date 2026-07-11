package services

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeResolver is the injected ipResolver seam: it maps hostnames to fixed
// answers so the resolve-and-check dialer can be tested without live DNS.
type fakeResolver struct {
	hosts map[string][]net.IPAddr
}

func (r fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	if addrs, ok := r.hosts[host]; ok {
		return addrs, nil
	}
	return nil, &net.DNSError{Err: "not found", Name: host, IsNotFound: true}
}

// resolverFor builds a fakeResolver mapping a single hostname to the given IPs.
func resolverFor(host string, ips ...string) fakeResolver {
	addrs := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, net.IPAddr{IP: net.ParseIP(ip)})
	}
	return fakeResolver{hosts: map[string][]net.IPAddr{host: addrs}}
}

// hostnameTargetFor rewrites a server URL to use hostname (keeping its port and
// adding a path), so a test can drive delivery through the injected resolver
// instead of the server's literal IP.
func hostnameTargetFor(t *testing.T, serverURL, hostname string) string {
	t.Helper()
	parsed, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: net.JoinHostPort(hostname, parsed.Port()), Path: "/hook"}).String()
}

// samplePayload is a representative reminder payload for the delivery tests.
func samplePayload() WebhookPayload {
	return WebhookPayload{
		Title:      "Period reminder",
		Message:    "Estimated next period around 2026-03-12.",
		Disclaimer: "These are estimates, not medical advice or a method of contraception.",
		Type:       DueReminderTypePeriod,
		EventDate:  "2026-03-12",
		LeadDays:   3,
	}
}

// captureLogOutput redirects the standard logger for the duration of fn and
// returns everything written, so a test can assert what a delivery logged.
func captureLogOutput(t *testing.T, fn func()) string {
	t.Helper()
	var buffer bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	}()
	fn()
	return buffer.String()
}

func TestWebhookDeliverySucceedsOn2xx(t *testing.T) {
	var received atomic.Bool
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		received.Store(true)
		gotBody, _ = io.ReadAll(request.Body)
		if request.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %q", request.Header.Get("Content-Type"))
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	deliverer := NewWebhookDeliverer(false)
	if err := deliverer.Deliver(context.Background(), server.URL, samplePayload()); err != nil {
		t.Fatalf("expected success on 2xx, got %v", err)
	}
	if !received.Load() {
		t.Fatal("server never received the request")
	}

	var decoded WebhookPayload
	if err := json.Unmarshal(gotBody, &decoded); err != nil {
		t.Fatalf("body was not valid JSON: %v", err)
	}
	if decoded.Disclaimer == "" {
		t.Fatal("payload must carry the mandatory disclaimer")
	}
}

func TestWebhookDeliveryFailsOnNon2xx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	deliverer := NewWebhookDeliverer(false)
	err := deliverer.Deliver(context.Background(), server.URL, samplePayload())
	if err == nil {
		t.Fatal("expected error on 500, got nil")
	}
}

// TestWebhookDeliveryRefusesRedirect proves the zero-redirect policy: a 302 to
// another location must fail delivery, so a redirect can never steer the request
// (or its body) to a second unvalidated origin.
func TestWebhookDeliveryRefusesRedirect(t *testing.T) {
	var secondaryHit atomic.Bool
	secondary := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		secondaryHit.Store(true)
		writer.WriteHeader(http.StatusOK)
	}))
	defer secondary.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		http.Redirect(writer, &http.Request{}, secondary.URL, http.StatusFound)
	}))
	defer redirector.Close()

	deliverer := NewWebhookDeliverer(false)
	err := deliverer.Deliver(context.Background(), redirector.URL, samplePayload())
	if err == nil {
		t.Fatal("expected a redirect to be refused, got nil error")
	}
	if secondaryHit.Load() {
		t.Fatal("SSRF: the redirect target was followed; zero-redirect policy breached")
	}
	// The returned error must not embed the full request URL (which could carry a
	// token) — only the host and a reason. The redirector's path must not appear.
	if strings.Contains(err.Error(), redirector.URL) {
		t.Fatalf("returned error leaked the full URL: %q", err.Error())
	}
}

// TestWebhookDeliveryRejectsNonHTTPScheme proves non-http(s) schemes are refused
// at delivery time (defence in depth over slice-1 save-time validation).
func TestWebhookDeliveryRejectsNonHTTPScheme(t *testing.T) {
	deliverer := NewWebhookDeliverer(false)
	for _, target := range []string{
		"ftp://example.test/hook",
		"file:///etc/passwd",
		"gopher://example.test/1",
	} {
		err := deliverer.Deliver(context.Background(), target, samplePayload())
		// Pin the specific scheme-guard sentinel, not just "some error": under a
		// negated scheme check the disallowed scheme slips past the guard and the
		// HTTP client rejects it later with a *different* non-nil error, which a
		// bare err==nil assertion would accept (that is why the guard survived).
		if !errors.Is(err, ErrWebhookDeliveryURLScheme) {
			t.Fatalf("expected scheme %q refused with ErrWebhookDeliveryURLScheme, got %v", target, err)
		}
	}
}

// TestWebhookDeliveryHandlesOversizedBody proves an endpoint that returns a huge
// body does not make us buffer it unbounded: we only need the status, and the
// LimitReader caps the read. A 2xx with a large body still succeeds.
func TestWebhookDeliveryHandlesOversizedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		blob := bytes.Repeat([]byte("x"), 1<<20) // 1 MiB, far beyond the read cap
		_, _ = writer.Write(blob)
	}))
	defer server.Close()

	deliverer := NewWebhookDeliverer(false)
	if err := deliverer.Deliver(context.Background(), server.URL, samplePayload()); err != nil {
		t.Fatalf("expected success on 2xx with oversized body, got %v", err)
	}
}

// TestWebhookDeliveryHonorsContextTimeout proves a slow endpoint is cut off:
// delivery aborts when the caller's context deadline passes.
func TestWebhookDeliveryHonorsContextTimeout(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		<-release // block until the test releases it
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := deliverWithClientTimeout(t, ctx, server.URL)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("expected delivery to abort near the deadline, took %v", elapsed)
	}
}

// deliverWithClientTimeout runs a delivery against a client whose own timeout is
// short, so the slow-endpoint case does not wait the full 10s envelope budget.
func deliverWithClientTimeout(t *testing.T, ctx context.Context, target string) error {
	t.Helper()
	client := &webhookDeliveryClient{
		httpClient: &http.Client{
			Timeout: 200 * time.Millisecond,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return errWebhookRedirect
			},
		},
	}
	return client.Deliver(ctx, target, samplePayload())
}

// TestWebhookDeliveryLogsHostOnly is the no-secret-in-logs headline for
// delivery: on a failure, the captured log must contain the destination HOST but
// NEVER the URL path, query, or userinfo (an ntfy token commonly rides there).
func TestWebhookDeliveryLogsHostOnly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	// Embed a token in userinfo AND query so we can assert neither leaks.
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	parsed.User = url.UserPassword("user", "s3cr3t-token")
	parsed.Path = "/notify/abcdef-secret-topic"
	parsed.RawQuery = "auth=tk_live_51supersecrettoken"
	secretURL := parsed.String()
	host := parsed.Hostname()

	deliverer := NewWebhookDeliverer(false)
	output := captureLogOutput(t, func() {
		_ = deliverer.Deliver(context.Background(), secretURL, samplePayload())
	})

	if !strings.Contains(output, "host="+host) {
		t.Fatalf("log should record the host, got: %q", output)
	}
	for _, secret := range []string{"s3cr3t-token", "tk_live_51supersecrettoken", "abcdef-secret-topic", "auth="} {
		if strings.Contains(output, secret) {
			t.Fatalf("log leaked secret substring %q: %q", secret, output)
		}
	}
}

// TestWebhookDeliveryBlocksPrivateWhenGated proves the off-by-default
// private-address gate: with the gate ON, a loopback literal is refused before
// any request; with the gate OFF (default), the same target is allowed (LAN
// self-hosting is legitimate).
func TestWebhookDeliveryBlocksPrivateWhenGated(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Gate ON: 127.0.0.1 is private/loopback → refused, no request made.
	gated := NewWebhookDeliverer(true)
	if err := gated.Deliver(context.Background(), server.URL, samplePayload()); err == nil {
		t.Fatal("expected private-address delivery to be refused when gated on")
	}
	if hits.Load() != 0 {
		t.Fatal("gated delivery should not have reached the server")
	}

	// Gate OFF (default): the same loopback target is allowed.
	open := NewWebhookDeliverer(false)
	if err := open.Deliver(context.Background(), server.URL, samplePayload()); err != nil {
		t.Fatalf("expected loopback delivery to succeed when gate is off, got %v", err)
	}
	if hits.Load() != 1 {
		t.Fatalf("expected exactly one request when gate is off, got %d", hits.Load())
	}
}

// TestIsPrivateHost pins the private-address classifier used by the gate.
func TestIsPrivateHost(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":       true,
		"10.0.0.5":        true,
		"192.168.1.10":    true,
		"172.16.0.1":      true,
		"169.254.0.1":     true,
		"::1":             true,
		"0.0.0.0":         true,
		"8.8.8.8":         false,
		"93.184.216.34":   false,
		"example.test":    false, // a hostname, not an IP literal
		"ntfy.example.io": false,
		"100.64.0.1":      true,  // RFC 6598 CGNAT literal (net.IP.IsPrivate omits it)
		"64:ff9b::a00:1":  true,  // RFC 6052 NAT64 literal wrapping 10.0.0.1
		"203.0.113.10":    false, // TEST-NET-3 stays external (over-block guard)
	}
	for host, want := range cases {
		if got := isPrivateHost(host); got != want {
			t.Errorf("isPrivateHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// TestWebhookDeliveryBlocksHostnameResolvingToPrivate proves the resolve-and-check
// denylist: with the gate ON, a HOSTNAME (not an IP literal) that resolves to a
// private/loopback address is refused before any request reaches the server, and
// the log records the reason + host only — never the resolved IP.
func TestWebhookDeliveryBlocksHostnameResolvingToPrivate(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const hostname = "attacker.example"
	target := hostnameTargetFor(t, server.URL, hostname)
	// The hostname resolves to the loopback server IP — the trivial bypass the gate
	// must now catch even though the target is expressed as a name.
	deliverer := newWebhookDelivererWithResolver(true, resolverFor(hostname, "127.0.0.1"))

	output := captureLogOutput(t, func() {
		if err := deliverer.Deliver(context.Background(), target, samplePayload()); err == nil {
			t.Fatal("expected hostname resolving to a private address to be refused when gated on")
		}
	})
	if hits.Load() != 0 {
		t.Fatalf("gated delivery reached the server %d time(s); the dial was not blocked", hits.Load())
	}
	if !strings.Contains(output, "reason=private_address_blocked") {
		t.Fatalf("expected private_address_blocked reason, got: %q", output)
	}
	if !strings.Contains(output, "host="+hostname) {
		t.Fatalf("expected host=%s in log, got: %q", hostname, output)
	}
	if strings.Contains(output, "127.0.0.1") {
		t.Fatalf("log leaked the resolved IP: %q", output)
	}
}

// TestWebhookDeliveryAllowsHostnameResolvingToPublic proves the gate does not
// over-block: with the gate ON, a hostname resolving to a PUBLIC address passes
// the private-address guard (it fails later only because the documentation-range
// IP is unroutable). The failure must NOT be the private-address sentinel.
func TestWebhookDeliveryAllowsHostnameResolvingToPublic(t *testing.T) {
	const hostname = "ntfy.example.io"
	// 192.0.2.1 is TEST-NET-1 (RFC 5737): classified public, guaranteed unroutable.
	deliverer := newWebhookDelivererWithResolver(true, resolverFor(hostname, "192.0.2.1"))
	target := "http://" + hostname + "/hook"

	// A short deadline so the unroutable dial aborts quickly instead of waiting the
	// full 10s envelope.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := deliverer.Deliver(ctx, target, samplePayload())
	if err == nil {
		t.Fatal("expected the unroutable public target to fail the dial")
	}
	if errors.Is(err, errWebhookPrivateAddress) {
		t.Fatalf("public-resolving hostname was wrongly blocked as private: %v", err)
	}
}

// TestWebhookDeliveryBlocksMixedPublicPrivateAnswer proves the any-record rule /
// DNS-rebinding defense: if a resolver returns a public AND a private address for
// the same hostname, the whole target is refused before any dial.
func TestWebhookDeliveryBlocksMixedPublicPrivateAnswer(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	const hostname = "rebind.example"
	target := hostnameTargetFor(t, server.URL, hostname)
	// Public first, then the loopback server IP: the guard must reject on the private
	// record regardless of order and never reach the server.
	deliverer := newWebhookDelivererWithResolver(true, resolverFor(hostname, "192.0.2.1", "127.0.0.1"))

	err := deliverer.Deliver(context.Background(), target, samplePayload())
	if err == nil {
		t.Fatal("expected a mixed public/private answer to be refused when gated on")
	}
	if hits.Load() != 0 {
		t.Fatalf("mixed-answer delivery reached the server %d time(s)", hits.Load())
	}
}

// TestWebhookDeliveryBlocksHostnameResolvingToCarrierRanges proves the
// resolve-and-check denylist also covers the two ranges Go's stdlib classifiers
// miss: RFC 6598 CGNAT (100.64.0.0/10, which net.IP.IsPrivate omits) and the RFC
// 6052 NAT64 well-known prefix (64:ff9b::/96) wrapping a private IPv4. With the
// gate ON, a HOSTNAME resolving to either is refused before any destination is
// contacted — reason=private_address_blocked is logged ONLY when the guarded
// dialer returns errWebhookPrivateAddress from its pre-dial check, so that reason
// proves no dial happened — and the log records the reason + host only, never the
// resolved IP. Non-vacuous: against a classifier omitting these ranges the guard
// would pass them and the logged reason would be transport_error.
func TestWebhookDeliveryBlocksHostnameResolvingToCarrierRanges(t *testing.T) {
	cases := map[string]string{
		"cgnat_rfc6598": "100.64.0.1",     // carrier-grade NAT space
		"nat64_rfc6052": "64:ff9b::a00:1", // NAT64 wrapping 10.0.0.1
	}
	for name, resolvedIP := range cases {
		t.Run(name, func(t *testing.T) {
			const hostname = "carrier.example"
			target := "http://" + hostname + "/hook"
			deliverer := newWebhookDelivererWithResolver(true, resolverFor(hostname, resolvedIP))

			// A short deadline bounds the regression path only: a classifier that
			// missed the range would fall through to a real dial of the unroutable
			// IP. The fixed classifier refuses before any dial, so this never waits.
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			output := captureLogOutput(t, func() {
				if err := deliverer.Deliver(ctx, target, samplePayload()); err == nil {
					t.Fatalf("expected hostname resolving to %s to be refused when gated on", resolvedIP)
				}
			})
			if !strings.Contains(output, "reason=private_address_blocked") {
				t.Fatalf("expected private_address_blocked (guard fired before any dial), got: %q", output)
			}
			if !strings.Contains(output, "host="+hostname) {
				t.Fatalf("expected host=%s in log, got: %q", hostname, output)
			}
			if strings.Contains(output, resolvedIP) {
				t.Fatalf("log leaked the resolved IP %q: %q", resolvedIP, output)
			}
		})
	}
}

// TestWebhookDeliveryAllowsHostnameResolvingToTestNet3 is the over-block guard
// for the exact sentinel the suite treats as external: 203.0.113.10 (TEST-NET-3)
// must stay allowed even with the gate ON. It passes the private-address guard
// and fails later only because the documentation-range IP is unroutable — so the
// logged reason must NOT be private_address_blocked (which is what a wrongful
// over-block would record).
func TestWebhookDeliveryAllowsHostnameResolvingToTestNet3(t *testing.T) {
	const hostname = "public.example"
	deliverer := newWebhookDelivererWithResolver(true, resolverFor(hostname, "203.0.113.10"))
	target := "http://" + hostname + "/hook"

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	output := captureLogOutput(t, func() {
		if err := deliverer.Deliver(ctx, target, samplePayload()); err == nil {
			t.Fatal("expected the unroutable TEST-NET-3 target to fail the dial")
		}
	})
	if strings.Contains(output, "reason=private_address_blocked") {
		t.Fatalf("TEST-NET-3 (203.0.113.10) was wrongly blocked as private: %q", output)
	}
}

// TestIsPrivateIP pins the core address classifier shared by the literal
// pre-check and the guarded dialer.
func TestIsPrivateIP(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":    true,
		"10.0.0.5":     true,
		"192.168.1.10": true,
		"172.16.0.1":   true,
		"169.254.0.1":  true, // link-local unicast
		"224.0.0.1":    true, // link-local multicast
		"::1":          true,
		"fe80::1":      true, // IPv6 link-local
		"fc00::1":      true, // IPv6 ULA (private)
		"0.0.0.0":      true, // unspecified
		"::":           true, // IPv6 unspecified
		"8.8.8.8":      false,
		"192.0.2.1":    false, // TEST-NET-1, public-classified
		"2606:4700::1": false, // public IPv6
		// RFC 6598 CGNAT (100.64.0.0/10): internal/carrier space that Go's
		// net.IP.IsPrivate() omits, so the gate must still block it — with the /10
		// boundaries staying public to guard against over-block.
		"100.64.0.1":      true,
		"100.127.255.255": true,
		"100.63.255.255":  false, // just below the /10
		"100.128.0.0":     false, // just above the /10
		// RFC 6052 NAT64 well-known prefix (64:ff9b::/96): classified by the
		// embedded IPv4 (last 4 bytes). A wrapped private / CGNAT v4 is blocked; a
		// wrapped PUBLIC v4 stays allowed (it routes to the public internet).
		"64:ff9b::a00:1":   true,  // wraps 10.0.0.1
		"64:ff9b::6440:1":  true,  // wraps 100.64.0.1 (CGNAT)
		"64:ff9b::808:808": false, // wraps 8.8.8.8 (public)
		// TEST-NET-3 must stay external — the positive delivery path relies on it.
		"203.0.113.10": false,
	}
	for literal, want := range cases {
		ip := net.ParseIP(literal)
		if ip == nil {
			t.Fatalf("test setup: %q did not parse as an IP", literal)
		}
		if got := isPrivateIP(ip); got != want {
			t.Errorf("isPrivateIP(%q) = %v, want %v", literal, got, want)
		}
	}
}

// stubDial is an injectable dialFunc: it records the address it was asked to dial
// and returns a canned conn/error, so guardedDialContext's dial-result branches
// are exercised without a real socket to a non-loopback host.
type stubDial struct {
	lastAddr string
	calls    int
	conn     net.Conn
	err      error
}

func (s *stubDial) dial(_ context.Context, _, addr string) (net.Conn, error) {
	s.calls++
	s.lastAddr = addr
	return s.conn, s.err
}

// TestGuardedDialContext exercises every branch of the resolve-and-check dialer
// directly through injected seams (no live DNS, no routable socket): IP-literal
// vs resolved host, resolver error, empty answer, private rejection, and the dial
// success/failure and malformed-address paths.
func TestGuardedDialContext(t *testing.T) {
	ctx := context.Background()

	t.Run("public literal dials and returns the conn", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		defer func() { _ = server.Close() }()
		stub := &stubDial{conn: client}
		dialFn := guardedDialContext(stub.dial, fakeResolver{})

		conn, err := dialFn(ctx, "tcp", "192.0.2.1:443")
		if err != nil {
			t.Fatalf("expected a public literal to dial, got %v", err)
		}
		if conn != client {
			t.Fatal("guarded dialer did not return the dialed conn")
		}
		if stub.lastAddr != "192.0.2.1:443" {
			t.Fatalf("dialed the wrong address: %q", stub.lastAddr)
		}
	})

	t.Run("private literal is refused before dialing", func(t *testing.T) {
		stub := &stubDial{}
		dialFn := guardedDialContext(stub.dial, fakeResolver{})

		_, err := dialFn(ctx, "tcp", "127.0.0.1:443")
		if !errors.Is(err, errWebhookPrivateAddress) {
			t.Fatalf("expected private-address refusal, got %v", err)
		}
		if stub.calls != 0 {
			t.Fatal("a private literal must not reach the dial")
		}
	})

	t.Run("resolved public host dials the validated IP", func(t *testing.T) {
		client, server := net.Pipe()
		defer func() { _ = client.Close() }()
		defer func() { _ = server.Close() }()
		stub := &stubDial{conn: client}
		dialFn := guardedDialContext(stub.dial, resolverFor("ntfy.example.io", "192.0.2.1"))

		if _, err := dialFn(ctx, "tcp", "ntfy.example.io:443"); err != nil {
			t.Fatalf("expected resolved public host to dial, got %v", err)
		}
		if stub.lastAddr != "192.0.2.1:443" {
			t.Fatalf("expected the resolved IP to be dialed, got %q", stub.lastAddr)
		}
	})

	t.Run("resolver error propagates without dialing", func(t *testing.T) {
		stub := &stubDial{}
		dialFn := guardedDialContext(stub.dial, fakeResolver{}) // empty map → unknown host errors

		if _, err := dialFn(ctx, "tcp", "unknown.example:443"); err == nil {
			t.Fatal("expected the resolver error to surface")
		}
		if stub.calls != 0 {
			t.Fatal("a resolver failure must not reach the dial")
		}
	})

	t.Run("empty resolver answer is refused", func(t *testing.T) {
		stub := &stubDial{}
		dialFn := guardedDialContext(stub.dial, resolverFor("empty.example")) // no IPs

		if _, err := dialFn(ctx, "tcp", "empty.example:443"); err == nil {
			t.Fatal("expected an empty answer to be refused")
		}
		if stub.calls != 0 {
			t.Fatal("an empty answer must not reach the dial")
		}
	})

	t.Run("dial failure surfaces the dial error", func(t *testing.T) {
		wantErr := errors.New("connection refused")
		stub := &stubDial{err: wantErr}
		dialFn := guardedDialContext(stub.dial, fakeResolver{})

		if _, err := dialFn(ctx, "tcp", "192.0.2.1:443"); !errors.Is(err, wantErr) {
			t.Fatalf("expected the dial error to surface, got %v", err)
		}
	})

	t.Run("malformed address surfaces the split error", func(t *testing.T) {
		stub := &stubDial{}
		dialFn := guardedDialContext(stub.dial, fakeResolver{})

		if _, err := dialFn(ctx, "tcp", "no-port-here"); err == nil {
			t.Fatal("expected SplitHostPort to reject a portless address")
		}
		if stub.calls != 0 {
			t.Fatal("a malformed address must not reach the dial")
		}
	})
}

// TestWebhookDeliveryURLParseFailure covers the unparseable-URL branch: a control
// character in the URL makes url.Parse fail, and the error must not echo the URL.
func TestWebhookDeliveryURLParseFailure(t *testing.T) {
	deliverer := NewWebhookDeliverer(false)
	output := captureLogOutput(t, func() {
		if err := deliverer.Deliver(context.Background(), "http://exa\x7fmple.test/\x00secret", samplePayload()); err == nil {
			t.Fatal("expected parse failure, got nil")
		}
	})
	if strings.Contains(output, "secret") {
		t.Fatalf("parse-failure log leaked URL content: %q", output)
	}
}
