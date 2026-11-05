package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/ovumcy/ovumcy-web/internal/api"
)

// TestRequestLoggerFormatCarriesNoClientIdentity pins the operator access log
// to the fields the privacy documentation promises: time, status, latency,
// method, the redacted route template and the redacted error. A client address,
// user agent, raw URL or any header or query value is not among them. The
// calendar-feed telemetry regression in internal/api keeps a copy of this
// format, so a change here that adds a field must be decided on both sides.
func TestRequestLoggerFormatCarriesNoClientIdentity(t *testing.T) {
	allowed := map[string]bool{
		"time": true, "status": true, "latency": true, "method": true,
		"request_path": true, "safe_error": true,
	}
	tags := regexp.MustCompile(`\$\{([^}]*)\}`).FindAllStringSubmatch(requestLoggerFormat, -1)
	if len(tags) == 0 {
		t.Fatal("the request-log format has no tags, so the pin measures nothing")
	}
	for _, tag := range tags {
		name := strings.SplitN(tag[1], ":", 2)[0]
		if !allowed[name] {
			t.Fatalf("the request-log format carries ${%s}; only %v are by design", tag[1], allowed)
		}
	}
	for _, forbidden := range []string{"${ip}", "${ips}", "${ua}", "${url}", "${path}", "${reqHeader", "${header", "${queryParams}"} {
		if strings.Contains(requestLoggerFormat, forbidden) {
			t.Fatalf("the request-log format carries %s", forbidden)
		}
	}
}

// TestRequestLoggerWritesOnlyTheRedactedFeedLine drives the real
// newRequestLogger closures (the request_path and safe_error tags) with a
// distinctive forwarded address and user agent against the feed URL, and
// requires the whole line to be exactly time, 200, latency, GET and the route
// template, with an empty error field. A closure that routes the raw path, the
// address or the agent into either custom tag changes the line and goes red.
func TestRequestLoggerWritesOnlyTheRedactedFeedLine(t *testing.T) {
	const token = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789ABCDEFGHJKLMNP"
	const userAgent = "zz-distinctive-ua-4f1c LineProbe/1.0"
	const clientIP = "203.0.113.77"

	var output bytes.Buffer
	app := fiber.New(fiberConfig(proxySettings{
		Enabled:        true,
		Header:         "X-Forwarded-For",
		TrustedProxies: []string{"0.0.0.0"},
	}, nil))
	app.Use(newRequestLogger(&output))
	var observedIP, observedUA string
	app.Get(api.CalendarFeedRateLimitPrefix+"/:token.ics", func(c fiber.Ctx) error {
		observedIP, observedUA = c.IP(), c.Get("User-Agent")
		return c.SendStatus(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, api.CalendarFeedRateLimitPrefix+"/"+token+".ics", nil)
	request.Header.Set("X-Forwarded-For", clientIP)
	request.Header.Set("User-Agent", userAgent)
	response, err := app.Test(request, testConfigNoTimeout)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", response.StatusCode)
	}
	// Positive control: the address and agent really reached the handler.
	if observedIP != clientIP || observedUA != userAgent {
		t.Fatalf("the request context saw %q / %q, so the absence checks below are vacuous", observedIP, observedUA)
	}

	line := strings.TrimSuffix(output.String(), "\n")
	pattern := regexp.MustCompile(`^[^|]+ \| 200 \| [^|]+ \| GET \| /calendar/feed/:token\.ics \| $`)
	if strings.Count(output.String(), "\n") != 1 || !pattern.MatchString(line) {
		t.Fatalf("the access line is not exactly the redacted shape, got %q", output.String())
	}
}
