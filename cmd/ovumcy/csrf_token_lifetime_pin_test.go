package main

import (
	"testing"
	"time"
)

// The CSRF token's idle timeout is a BEHAVIOR-PRESERVING pin, not a security
// floor: fiber v2 expressed the same window as Expiration=1h, v3 renamed the
// field to IdleTimeout and defaults it to 30m. Until this file nothing read the
// value back, so a silent revert to the v3 default would have halved every
// form's validity window — the failure csrfMiddlewareConfig's own comment names
// — with the whole suite still green.
//
// The assert reads the REAL csrfMiddlewareConfig rather than a copy: the api
// package's test app carries its own hand-written csrf.Config
// (internal/api/test_onboarding_app_setup_helpers_test.go), and asserting
// against that one would pin the double instead of the server.
//
// handler is only closed over by the config's ErrorHandler, which this test
// never drives, so a nil handler is safe here.
func TestCSRFConfigPinsTheTokenIdleTimeoutToOneHour(t *testing.T) {
	for _, cookieSecure := range []bool{false, true} {
		config := csrfMiddlewareConfig(cookieSecure, nil)
		if config.IdleTimeout != time.Hour {
			t.Fatalf("csrfMiddlewareConfig(cookieSecure=%t).IdleTimeout = %s, want %s (fiber v3 defaults it to 30m)",
				cookieSecure, config.IdleTimeout, time.Hour)
		}
	}
}
