package main

import (
	"bytes"
	"log"
	"net/http"
	"strings"
	"testing"

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

			app := newCalendarFeedTestApp(t, handler, calendarFeedLimiterBudget)
			target := api.CalendarFeedRateLimitPrefix + "/" + token + suffix
			spelling := api.CalendarFeedRateLimitPrefix + "/<token>" + suffix
			calendarFeedSend(t, app, http.MethodGet, spelling, target)
			if status := calendarFeedSend(t, app, http.MethodGet, spelling, target); status != http.StatusTooManyRequests {
				t.Fatalf("GET %s answered %d, want 429: the log assertion below needs the limiter to fire", spelling, status)
			}

			output := logged.String()
			if strings.Contains(output, token) {
				t.Fatalf("the 429 path for %s logged the feed token verbatim", spelling)
			}
			if !strings.Contains(output, "rate limit reached") || !strings.Contains(output, `action="rate_limit"`) {
				t.Fatalf("expected the rate-limit log line and audit event for %s, got %q", spelling, output)
			}
		})
	}
}
