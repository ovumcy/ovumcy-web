package main

import (
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/services"
)

// The logout endpoint carries two budgets that answer different questions:
//
//   - the per-IP edge limiter in front of DELETE /api/v1/sessions/current
//     (60 / 15 min), which bounds requests from one address and must stay wide
//     enough for a household instance behind one NAT;
//   - the per-account, identity-keyed budget AuthService enforces
//     (20 / 15 min), which bounds failures against one account.
//
// Until this split they read from ONE configuration pair, so the per-account
// limiter silently ran at the per-IP number — three times the documented budget
// — and no test could see it, because the wiring lived inside main(). Both tests
// below pin the two halves the audit found disagreeing with
// docs/security/auth-policy-and-rate-limits.md.

func minimalRuntimeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("SECRET_KEY", "0123456789abcdef0123456789abcdef")
	t.Setenv("DB_DRIVER", "sqlite")
	t.Setenv("DB_PATH", "data/ovumcy.db")
	// A relative CALENDAR_FEED_FENCE_PATH refuses the boot, so one exported by
	// the shell running the tests must not reach loadRuntimeConfig.
	t.Setenv("CALENDAR_FEED_FENCE_PATH", "")
}

// TestLogoutBudgetDefaultsAreTheDocumentedPair asserts the shipped defaults:
// 60 / 15 min per IP and 20 / 15 min per account, the latter defaulted from the
// service constants so the two cannot drift.
func TestLogoutBudgetDefaultsAreTheDocumentedPair(t *testing.T) {
	minimalRuntimeEnv(t)

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if config.RateLimits.LogoutMax != 60 || config.RateLimits.LogoutWindow != 15*time.Minute {
		t.Errorf("per-IP logout budget = %d / %s, want 60 / 15m",
			config.RateLimits.LogoutMax, config.RateLimits.LogoutWindow)
	}
	if config.RateLimits.LogoutAccountMax != services.DefaultLogoutAttemptsLimit {
		t.Errorf("per-account logout budget = %d, want services.DefaultLogoutAttemptsLimit (%d)",
			config.RateLimits.LogoutAccountMax, services.DefaultLogoutAttemptsLimit)
	}
	if config.RateLimits.LogoutAccountWindow != services.DefaultLogoutAttemptsWindow {
		t.Errorf("per-account logout window = %s, want services.DefaultLogoutAttemptsWindow (%s)",
			config.RateLimits.LogoutAccountWindow, services.DefaultLogoutAttemptsWindow)
	}
	if services.DefaultLogoutAttemptsLimit != 20 || services.DefaultLogoutAttemptsWindow != 15*time.Minute {
		t.Errorf("service default logout budget = %d / %s, want the documented 20 / 15m",
			services.DefaultLogoutAttemptsLimit, services.DefaultLogoutAttemptsWindow)
	}
}

// TestLogoutBudgetsMoveIndependently asserts the two budgets read from two
// environment pairs and that the per-account limiter is wired from the ACCOUNT
// pair. Setting each pair alone must move only its own half — the regression is
// a build where LogoutAttempts carries the per-IP numbers.
func TestLogoutBudgetsMoveIndependently(t *testing.T) {
	minimalRuntimeEnv(t)
	t.Setenv("RATE_LIMIT_LOGOUT_MAX", "37")
	t.Setenv("RATE_LIMIT_LOGOUT_WINDOW", "7m")
	t.Setenv("RATE_LIMIT_LOGOUT_ACCOUNT_MAX", "5")
	t.Setenv("RATE_LIMIT_LOGOUT_ACCOUNT_WINDOW", "3m")

	config, err := loadRuntimeConfig(time.UTC)
	if err != nil {
		t.Fatalf("load runtime config: %v", err)
	}

	if config.RateLimits.LogoutMax != 37 || config.RateLimits.LogoutWindow != 7*time.Minute {
		t.Errorf("per-IP logout budget = %d / %s, want 37 / 7m",
			config.RateLimits.LogoutMax, config.RateLimits.LogoutWindow)
	}
	if config.RateLimits.LogoutAccountMax != 5 || config.RateLimits.LogoutAccountWindow != 3*time.Minute {
		t.Errorf("per-account logout budget = %d / %s, want 5 / 3m",
			config.RateLimits.LogoutAccountMax, config.RateLimits.LogoutAccountWindow)
	}

	options := bootstrapOptions(config)
	if options.LogoutAttempts == nil {
		t.Fatal("bootstrap options carry no logout attempt limit; the per-account limiter would keep the service default and ignore configuration")
	}
	if options.LogoutAttempts.Max != config.RateLimits.LogoutAccountMax ||
		options.LogoutAttempts.Window != config.RateLimits.LogoutAccountWindow {
		t.Errorf("per-account limiter wired from %d / %s, want the ACCOUNT pair %d / %s (per-IP pair is %d / %s)",
			options.LogoutAttempts.Max, options.LogoutAttempts.Window,
			config.RateLimits.LogoutAccountMax, config.RateLimits.LogoutAccountWindow,
			config.RateLimits.LogoutMax, config.RateLimits.LogoutWindow)
	}
}
