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
		{"GET", strings.ToUpper(feedURL), true},
		{"GET", feedURL + "/", true},
		{"GET", feedURL + "//", true},
		{"GET", CalendarFeedRateLimitPrefix + "/a.b.ics", true}, // token "a.b": the remainder holds exactly one ".ics", so it is both the first and only occurrence
		{"GET", CalendarFeedRateLimitPrefix + "/", false},
		{"GET", CalendarFeedRateLimitPrefix, false},
		{"GET", CalendarFeedRateLimitPrefix + "back", false},
		{"GET", CalendarFeedRateLimitPrefix + "/a/b.ics", false}, // token would contain "/"
		{"GET", CalendarFeedRateLimitPrefix + "/.ics", false},    // empty token
		{"GET", "/calendar/day/2026-01-15", false},
		{"GET", "/", false},
		// fiber's router finds the token/suffix boundary at the FIRST ".ics" in
		// the remainder, not the last (path.go's findParamLen uses
		// strings.Index, never strings.LastIndex): each of these carries a
		// second ".ics" further right that the router never reaches, so it
		// refuses the whole path rather than matching a shorter token plus
		// trailing garbage. See cmd/ovumcy's route-shape definition test, which
		// confirms this against fiber's own router rather than this table alone.
		{"GET", CalendarFeedRateLimitPrefix + "/a.ics.ics", false},
		{"GET", CalendarFeedRateLimitPrefix + "/.ics.ics", false},
		{"GET", CalendarFeedRateLimitPrefix + "/a.icsb.ics", false},
		{"GET", CalendarFeedRateLimitPrefix + "/a.ICS.ics", false},
	}
	for _, testCase := range cases {
		if got := IsCalendarFeedRequest(testCase.method, testCase.path); got != testCase.want {
			t.Errorf("IsCalendarFeedRequest(%q, %q) = %t, want %t", testCase.method, testCase.path, got, testCase.want)
		}
	}

	// The mutating-verb refusal is a property of the method alone (see
	// IsCalendarFeedRequest), so one representative path stands in for every
	// non-GET/HEAD verb rather than repeating the same feed URL under each.
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if got := IsCalendarFeedRequest(method, feedURL); got {
			t.Errorf("IsCalendarFeedRequest(%q, %q) = %t, want false — the route dispatches GET/HEAD only", method, feedURL, got)
		}
	}
}
