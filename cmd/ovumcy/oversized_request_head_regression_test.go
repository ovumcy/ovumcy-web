package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
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
	if status, _, _ := requestWithCookieOfSize(t, address, 2000); status != http.StatusOK {
		t.Fatalf("anchor failed: a 2000-byte cookie must still be served, got %d", status)
	}

	status, body, headers := requestWithCookieOfSize(t, address, 5000)
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

func requestWithCookieOfSize(t *testing.T, address string, size int) (int, string, http.Header) {
	t.Helper()

	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	raw := fmt.Sprintf(
		"GET /login HTTP/1.1\r\nHost: %s\r\nAccept-Language: en\r\nCookie: ovumcy_probe=%s\r\nConnection: close\r\n\r\n",
		address, strings.Repeat("A", size),
	)
	if _, err := conn.Write([]byte(raw)); err != nil {
		t.Fatalf("write request: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("set deadline: %v", err)
	}

	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read response for a %d-byte cookie: %v", size, err)
	}
	defer func() { _ = response.Body.Close() }()

	body := make([]byte, 512)
	read, _ := response.Body.Read(body)
	return response.StatusCode, string(body[:read]), response.Header
}
