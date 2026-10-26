package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

// A request head — start line plus every header, cookies included — must fit
// fasthttp's read buffer (4 KB by default). Ovumcy's own cookies do not fill it:
// ~450 B on a normal signed-in request, measured from the running app. Summing
// every cookie the app can define reaches ~2.9 KB, but those states are mutually
// exclusive by lifecycle, so a real flow never approaches the limit. A domain
// shared with other cookie-setting services can.
//
// This must be driven over a REAL listener. app.Test feeds an in-memory
// connection and surfaces the read error to the caller, so it never reaches
// fiber's server error path — the very path under test here.
func TestOversizedRequestHeadIsRejectedVisibly(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var logged bytes.Buffer
	log.SetOutput(&logged)

	app := newCSRFGuardTestApp(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	time.Sleep(300 * time.Millisecond)

	address := listener.Addr().String()

	// Positive anchor: a head comfortably inside the buffer still serves the page.
	// Without it, the rejection below is equally satisfied by a server that
	// refuses everything.
	status, _, anchorHeaders := requestWithCookieOfSize(t, address, 2000)
	if status != http.StatusOK {
		t.Fatalf("anchor failed: a 2000-byte cookie must still be served, got %d", status)
	}
	if len(anchorHeaders.Values("Set-Cookie")) == 0 {
		t.Fatalf("anchor failed: the served login page must set a cookie, or the 431 check below proves nothing")
	}

	var body string
	var headers http.Header
	status, body, headers = requestWithCookieOfSize(t, address, 5000)
	if status != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("expected 431 for a head past the read buffer, got %d (body %q)", status, body)
	}

	// A rejection is still a response: it must carry the same hardening as any
	// other, or the one reply an unauthenticated stranger can always elicit is
	// also the one without the headers. nosniff matters here specifically —
	// the body is JSON delivered to a browser that asked for a page.
	for header, want := range map[string]string{
		"X-Content-Type-Options": "nosniff",
		"Cache-Control":          "no-store",
	} {
		if got := headers.Get(header); got != want {
			t.Fatalf("431 response %s = %q, want %q — error responses must keep the standard hardening", header, got, want)
		}
	}

	// The head never parsed, yet fiber still runs the app-wide middleware as an
	// empty "GET /", and the CSRF middleware mints a cookie on it. A cookie on
	// this answer belongs to a request that does not exist.
	if cookies := headers.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("431 response must set no cookie, got %q", cookies)
	}

	// The client gets the stable key, not fiber's bare English string.
	var envelope struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &envelope); err != nil {
		t.Fatalf("expected a JSON error envelope, got %q: %v", body, err)
	}
	if envelope.Error != "request_headers_too_large" {
		t.Fatalf("error key = %q, want request_headers_too_large", envelope.Error)
	}

	// The operator must be able to find it. The request logger records this as
	// "404 | GET | /" because the head never parsed, so the explicit line is the
	// only thing tying the user's 431 to the server side.
	time.Sleep(200 * time.Millisecond)
	if !strings.Contains(logged.String(), "431 request header fields too large") {
		t.Fatalf("expected an explicit 431 log line, got %q", logged.String())
	}
}

// The test above measures the limit through whatever default the pinned Fiber
// release ships, so on a release whose default equals the documented 4 KB it
// cannot tell a pinned buffer from an unpinned one. This one can: the operator
// documentation states 4 KB, and a framework default that moves (Fiber 3.6
// doubled it) must not move the limit with it.
func TestFiberConfigPinsTheDocumentedReadBuffer(t *testing.T) {
	t.Parallel()

	const documentedRequestHeadBytes = 4096
	if got := fiberConfig(proxySettings{}, nil).ReadBufferSize; got != documentedRequestHeadBytes {
		t.Fatalf("fiberConfig ReadBufferSize = %d, want the documented %d — an unset value takes the framework default", got, documentedRequestHeadBytes)
	}
}

// Every head fasthttp cannot parse — not only one past the buffer — reaches the
// error handler the same way: fiber resets the head and runs the app-wide
// middleware on it as an empty "GET /". A malformed head (400) must set no
// cookie either; a fix keyed on the 431 status alone leaves this one minting.
func TestMalformedRequestHeadSetsNoCookie(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	log.SetOutput(io.Discard)

	app := newCSRFGuardTestApp(t)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = app.Listener(listener, fiber.ListenConfig{DisableStartupMessage: true}) }()
	t.Cleanup(func() { _ = app.Shutdown() })
	time.Sleep(300 * time.Millisecond)

	address := listener.Addr().String()
	status, body, headers := sendRawRequest(t, address,
		"GET /login HTTP/1.1\r\nHost: "+address+"\r\nthis line is no header\r\nConnection: close\r\n\r\n")
	if status != http.StatusBadRequest {
		t.Fatalf("expected 400 for a malformed head, got %d (body %q)", status, body)
	}
	if cookies := headers.Values("Set-Cookie"); len(cookies) != 0 {
		t.Fatalf("400 for a head that never parsed must set no cookie, got %q", cookies)
	}
}

// The cookie drop is keyed on the reset head, not on an error status: a parsed
// request refused with 413 renders a page whose form token rides the cookie
// set on that same answer, so the error handler must leave it in place.
func TestParsedRequestErrorKeepsItsCookie(t *testing.T) {
	app := fiber.New(fiberConfig(proxySettings{}, newRateLimitTestHandler(t)))
	app.Get("/probe-parsed-413", func(c fiber.Ctx) error {
		c.Cookie(&fiber.Cookie{Name: "ovumcy_probe", Value: "kept"})
		return fiber.ErrRequestEntityTooLarge
	})

	request := httptest.NewRequest(http.MethodGet, "/probe-parsed-413", nil)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", response.StatusCode)
	}
	for _, cookie := range response.Header.Values("Set-Cookie") {
		if strings.HasPrefix(cookie, "ovumcy_probe=kept") {
			return
		}
	}
	t.Fatalf("a parsed request's 413 must keep the cookie set on it, got %q", response.Header.Values("Set-Cookie"))
}

func requestWithCookieOfSize(t *testing.T, address string, size int) (int, string, http.Header) {
	t.Helper()

	return sendRawRequest(t, address, fmt.Sprintf(
		"GET /login HTTP/1.1\r\nHost: %s\r\nAccept-Language: en\r\nCookie: ovumcy_probe=%s\r\nConnection: close\r\n\r\n",
		address, strings.Repeat("A", size),
	))
}

func sendRawRequest(t *testing.T, address, raw string) (int, string, http.Header) {
	t.Helper()

	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := conn.Write([]byte(raw)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	body := make([]byte, 512)
	read, _ := response.Body.Read(body)
	return response.StatusCode, string(body[:read]), response.Header
}
