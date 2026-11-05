package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestRequestLoggerFormatCarriesNoClientIdentity pins the operator access log
// to the fields the privacy documentation promises: time, status, latency,
// method, the redacted route template and the redacted error. A client address,
// user agent, raw URL or any header or query value is not among them. The
// calendar-feed telemetry regression in internal/api mirrors this format by
// hand, so a change here that adds a field must be decided on both sides; the
// test below this one drives the production logger itself.
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

// TestRequestLoggerKeepsTheCalendarFeedTokenAndClientIdentityOutOfTheLine mounts
// the production request logger (newRequestLogger, with its request_path tag
// body) over the real route table and polls the real feed route with GET and
// HEAD, each carrying a forwarded client address and a user agent. The format
// pin above checks the tag names only; this checks what the tags write: the
// line carries the route template and neither the capability token, the client
// address nor the user agent. A second logger that does print ${ip} and ${ua}
// proves both reached the request context, so the absence is not vacuous.
func TestRequestLoggerKeepsTheCalendarFeedTokenAndClientIdentityOutOfTheLine(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	owner := seedOwner(t, db.NewRepositories(database), "feed-access-log@example.com", 14)
	token := armCalendarFeedToken(t, database, owner.ID)
	if len(token) <= 16 {
		t.Fatalf("the minted feed token is implausibly short (%d)", len(token))
	}

	var accessLog, controlLog bytes.Buffer
	app := fiber.New(fiberConfig(proxySettings{
		Enabled:        true,
		Header:         fiber.HeaderXForwardedFor,
		TrustedProxies: []string{"0.0.0.0"},
	}, handler))
	app.Use(newRequestLogger(&accessLog))
	app.Use(logger.New(logger.Config{Stream: &controlLog, Format: "${method} ${ip} ${ua}\n"}))
	app.Use(api.MethodOverride(handler))
	app.Use(handler.LanguageMiddleware)
	api.RegisterRoutes(app, handler)
	app.Use(handler.NotFound)

	const (
		clientIP  = "203.0.113.77"
		userAgent = "AccessLogProbeClient/9.87.6 (zz-distinctive-ua-4f1c)"
		template  = "/calendar/feed/:token.ics"
	)
	methods := []string{http.MethodGet, http.MethodHead}
	for _, method := range methods {
		request := httptest.NewRequest(method, api.CalendarFeedRateLimitPrefix+"/"+token+".ics", nil)
		request.Header.Set("X-Forwarded-For", clientIP)
		request.Header.Set("User-Agent", userAgent)
		response, err := app.Test(request, testConfigNoTimeout)
		if err != nil {
			t.Fatalf("%s feed request failed: %v", method, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s answered %d; the scan below would measure a refused request", method, response.StatusCode)
		}
	}

	control := controlLog.String()
	for _, method := range methods {
		if !strings.Contains(control, method+" "+clientIP+" "+userAgent) {
			t.Fatalf("the proxy config did not surface the %s client address and agent, so a clean line proves nothing; control log:\n%s", method, control)
		}
	}

	lines := strings.Split(strings.TrimSuffix(accessLog.String(), "\n"), "\n")
	if len(lines) != len(methods) {
		t.Fatalf("expected one access-log line per request, got %d:\n%s", len(lines), accessLog.String())
	}
	selector, verifier := token[:16], token[16:]
	for index, method := range methods {
		line := lines[index]
		for _, want := range []string{"| 200 |", "| " + method + " |", "| " + template + " |"} {
			if !strings.Contains(line, want) {
				t.Fatalf("the %s line lacks %q, so it is not the route-template entry:\n%s", method, want, line)
			}
		}
		for _, secret := range []string{token, selector, verifier, clientIP, userAgent, "zz-distinctive-ua-4f1c"} {
			if strings.Contains(line, secret) {
				t.Fatalf("the %s access-log line carries %q:\n%s", method, secret, line)
			}
		}
	}
}
