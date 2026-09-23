package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// api.RegisterRoutes gives every GET route a HEAD twin running the same chain,
// so HEAD /calendar builds the same month grid a GET does. The calendar page
// budget must therefore be spent — and enforced — by both methods from one
// bucket, while the day panel sharing the /calendar prefix spends none of it.
func TestCalendarPageBudgetCoversHeadAndSkipsTheDayPanel(t *testing.T) {
	handler := newRateLimitTestHandler(t)

	cases := []struct {
		name        string
		sequence    []string
		wantLimited bool
	}{
		{name: "head then head", sequence: []string{"HEAD /calendar", "HEAD /calendar"}, wantLimited: true},
		{name: "head then get", sequence: []string{"HEAD /calendar", "GET /calendar"}, wantLimited: true},
		{name: "get then head", sequence: []string{"GET /calendar", "HEAD /calendar"}, wantLimited: true},
		{name: "day panel spends nothing", sequence: []string{"GET /calendar/day/2026-01-01", "GET /calendar"}, wantLimited: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := newRateLimitEnvelopeTestApp(t, handler, rateLimitSurface{})
			var last int
			for _, step := range tc.sequence {
				method, path, _ := strings.Cut(step, " ")
				response, err := app.Test(httptest.NewRequest(method, path, nil), testConfigNoTimeout)
				if err != nil {
					t.Fatalf("%s failed: %v", step, err)
				}
				_ = response.Body.Close()
				last = response.StatusCode
			}
			if limited := last == http.StatusTooManyRequests; limited != tc.wantLimited {
				t.Fatalf("last status = %d, want limited=%v", last, tc.wantLimited)
			}
		})
	}
}
