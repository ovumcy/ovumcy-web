package main

import (
	"os"
	"strconv"
	"testing"
	"time"
)

func itoa(value int) string { return strconv.Itoa(value) }

// Every RATE_LIMIT_* setting has a ceiling: a value above it falls back to the
// default instead of widening — or switching off — the limiter. The table below
// is the contract; a new setting without a row here has no ceiling.
var rateLimitCeilingCases = []struct {
	key      string
	read     func(rateLimitSettings) int
	ceiling  int
	fallback int
}{
	{key: "RATE_LIMIT_LOGIN_MAX", read: func(s rateLimitSettings) int { return s.LoginMax }, ceiling: rateLimitCredentialMaxCeiling, fallback: 8},
	{key: "RATE_LIMIT_REGISTER_MAX", read: func(s rateLimitSettings) int { return s.RegisterMax }, ceiling: rateLimitCredentialMaxCeiling, fallback: 8},
	{key: "RATE_LIMIT_FORGOT_PASSWORD_MAX", read: func(s rateLimitSettings) int { return s.ForgotPasswordMax }, ceiling: rateLimitCredentialMaxCeiling, fallback: 8},
	{key: "RATE_LIMIT_LOGOUT_MAX", read: func(s rateLimitSettings) int { return s.LogoutMax }, ceiling: rateLimitLogoutMaxCeiling, fallback: 60},
	{key: "RATE_LIMIT_LOGOUT_ACCOUNT_MAX", read: func(s rateLimitSettings) int { return s.LogoutAccountMax }, ceiling: rateLimitLogoutAccountMaxCeiling, fallback: 20},
	{key: "RATE_LIMIT_API_MAX", read: func(s rateLimitSettings) int { return s.APIMax }, ceiling: rateLimitAPIMaxCeiling, fallback: 300},
	{key: "RATE_LIMIT_CALENDAR_FEED_MAX", read: func(s rateLimitSettings) int { return s.CalendarFeedMax }, ceiling: rateLimitCalendarFeedMaxCeiling, fallback: 20},
}

var rateLimitWindowCases = []struct {
	key      string
	read     func(rateLimitSettings) time.Duration
	fallback time.Duration
}{
	{key: "RATE_LIMIT_LOGIN_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.LoginWindow }, fallback: 15 * time.Minute},
	{key: "RATE_LIMIT_REGISTER_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.RegisterWindow }, fallback: 15 * time.Minute},
	{key: "RATE_LIMIT_FORGOT_PASSWORD_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.ForgotPasswordWindow }, fallback: time.Hour},
	{key: "RATE_LIMIT_LOGOUT_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.LogoutWindow }, fallback: 15 * time.Minute},
	{key: "RATE_LIMIT_LOGOUT_ACCOUNT_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.LogoutAccountWindow }, fallback: 15 * time.Minute},
	{key: "RATE_LIMIT_API_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.APIWindow }, fallback: time.Minute},
	{key: "RATE_LIMIT_CALENDAR_FEED_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.CalendarFeedWindow }, fallback: time.Minute},
}

func loadRateLimits(t *testing.T) rateLimitSettings {
	t.Helper()
	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	return config.RateLimits
}

// TestRateLimitMaxSettingsHaveCeilings: the ceiling itself is accepted, one
// above it is refused in favour of the default, and the calendar feed — the
// only cap on a cookieless surface — is bounded like every other row.
func TestRateLimitMaxSettingsHaveCeilings(t *testing.T) {
	for _, tc := range rateLimitCeilingCases {
		t.Run(tc.key, func(t *testing.T) {
			minimalRuntimeEnv(t)

			t.Setenv(tc.key, itoa(tc.ceiling))
			if got := tc.read(loadRateLimits(t)); got != tc.ceiling {
				t.Fatalf("%s at its ceiling %d read back as %d", tc.key, tc.ceiling, got)
			}

			t.Setenv(tc.key, itoa(tc.ceiling+1))
			if got := tc.read(loadRateLimits(t)); got != tc.fallback {
				t.Fatalf("%s=%d (above the ceiling %d) read back as %d, want the default %d", tc.key, tc.ceiling+1, tc.ceiling, got, tc.fallback)
			}

			t.Setenv(tc.key, "1000000")
			if got := tc.read(loadRateLimits(t)); got != tc.fallback {
				t.Fatalf("%s=1000000 read back as %d, want the default %d: a huge value must not switch the limiter off", tc.key, got, tc.fallback)
			}

			t.Setenv(tc.key, "0")
			if got := tc.read(loadRateLimits(t)); got != tc.fallback {
				t.Fatalf("%s=0 read back as %d, want the default %d", tc.key, got, tc.fallback)
			}
		})
	}
}

// TestRateLimitWindowSettingsHaveCeilings: a window may not exceed a day (a
// refused client would otherwise stay refused for as long as the process runs)
// nor drop below a second.
func TestRateLimitWindowSettingsHaveCeilings(t *testing.T) {
	for _, tc := range rateLimitWindowCases {
		t.Run(tc.key, func(t *testing.T) {
			minimalRuntimeEnv(t)

			t.Setenv(tc.key, "24h")
			if got := tc.read(loadRateLimits(t)); got != 24*time.Hour {
				t.Fatalf("%s=24h read back as %s", tc.key, got)
			}

			t.Setenv(tc.key, "24h1s")
			if got := tc.read(loadRateLimits(t)); got != tc.fallback {
				t.Fatalf("%s=24h1s read back as %s, want the default %s", tc.key, got, tc.fallback)
			}

			t.Setenv(tc.key, "500ms")
			if got := tc.read(loadRateLimits(t)); got != tc.fallback {
				t.Fatalf("%s=500ms read back as %s, want the default %s", tc.key, got, tc.fallback)
			}

			t.Setenv(tc.key, "1s")
			if got := tc.read(loadRateLimits(t)); got != time.Second {
				t.Fatalf("%s=1s read back as %s", tc.key, got)
			}
		})
	}
}

// TestGetEnvDurationInRange pins the helper the windows read through: the full
// inclusive range is accepted, and unset, unparseable or out-of-range input
// falls back.
func TestGetEnvDurationInRange(t *testing.T) {
	const key = "TEST_ENV_DURATION_IN_RANGE"
	cases := []struct {
		name  string
		value string
		set   bool
		want  time.Duration
	}{
		{name: "unset -> fallback", set: false, want: time.Minute},
		{name: "floor accepted", value: "1s", set: true, want: time.Second},
		{name: "ceiling accepted", value: "1h", set: true, want: time.Hour},
		{name: "mid-range accepted", value: "90s", set: true, want: 90 * time.Second},
		{name: "below floor -> fallback", value: "999ms", set: true, want: time.Minute},
		{name: "above ceiling -> fallback", value: "1h1ns", set: true, want: time.Minute},
		{name: "unparseable -> fallback", value: "soon", set: true, want: time.Minute},
		{name: "blank -> fallback", value: "  ", set: true, want: time.Minute},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.set {
				t.Setenv(key, tc.value)
			} else if err := os.Unsetenv(key); err != nil {
				t.Fatalf("unset %s: %v", key, err)
			}
			if got := getEnvDurationInRange(key, time.Minute, time.Second, time.Hour); got != tc.want {
				t.Fatalf("getEnvDurationInRange(%q) = %s, want %s", tc.value, got, tc.want)
			}
		})
	}
}
