package api

import (
	"strings"
	"testing"
)

// TestIsCalendarFeedRequestMatchesTheRouteFormAndMethod pins the boundary
// IsCalendarFeedRequest enforces: a concrete feed URL under GET or HEAD is
// in; a mutating verb at that same URL, the bare prefix (with or without a
// trailing slash), a nested path segment, an empty token, and a neighbour
// that merely continues the prefix's characters are all out. Case and
// trailing-slash spellings are folded in exactly as fiber's own router does
// under the shipped fiberConfig — see cmd/ovumcy's route-shape definition
// test, which drives a real fiber.Router registered with this exact pattern
// and cross-checks every case here (and more) against what actually
// dispatches, rather than trusting this table alone.
//
// The bare-prefix-with-trailing-slash case used to be pinned TRUE here
// (TestIsCalendarFeedRequestPathRequiresTheSeparator, the predicate's
// previous path-only form, formerly in middleware_language_helpers_mutkill_test.go):
// a HasPrefix(path, prefix+"/") match treated "/calendar/feed/" itself as the
// feed, handing a path no route ever answers the same middleware skip a real
// feed URL gets. The route's form requires a non-empty token, which this path
// does not have, so it is now FALSE.
func TestIsCalendarFeedRequestMatchesTheRouteFormAndMethod(t *testing.T) {
	feedURL := CalendarFeedRateLimitPrefix + "/" + strings.Repeat("A", 48) + ".ics"

	cases := []struct {
		method string
		path   string
		want   bool
	}{
		{"GET", feedURL, true},
		{"HEAD", feedURL, true},
		{"POST", feedURL, false},
		{"PUT", feedURL, false},
		{"PATCH", feedURL, false},
		{"DELETE", feedURL, false},
		{"GET", strings.ToUpper(feedURL), true},
		{"GET", feedURL + "/", true},
		{"GET", feedURL + "//", true},
		{"GET", CalendarFeedRateLimitPrefix + "/a.b.ics", true}, // token "a.b": only the FINAL ".ics" is the literal suffix
		{"GET", CalendarFeedRateLimitPrefix + "/", false},
		{"GET", CalendarFeedRateLimitPrefix, false},
		{"GET", CalendarFeedRateLimitPrefix + "back", false},
		{"GET", CalendarFeedRateLimitPrefix + "/a/b.ics", false}, // token would contain "/"
		{"GET", CalendarFeedRateLimitPrefix + "/.ics", false},    // empty token
		{"GET", "/calendar/day/2026-01-15", false},
		{"GET", "/", false},
	}
	for _, testCase := range cases {
		if got := IsCalendarFeedRequest(testCase.method, testCase.path); got != testCase.want {
			t.Errorf("IsCalendarFeedRequest(%q, %q) = %t, want %t", testCase.method, testCase.path, got, testCase.want)
		}
	}
}
