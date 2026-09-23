package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	{key: "RATE_LIMIT_CALENDAR_MAX", read: func(s rateLimitSettings) int { return s.CalendarMax }, ceiling: rateLimitCalendarMaxCeiling, fallback: 300},
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
	{key: "RATE_LIMIT_CALENDAR_WINDOW", read: func(s rateLimitSettings) time.Duration { return s.CalendarWindow }, fallback: time.Minute},
}

func loadRateLimits(t *testing.T) rateLimitSettings {
	t.Helper()
	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	return config.RateLimits
}

// loadRateLimitsRefusing loads the settings with key set to an out-of-range
// value and fails unless the refusal reached the boot log: a value replaced in
// silence leaves the operator believing the limiter runs at what they set.
func loadRateLimitsRefusing(t *testing.T, key string, value string) rateLimitSettings {
	t.Helper()
	t.Setenv(key, value)

	var captured bytes.Buffer
	settings := func() rateLimitSettings {
		previous := log.Writer()
		log.SetOutput(&captured)
		defer log.SetOutput(previous)
		return loadRateLimits(t)
	}()

	if want := "invalid " + key + "=" + strconv.Quote(value); !strings.Contains(captured.String(), want) {
		t.Fatalf("%s=%s was replaced without being logged; want %q in %q", key, value, want, captured.String())
	}
	return settings
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

			if got := tc.read(loadRateLimitsRefusing(t, tc.key, itoa(tc.ceiling+1))); got != tc.fallback {
				t.Fatalf("%s=%d (above the ceiling %d) read back as %d, want the default %d", tc.key, tc.ceiling+1, tc.ceiling, got, tc.fallback)
			}

			if got := tc.read(loadRateLimitsRefusing(t, tc.key, "1000000")); got != tc.fallback {
				t.Fatalf("%s=1000000 read back as %d, want the default %d: a huge value must not switch the limiter off", tc.key, got, tc.fallback)
			}

			if got := tc.read(loadRateLimitsRefusing(t, tc.key, "0")); got != tc.fallback {
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

			if got := tc.read(loadRateLimitsRefusing(t, tc.key, "24h1s")); got != tc.fallback {
				t.Fatalf("%s=24h1s read back as %s, want the default %s", tc.key, got, tc.fallback)
			}

			if got := tc.read(loadRateLimitsRefusing(t, tc.key, "500ms")); got != tc.fallback {
				t.Fatalf("%s=500ms read back as %s, want the default %s", tc.key, got, tc.fallback)
			}

			// The one-second floor holds for every window, but a credential
			// pair is also held to its per-minute rate, which no MAX of at
			// least one meets over a single second: there the pair refuses it.
			if isCredentialRateLimitKey(tc.key) {
				if got := tc.read(loadRateLimitsRefusingPair(t, credentialMaxKey(tc.key), "", tc.key, "1s")); got != tc.fallback {
					t.Fatalf("%s=1s read back as %s, want the default %s: its pair exceeds the credential rate ceiling", tc.key, got, tc.fallback)
				}
				return
			}
			t.Setenv(tc.key, "1s")
			if got := tc.read(loadRateLimits(t)); got != time.Second {
				t.Fatalf("%s=1s read back as %s", tc.key, got)
			}
		})
	}
}

// isCredentialRateLimitKey reports whether a RATE_LIMIT_* key belongs to a
// credential endpoint, derived from the ceiling its MAX is held to in
// rateLimitCeilingCases rather than from a second list of names.
func isCredentialRateLimitKey(key string) bool {
	maxKey := credentialMaxKey(key)
	for _, tc := range rateLimitCeilingCases {
		if tc.key == maxKey {
			return tc.ceiling == rateLimitCredentialMaxCeiling
		}
	}
	return false
}

func credentialMaxKey(key string) string {
	return strings.TrimSuffix(strings.TrimSuffix(key, "_WINDOW"), "_MAX") + "_MAX"
}

func credentialWindowKey(maxKey string) string {
	return strings.TrimSuffix(maxKey, "_MAX") + "_WINDOW"
}

// loadRateLimitsRefusingPair sets a MAX/WINDOW pair (an empty value leaves
// that half at its default) and fails unless the boot log carries the pair
// refusal: it names both keys, the allowed rate and a pair that is allowed.
func loadRateLimitsRefusingPair(t *testing.T, maxKey, maxValue, windowKey, windowValue string) rateLimitSettings {
	t.Helper()
	t.Setenv(maxKey, maxValue)
	t.Setenv(windowKey, windowValue)

	var captured bytes.Buffer
	settings := func() rateLimitSettings {
		previous := log.Writer()
		log.SetOutput(&captured)
		defer log.SetOutput(previous)
		return loadRateLimits(t)
	}()

	for _, want := range []string{
		"invalid " + maxKey + "=",
		" with " + windowKey + "=",
		"at most " + itoa(rateLimitCredentialPerMinuteCeiling) + " requests per minute",
		maxKey + "=100 with " + windowKey + "=200s",
	} {
		if !strings.Contains(captured.String(), want) {
			t.Fatalf("%s=%q with %s=%q was replaced without the pair refusal in the boot log; want %q in %q", maxKey, maxValue, windowKey, windowValue, want, captured.String())
		}
	}
	return settings
}

// TestCredentialRateLimitPairsHaveARateCeiling holds each credential pair to
// rateLimitCredentialPerMinuteCeiling requests a minute. The count ceiling
// alone let 100 over a one-second window through — 6000 bcrypt compares a
// minute from one address. Exactly the ceiling is accepted, one request more
// in the same window is refused, a refused pair falls back on BOTH halves, and
// the example the refusal names is itself accepted.
func TestCredentialRateLimitPairsHaveARateCeiling(t *testing.T) {
	credentialRows := 0
	for _, row := range rateLimitCeilingCases {
		if row.ceiling != rateLimitCredentialMaxCeiling {
			continue
		}
		credentialRows++
		maxKey, readMax, fallbackMax := row.key, row.read, row.fallback
		windowKey := credentialWindowKey(maxKey)
		readWindow, fallbackWindow := credentialWindowRow(t, windowKey)

		t.Run(maxKey, func(t *testing.T) {
			minimalRuntimeEnv(t)

			if !credentialRateWithinCeiling(fallbackMax, fallbackWindow) {
				t.Fatalf("the default pair %d / %s is itself above the credential rate ceiling", fallbackMax, fallbackWindow)
			}

			for _, pair := range []struct {
				max    int
				window string
				want   time.Duration
			}{
				{max: 30, window: "1m", want: time.Minute},
				{max: 100, window: "200s", want: 200 * time.Second},
				{max: 1, window: "2s", want: 2 * time.Second},
			} {
				t.Setenv(maxKey, itoa(pair.max))
				t.Setenv(windowKey, pair.window)
				settings := loadRateLimits(t)
				if readMax(settings) != pair.max || readWindow(settings) != pair.want {
					t.Fatalf("%s=%d with %s=%s is within the rate ceiling and read back as %d / %s", maxKey, pair.max, windowKey, pair.window, readMax(settings), readWindow(settings))
				}
			}

			for _, pair := range []struct{ max, window string }{
				{max: "31", window: "1m"},
				{max: "100", window: "199s"},
				{max: "2", window: "3s"},
				{max: "", window: "1s"},
			} {
				settings := loadRateLimitsRefusingPair(t, maxKey, pair.max, windowKey, pair.window)
				if readMax(settings) != fallbackMax || readWindow(settings) != fallbackWindow {
					t.Fatalf("%s=%q with %s=%q is above the rate ceiling and read back as %d / %s, want both defaults %d / %s",
						maxKey, pair.max, windowKey, pair.window, readMax(settings), readWindow(settings), fallbackMax, fallbackWindow)
				}
			}
		})
	}
	if credentialRows != 3 {
		t.Fatalf("found %d credential rows in rateLimitCeilingCases, want login, registration and password reset", credentialRows)
	}
}

// TestCredentialRateLimitRefusalLeavesTheOtherPairsAlone: one credential pair
// above the rate ceiling falls back on its own, never on its neighbours.
func TestCredentialRateLimitRefusalLeavesTheOtherPairsAlone(t *testing.T) {
	minimalRuntimeEnv(t)
	t.Setenv("RATE_LIMIT_REGISTER_MAX", "30")
	t.Setenv("RATE_LIMIT_REGISTER_WINDOW", "1m")
	t.Setenv("RATE_LIMIT_FORGOT_PASSWORD_MAX", "100")
	t.Setenv("RATE_LIMIT_FORGOT_PASSWORD_WINDOW", "200s")

	settings := loadRateLimitsRefusingPair(t, "RATE_LIMIT_LOGIN_MAX", "31", "RATE_LIMIT_LOGIN_WINDOW", "1m")
	if settings.LoginMax != 8 || settings.LoginWindow != 15*time.Minute {
		t.Fatalf("login pair above the rate ceiling read back as %d / %s, want 8 / 15m", settings.LoginMax, settings.LoginWindow)
	}
	if settings.RegisterMax != 30 || settings.RegisterWindow != time.Minute {
		t.Fatalf("register pair = %d / %s, want the configured 30 / 1m", settings.RegisterMax, settings.RegisterWindow)
	}
	if settings.ForgotPasswordMax != 100 || settings.ForgotPasswordWindow != 200*time.Second {
		t.Fatalf("forgot-password pair = %d / %s, want the configured 100 / 200s", settings.ForgotPasswordMax, settings.ForgotPasswordWindow)
	}
}

// TestCredentialMaxCeilingIsReadOnlyThroughTheRateCheck: the credential count
// ceiling is referenced by server code only inside getCredentialRateLimit, so a
// credential setting added later cannot be read past the rate ceiling.
func TestCredentialMaxCeilingIsReadOnlyThroughTheRateCheck(t *testing.T) {
	source := serverSourceText(t)
	start := strings.Index(source, "func getCredentialRateLimit(")
	if start < 0 {
		t.Fatal("getCredentialRateLimit not found in the server sources — update this guard alongside the rename")
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatal("could not find the end of getCredentialRateLimit")
	}
	inside := strings.Count(source[start:start+end], "rateLimitCredentialMaxCeiling")
	if inside == 0 {
		t.Fatal("getCredentialRateLimit no longer reads rateLimitCredentialMaxCeiling; the guard is measuring the wrong thing")
	}
	// The one occurrence allowed outside it is the constant's own declaration.
	if outside := strings.Count(source, "rateLimitCredentialMaxCeiling") - inside; outside != 1 {
		t.Fatalf("rateLimitCredentialMaxCeiling appears %d times outside getCredentialRateLimit, want only its declaration; read a credential pair through getCredentialRateLimit", outside)
	}

	// The ceiling constant is only half of it: a credential key read by name
	// through any other helper skips the rate check without touching the
	// constant. Every string literal naming one — or starting one, the piece a
	// concatenation builds it from — must be an argument of a
	// getCredentialRateLimit call. No constant holds these keys, so no
	// declaration site is exempt either.
	throughRateCheck, elsewhere := credentialKeyLiteralSites(t)
	for _, site := range elsewhere {
		t.Errorf("%s names a credential rate-limit key outside a getCredentialRateLimit call; read the pair through getCredentialRateLimit so it is held to the rate ceiling", site)
	}
	for _, key := range []string{
		"RATE_LIMIT_LOGIN_MAX", "RATE_LIMIT_LOGIN_WINDOW",
		"RATE_LIMIT_REGISTER_MAX", "RATE_LIMIT_REGISTER_WINDOW",
		"RATE_LIMIT_FORGOT_PASSWORD_MAX", "RATE_LIMIT_FORGOT_PASSWORD_WINDOW",
	} {
		if !throughRateCheck[key] {
			t.Errorf("%s is not passed to getCredentialRateLimit as a literal; the guard is measuring the wrong thing", key)
		}
	}
}

// credentialRateLimitKeyPattern matches a credential RATE_LIMIT_ key or the
// prefix a concatenation would build one from.
var credentialRateLimitKeyPattern = regexp.MustCompile(`RATE_LIMIT_(LOGIN|REGISTER|FORGOT_PASSWORD)_`)

// credentialKeyLiteralSites parses every non-test server Go source (comments
// are not string literals, so prose naming a key is not a site) and sorts each
// string literal matching credentialRateLimitKeyPattern into the keys passed
// directly to getCredentialRateLimit and the positions of every other one.
func credentialKeyLiteralSites(t *testing.T) (map[string]bool, []string) {
	t.Helper()
	fileSet := token.NewFileSet()
	throughRateCheck := map[string]bool{}
	var elsewhere []string
	parsed := 0
	for _, tree := range []string{".", filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(tree, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() && entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			file, err := parser.ParseFile(fileSet, path, nil, 0)
			if err != nil {
				return err
			}
			parsed++
			rateCheckArgs := map[*ast.BasicLit]bool{}
			ast.Inspect(file, func(node ast.Node) bool {
				switch typed := node.(type) {
				case *ast.CallExpr:
					if ident, ok := typed.Fun.(*ast.Ident); ok && ident.Name == "getCredentialRateLimit" {
						for _, arg := range typed.Args {
							if literal, ok := arg.(*ast.BasicLit); ok {
								rateCheckArgs[literal] = true
							}
						}
					}
				case *ast.BasicLit:
					if typed.Kind != token.STRING || !credentialRateLimitKeyPattern.MatchString(typed.Value) {
						return true
					}
					if rateCheckArgs[typed] {
						if key, unquoteErr := strconv.Unquote(typed.Value); unquoteErr == nil {
							throughRateCheck[key] = true
						}
						return true
					}
					elsewhere = append(elsewhere, fileSet.Position(typed.Pos()).String()+" ("+typed.Value+")")
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}
	if parsed == 0 {
		t.Fatal("no server sources parsed; this guard would pass on an empty corpus")
	}
	return throughRateCheck, elsewhere
}

// credentialWindowRow returns the reader and default of a credential WINDOW
// setting from rateLimitWindowCases.
func credentialWindowRow(t *testing.T, windowKey string) (func(rateLimitSettings) time.Duration, time.Duration) {
	t.Helper()
	for _, row := range rateLimitWindowCases {
		if row.key == windowKey {
			return row.read, row.fallback
		}
	}
	t.Fatalf("no window row for %s", windowKey)
	return nil, 0
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
