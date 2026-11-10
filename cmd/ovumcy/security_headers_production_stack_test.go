package main

import (
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The securityHeadersMiddleware tests in main_test.go mount the middleware on
// a bare fiber app and compare against the constant it emits, so they hold
// with the production mount removed and with any policy the constant names.
// These two read the headers off the app newFiberApp assembles.

// TestSecurityHeadersReachPagesAndAPIOnTheProductionStack requests one HTML
// page and one /api/v1 route through the production middleware chain and
// route table.
func TestSecurityHeadersReachPagesAndAPIOnTheProductionStack(t *testing.T) {
	app := newCalendarFeedNoCookieTestApp(t)

	for _, path := range []string{"/login", "/api/v1/users/current"} {
		response, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil), testConfigNoTimeout)
		if err != nil {
			t.Fatalf("GET %s failed: %v", path, err)
		}
		_ = response.Body.Close()
		if response.StatusCode == http.StatusNotFound {
			t.Fatalf("GET %s answered 404: the probe no longer reaches a real route", path)
		}
		assertDefaultSecurityHeaders(t, response, false)
	}
}

// TestContentSecurityPolicyNamesNoOtherOriginOnTheProductionStack pins the
// policy the browser receives as literals, not through the constant that
// emits it: the instance loads no script, style, image, font, frame or
// connection from another origin, which is what keeps analytics, telemetry
// and third-party identifiers out of every page.
func TestContentSecurityPolicyNamesNoOtherOriginOnTheProductionStack(t *testing.T) {
	app := newCalendarFeedNoCookieTestApp(t)

	response, err := app.Test(httptest.NewRequest(http.MethodGet, "/login", nil), testConfigNoTimeout)
	if err != nil {
		t.Fatalf("GET /login failed: %v", err)
	}
	_ = response.Body.Close()

	policy := response.Header.Get("Content-Security-Policy")
	if policy == "" {
		t.Fatal("GET /login carries no Content-Security-Policy")
	}
	got := map[string][]string{}
	for _, directive := range strings.Split(policy, ";") {
		fields := strings.Fields(directive)
		if len(fields) == 0 {
			continue
		}
		if _, repeated := got[fields[0]]; repeated {
			t.Errorf("directive %s appears twice in %q", fields[0], policy)
		}
		got[fields[0]] = fields[1:]
		for _, source := range fields[1:] {
			lower := strings.ToLower(source)
			if strings.Contains(lower, "http:") || strings.Contains(lower, "https:") || strings.Contains(lower, "//") || strings.Contains(lower, "*") {
				t.Errorf("%s allows %s: a page may then load from another origin", fields[0], source)
			}
			switch source {
			case "'self'", "'none'", "data:":
			default:
				t.Errorf("%s allows %s, which is neither 'self', 'none' nor data:", fields[0], source)
			}
		}
	}

	want := map[string][]string{
		"default-src":     {"'self'"},
		"script-src":      {"'self'"},
		"style-src":       {"'self'"},
		"img-src":         {"'self'", "data:"},
		"font-src":        {"'self'"},
		"connect-src":     {"'self'"},
		"manifest-src":    {"'self'"},
		"object-src":      {"'none'"},
		"base-uri":        {"'self'"},
		"form-action":     {"'self'"},
		"frame-ancestors": {"'none'"},
		"worker-src":      {"'none'"},
	}
	if !maps.EqualFunc(got, want, func(a, b []string) bool { return strings.Join(a, " ") == strings.Join(b, " ") }) {
		t.Errorf("Content-Security-Policy = %q\nparsed %v\nwant   %v", policy, got, want)
	}
}
