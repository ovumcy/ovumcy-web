package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// docs/security/auth-policy-and-rate-limits.md states numbers — per-IP budgets
// with the environment variables that tune them, and per-account lockout
// thresholds — and until this file no test and no hook read any of them back.
// The precedent is scripts/webhookdocs (the SECURITY.md matrix against the code)
// and owner_only_coverage_regression_test.go (the OwnerOnly exclusion set against
// its bullet, in both directions); both run inside `go test ./...`, so they block.
//
// BOTH DIRECTIONS, because one is not a check. A value in the doc absent from
// code is a stale claim; an environment variable in code absent from the doc is
// an undocumented control, and pinning only the first leaves the class fixed at
// N of N+1 sites, which the repo's own measurement rules call a new defect rather
// than a partial fix.
//
// WHAT IS PINNED IS THE BOOT-RESOLVED VALUE, not the nearest constant. Three of
// the per-account budgets are overridden at boot from the configuration
// (bootstrapOptions), and the constant they fall back to is NOT what ships:
// services.DefaultRecoveryAttemptsWindow is 15 minutes while the recovery budget
// the doc states, and the server runs, is the RATE_LIMIT_FORGOT_PASSWORD_WINDOW
// default of one hour. Asserting against the constant would pin a value no
// request ever meets.
//
// THE CEILINGS ARE PINNED AS NUMBERS, NOT AS BEHAVIOUR. rate_limit_ceilings_test.go
// already proves each setting accepts its ceiling and refuses one past it; what
// nothing read until here is the doc's sentence stating those ceilings, so a
// ceiling retuned in config.go left the doc promising the old bound.

const authPolicyDoc = "auth-policy-and-rate-limits.md"

type documentedBudget struct {
	max    int
	window time.Duration
}

func authPolicyDocText(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "security", authPolicyDoc)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// parseDocDuration reads the doc's own spelling ("15 minutes", "1 hour"). It is
// deliberately narrow: a unit it does not know fails the test rather than
// silently comparing against a zero duration.
func parseDocDuration(t *testing.T, text string) time.Duration {
	t.Helper()
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 2 {
		t.Fatalf("cannot read a duration from %q", text)
	}
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("cannot read a duration from %q: %v", text, err)
	}
	switch strings.TrimSuffix(fields[1], "s") {
	case "minute":
		return time.Duration(n) * time.Minute
	case "hour":
		return time.Duration(n) * time.Hour
	case "second":
		return time.Duration(n) * time.Second
	}
	t.Fatalf("unknown duration unit in %q", text)
	return 0
}

var (
	rateLimitEnvPattern = regexp.MustCompile(`RATE_LIMIT_[A-Z0-9_]+\*?`)
	docTableRowPattern  = regexp.MustCompile(`(?m)^\|\s*` + "`" + `[^|]+\|\s*(\d+) requests / ([^|]+?)\s*\|([^|]*)\|`)
	// Every endpoint row opens with a code-formatted cell; the header and the
	// separator do not, so this counts rows without reading their budgets.
	docTableRowCount = regexp.MustCompile(`(?m)^\|\s*` + "`")
	// A bullet's budget can be followed by a comma, a full stop, or a
	// parenthetical aside ("20 per 15 minutes (account-scoped)").
	docBulletPattern = regexp.MustCompile(`(?m)^- (.+?)(?: \([^)]*\))?: (\d+) (?:failures /|per) ([a-z0-9 ]+?)(?:[,.]|\s+\()`)
)

// TestAuthPolicyDocPinsTheRateLimitEnvVariables checks the per-IP table against
// the loaded configuration, and the set of RATE_LIMIT_* names three ways: the
// doc, the source that reads them, and this test's own expectations. A variable
// added to config.go without a row in the doc fails here, and so does a row in
// the doc naming a variable nothing reads.
func TestAuthPolicyDocPinsTheRateLimitEnvVariables(t *testing.T) {
	minimalRuntimeEnv(t)

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	maxByEnv := map[string]int{
		"RATE_LIMIT_LOGIN_MAX":           config.RateLimits.LoginMax,
		"RATE_LIMIT_REGISTER_MAX":        config.RateLimits.RegisterMax,
		"RATE_LIMIT_FORGOT_PASSWORD_MAX": config.RateLimits.ForgotPasswordMax,
		"RATE_LIMIT_LOGOUT_MAX":          config.RateLimits.LogoutMax,
		"RATE_LIMIT_LOGOUT_ACCOUNT_MAX":  config.RateLimits.LogoutAccountMax,
		"RATE_LIMIT_API_MAX":             config.RateLimits.APIMax,
		"RATE_LIMIT_CALENDAR_FEED_MAX":   config.RateLimits.CalendarFeedMax,
		"RATE_LIMIT_CALENDAR_MAX":        config.RateLimits.CalendarMax,
	}
	windowByEnv := map[string]time.Duration{
		"RATE_LIMIT_LOGIN_WINDOW":           config.RateLimits.LoginWindow,
		"RATE_LIMIT_REGISTER_WINDOW":        config.RateLimits.RegisterWindow,
		"RATE_LIMIT_FORGOT_PASSWORD_WINDOW": config.RateLimits.ForgotPasswordWindow,
		"RATE_LIMIT_LOGOUT_WINDOW":          config.RateLimits.LogoutWindow,
		"RATE_LIMIT_LOGOUT_ACCOUNT_WINDOW":  config.RateLimits.LogoutAccountWindow,
		"RATE_LIMIT_API_WINDOW":             config.RateLimits.APIWindow,
		"RATE_LIMIT_CALENDAR_FEED_WINDOW":   config.RateLimits.CalendarFeedWindow,
		"RATE_LIMIT_CALENDAR_WINDOW":        config.RateLimits.CalendarWindow,
	}

	known := map[string]bool{}
	for name := range maxByEnv {
		known[name] = true
	}
	for name := range windowByEnv {
		known[name] = true
	}

	inCode := rateLimitEnvNames(serverSourceText(t))
	doc := authPolicyDocText(t)
	inDoc := rateLimitEnvNames(doc)

	assertSameNames(t, "the server sources", inCode, "the doc", inDoc)
	assertSameNames(t, "the server sources", inCode, "this test's expectations", known)

	rows := docTableRowPattern.FindAllStringSubmatch(doc, -1)
	// A row this pattern cannot read would be dropped in SILENCE, and its budget
	// is checked by nothing else — the name comparison above passes on a row that
	// reuses an already-documented variable. Count the rows the table has and
	// insist every one of them parsed.
	if written := docTableRowCount.FindAllString(doc, -1); len(rows) != len(written) {
		t.Fatalf("%s: %d table row(s) written, %d parsed — a row's budget wording changed and its numbers are now read by nothing",
			authPolicyDoc, len(written), len(rows))
	}
	if len(rows) == 0 {
		t.Fatal("no rate-limit rows parsed from the doc table; the table's shape changed and this test reads nothing")
	}
	for _, row := range rows {
		budget := documentedBudget{max: mustAtoi(t, row[1]), window: parseDocDuration(t, row[2])}
		names := rateLimitEnvPattern.FindAllString(row[3], -1)
		if len(names) == 0 {
			t.Errorf("doc row %q names no RATE_LIMIT_* variable", strings.TrimSpace(row[0]))
			continue
		}
		for _, name := range names {
			if value, ok := maxByEnv[name]; ok && value != budget.max {
				t.Errorf("%s: doc says %d requests, %s defaults to %d", authPolicyDoc, budget.max, name, value)
			}
			if value, ok := windowByEnv[name]; ok && value != budget.window {
				t.Errorf("%s: doc says a %s window, %s defaults to %s", authPolicyDoc, budget.window, name, value)
			}
		}
	}
}

// TestAuthPolicyDocPinsThePerAccountLockoutThresholds reads the bullets under the
// per-account heading against what bootstrapOptions actually wires, so a budget
// retuned in code without the doc following — or the reverse — fails.
func TestAuthPolicyDocPinsThePerAccountLockoutThresholds(t *testing.T) {
	minimalRuntimeEnv(t)

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}
	options := bootstrapOptions(config)
	if options.LogoutAttempts == nil {
		t.Fatal("bootstrapOptions left LogoutAttempts nil; the account budget would fall back to the service default unread")
	}

	expected := map[string]documentedBudget{
		"Recovery-code redemption":   {options.RecoveryAttempts.Max, options.RecoveryAttempts.Window},
		"Login attempts":             {options.LoginAttempts.Max, options.LoginAttempts.Window},
		"Logout attempts":            {options.LogoutAttempts.Max, options.LogoutAttempts.Window},
		"TOTP login challenge":       {services.DefaultTOTPAttemptsLimit, services.DefaultTOTPAttemptsWindow},
		"TOTP disable":               {services.DefaultTOTPDisableAttemptsLimit, services.DefaultTOTPDisableAttemptsWindow},
		"Settings re-authentication": {services.DefaultSettingsReauthAttemptsLimit, services.DefaultSettingsReauthAttemptsWindow},
	}

	doc := authPolicyDocText(t)
	marker := strings.Index(doc, "Plus per-account, identity-keyed budgets")
	if marker < 0 {
		t.Fatal("the per-account heading is gone from the doc; this test reads nothing")
	}

	documented := map[string]documentedBudget{}
	for _, bullet := range docBulletPattern.FindAllStringSubmatch(doc[marker:], -1) {
		documented[strings.TrimSpace(bullet[1])] = documentedBudget{
			max:    mustAtoi(t, bullet[2]),
			window: parseDocDuration(t, bullet[3]),
		}
	}

	docNames := map[string]bool{}
	for name := range documented {
		docNames[name] = true
	}
	codeNames := map[string]bool{}
	for name := range expected {
		codeNames[name] = true
	}
	assertSameNames(t, "the doc", docNames, "what the boot wires", codeNames)

	for name, want := range expected {
		got, ok := documented[name]
		if !ok {
			continue // already reported by assertSameNames
		}
		if got != want {
			t.Errorf("%s: %q documented as %d / %s, the boot wires %d / %s",
				authPolicyDoc, name, got.max, got.window, want.max, want.window)
		}
	}
}

var (
	docCeilingPattern      = regexp.MustCompile(`(\d+) on the ([A-Za-z -]+?)(?: \(|,| and )`)
	docWindowBoundsPattern = regexp.MustCompile("`\\*_WINDOW` must lie between (one [a-z]+) and (one [a-z]+)")
	docCeilingPhraseToEnvs = map[string][]string{
		"three credential endpoints": {"RATE_LIMIT_LOGIN_MAX", "RATE_LIMIT_REGISTER_MAX", "RATE_LIMIT_FORGOT_PASSWORD_MAX"},
		"per-IP logout row":          {"RATE_LIMIT_LOGOUT_MAX"},
		"per-account logout budget":  {"RATE_LIMIT_LOGOUT_ACCOUNT_MAX"},
		"API catch-all":              {"RATE_LIMIT_API_MAX"},
		"calendar page":              {"RATE_LIMIT_CALENDAR_MAX"},
		"calendar feed":              {"RATE_LIMIT_CALENDAR_FEED_MAX"},
	}
	docWindowWords = map[string]time.Duration{"one second": time.Second, "one minute": time.Minute, "one hour": time.Hour, "one day": 24 * time.Hour}
)

// TestAuthPolicyDocPinsTheRateLimitCeilings reads the ceilings sentence against
// rateLimitCeilingCases in both directions: a ceiling the doc states for a
// setting must be the one config.go enforces, and every setting with a ceiling
// must be named in the sentence.
func TestAuthPolicyDocPinsTheRateLimitCeilings(t *testing.T) {
	doc := authPolicyDocText(t)
	start := strings.Index(doc, "Every setting above has a ceiling as well as a floor")
	if start < 0 {
		t.Fatal("the ceilings paragraph is gone from the doc; this test reads nothing")
	}
	paragraph := doc[start:]
	if end := strings.Index(paragraph, "\n\n"); end >= 0 {
		paragraph = paragraph[:end]
	}
	paragraph = strings.Join(strings.Fields(paragraph), " ")

	ceilingByEnv := map[string]int{}
	for _, tc := range rateLimitCeilingCases {
		ceilingByEnv[tc.key] = tc.ceiling
	}

	documented := map[string]bool{}
	matches := docCeilingPattern.FindAllStringSubmatch(paragraph, -1)
	if len(matches) == 0 {
		t.Fatal("no ceiling parsed from the doc's ceilings paragraph; its wording changed and this test reads nothing")
	}
	for _, match := range matches {
		envs, ok := docCeilingPhraseToEnvs[match[2]]
		if !ok {
			t.Errorf("%s states a ceiling for %q, which this test maps to no setting", authPolicyDoc, match[2])
			continue
		}
		stated := mustAtoi(t, match[1])
		for _, env := range envs {
			documented[env] = true
			if enforced, ok := ceilingByEnv[env]; !ok {
				t.Errorf("%s states a ceiling of %d for %s, which has no ceiling in config.go", authPolicyDoc, stated, env)
			} else if enforced != stated {
				t.Errorf("%s states a ceiling of %d for %s, config.go enforces %d", authPolicyDoc, stated, env, enforced)
			}
		}
	}
	for env := range ceilingByEnv {
		if !documented[env] {
			t.Errorf("%s has a ceiling in config.go that %s does not state", env, authPolicyDoc)
		}
	}

	bounds := docWindowBoundsPattern.FindStringSubmatch(paragraph)
	if bounds == nil {
		t.Fatal("the doc's window bounds sentence is gone; the [floor, ceiling] of every *_WINDOW is read by nothing")
	}
	floor, floorOK := docWindowWords[bounds[1]]
	ceiling, ceilingOK := docWindowWords[bounds[2]]
	if !floorOK || !ceilingOK {
		t.Fatalf("cannot read the window bounds from %q", bounds[0])
	}
	if floor != rateLimitWindowFloor || ceiling != rateLimitWindowCeiling {
		t.Errorf("%s bounds every window to [%s, %s], config.go enforces [%s, %s]",
			authPolicyDoc, floor, ceiling, rateLimitWindowFloor, rateLimitWindowCeiling)
	}
}

// serverSourceText reads every non-test server source, not just config.go: a
// RATE_LIMIT_* variable read from anywhere else would otherwise be invisible to
// both directions of this check, which is the N-of-N+1 shape the repo's
// measurement rules call a new defect rather than a partial fix.
func serverSourceText(t *testing.T) string {
	t.Helper()
	var builder strings.Builder
	for _, tree := range []string{".", filepath.Join("..", "..", "internal")} {
		err := filepath.WalkDir(tree, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			builder.Write(data)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", tree, err)
		}
	}
	if builder.Len() == 0 {
		t.Fatal("no server sources read; this test's setup is wrong and it would pass on an empty corpus")
	}
	return builder.String()
}

// rateLimitEnvNames drops the glob forms the prose uses — "the
// RATE_LIMIT_LOGOUT_* per-IP row" names a pair, not a variable, and neither side
// can be missing it.
func rateLimitEnvNames(text string) map[string]bool {
	names := map[string]bool{}
	for _, name := range rateLimitEnvPattern.FindAllString(text, -1) {
		if strings.HasSuffix(name, "*") || strings.HasSuffix(name, "_") {
			continue
		}
		names[name] = true
	}
	return names
}

func assertSameNames(t *testing.T, leftLabel string, left map[string]bool, rightLabel string, right map[string]bool) {
	t.Helper()
	for _, missing := range missingFrom(left, right) {
		t.Errorf("%s names %q, %s does not", leftLabel, missing, rightLabel)
	}
	for _, missing := range missingFrom(right, left) {
		t.Errorf("%s names %q, %s does not", rightLabel, missing, leftLabel)
	}
}

func missingFrom(have map[string]bool, want map[string]bool) []string {
	var missing []string
	for name := range have {
		if !want[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

func mustAtoi(t *testing.T, text string) int {
	t.Helper()
	n, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("cannot read a count from %q: %v", text, err)
	}
	return n
}
