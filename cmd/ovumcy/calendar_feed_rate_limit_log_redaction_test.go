package main

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/ovumcy/ovumcy-web/internal/api"
	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestCalendarFeedRateLimitLogMasksTheTokenOnEveryCaseOfTheSuffix drives the
// production stack past the feed budget with the suffix spelled in each case the
// limiter's case-folded predicate admits, and asserts neither 429 sink carries
// the token. Whichever path SafeRequestLogPath picks on the limiter's USE route,
// the raw-path mask it may fall back to is case-folded to match
// (TestSanitizeRequestLogPathMasksRawTokenDotICSFallback).
func TestCalendarFeedRateLimitLogMasksTheTokenOnEveryCaseOfTheSuffix(t *testing.T) {
	handler, database := newRateLimitTestHandlerAndDB(t)
	user := seedOwner(t, db.NewRepositories(database), "calendar-feed-log-mask@example.com", 14)
	token := armCalendarFeedToken(t, database, user.ID)

	for _, suffix := range []string{".ics", ".ICS", ".Ics"} {
		t.Run(suffix, func(t *testing.T) {
			originalWriter := log.Writer()
			defer log.SetOutput(originalWriter)
			var logged bytes.Buffer
			log.SetOutput(&logged)

			// The production stack behind a trusted proxy: the forwarded address
			// and the agent are distinctive, and neither sink may carry them.
			const clientIP = "203.0.113.77"
			const userAgent = "zz-distinctive-ua-4f1c RateLimitProbe/1.0"
			settings := uniformRateLimits(t, 100000, time.Hour)
			settings.CalendarFeedMax = calendarFeedLimiterBudget
			config := runtimeConfig{
				Location:        time.UTC,
				DefaultLanguage: "en",
				RateLimits:      settings,
				Proxy: proxySettings{
					Enabled:        true,
					Header:         "X-Forwarded-For",
					TrustedProxies: []string{"0.0.0.0"},
				},
			}
			// The production chain, as newFiberApp builds it, with one control
			// logger ahead of it that DOES print the client address and agent: it
			// proves both reached the request context, so the absence checks below
			// are not green for a client that never arrived.
			var controlLog bytes.Buffer
			app := fiber.New(fiberConfig(config.Proxy, handler))
			app.Use(logger.New(logger.Config{Stream: &controlLog, Format: "${status} ${ip} ${ua}\n"}))
			configureFiberMiddleware(app, config, handler)
			registerStaticContentTypes()
			app.Use("/static", newStaticAssetHandler())
			api.RegisterRoutes(app, handler)
			app.Use(handler.NotFound)
			target := api.CalendarFeedRateLimitPrefix + "/" + token + suffix
			spelling := api.CalendarFeedRateLimitPrefix + "/<token>" + suffix
			send := func() int {
				request := httptest.NewRequest(http.MethodGet, target, nil)
				request.Header.Set("X-Forwarded-For", clientIP)
				request.Header.Set("User-Agent", userAgent)
				response, err := app.Test(request, testConfigNoTimeout)
				if err != nil {
					t.Fatalf("GET %s: %v", spelling, err)
				}
				_ = response.Body.Close()
				return response.StatusCode
			}
			send()
			if status := send(); status != http.StatusTooManyRequests {
				t.Fatalf("GET %s answered %d, want 429: the log assertion below needs the limiter to fire", spelling, status)
			}

			if want := "429 " + clientIP + " " + userAgent; !strings.Contains(controlLog.String(), want) {
				t.Fatalf("the control logger did not see the refused request from %s with its agent, so the absence checks below prove nothing; control log:\n%s", spelling, controlLog.String())
			}

			output := logged.String()
			if strings.Contains(output, token) {
				t.Fatalf("the 429 path for %s logged the feed token verbatim", spelling)
			}
			for _, identity := range []string{clientIP, "zz-distinctive-ua-4f1c", userAgent} {
				if strings.Contains(output, identity) {
					t.Fatalf("the 429 path for %s logged client identity %q: %q", spelling, identity, output)
				}
			}
			if !strings.Contains(output, "rate limit reached") || !strings.Contains(output, `action="rate_limit"`) {
				t.Fatalf("expected the rate-limit log line and audit event for %s, got %q", spelling, output)
			}
		})
	}
}
