package services

import (
	"fmt"
	"testing"
	"time"
)

// The tests in this file pin the eviction guarantees of the shared
// AttemptLimiter: neither the size-cap trim nor the stale sweep may lift a
// budget that is still live under its own policy, and one scope's flood does
// not reach another scope's budget. Each policy is wired the way bootstrap
// wires it — one limiter, several scopes with their own budgets — so the keys
// carry the real scope prefixes and identity fingerprints.

var evictionTestSecret = []byte("eviction-test-secret")

// churnFreshKeys records one failure under each of `count` never-seen login
// identities, the cheapest flood an attacker can aim at the shared map: the
// login form accepts any email, and each email mints its own identity key.
func churnFreshKeys(policy *AuthAttemptPolicy, from time.Time, count int) time.Time {
	at := from
	for index := range count {
		at = from.Add(time.Duration(index+1) * time.Millisecond)
		policy.AddFailure(evictionTestSecret, fmt.Sprintf("198.51.100.%d", index%200), fmt.Sprintf("churn-%05d@example.com", index), at)
	}
	return at
}

// TestAttemptLimiterChurnDoesNotLiftActiveLockout: a victim locked out under
// the login budget stays locked out after its scope is flooded past the cap
// with fresh keys. Before the pin, the trim evicted the entries with the oldest
// most-recent failure — which is exactly the victim, because the lockout is
// what stopped the attacker from touching that key.
func TestAttemptLimiterChurnDoesNotLiftActiveLockout(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	login := NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow)
	base := time.Now().UTC()

	for range DefaultLoginAttemptsLimit {
		login.AddFailure(evictionTestSecret, "203.0.113.7", "victim@example.com", base)
	}
	if !login.TooManyRecent(evictionTestSecret, "203.0.113.7", "victim@example.com", base) {
		t.Fatal("setup: the victim must be locked out before the churn")
	}

	after := churnFreshKeys(login, base, evictAboveSize+1)

	if !login.TooManyRecent(evictionTestSecret, "203.0.113.7", "victim@example.com", after) {
		t.Fatal("churning fresh keys past the cap lifted the victim's login lockout")
	}
	// The identity key alone must hold as well: a lockout that survives only
	// through the client key is lifted by the attacker's next address.
	if !login.TooManyRecent(evictionTestSecret, "198.51.100.250", "victim@example.com", after) {
		t.Fatal("the victim's identity-keyed lockout did not survive the churn")
	}
}

// TestAttemptLimiterChurnDoesNotEraseTOTPBudget: the totp scope shares the
// limiter with login, and a flood of login identities must neither lift a TOTP
// lockout nor erase a partially spent TOTP budget — a 2FA code has one million
// values, so every erased failure is a free guess. The cap is per scope, so a
// login flood, however large, never reaches a totp entry at all.
func TestAttemptLimiterChurnDoesNotEraseTOTPBudget(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	login := NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow)
	totp := NewAuthAttemptPolicy("totp", limiter, DefaultTOTPAttemptsLimit, DefaultTOTPAttemptsWindow)
	base := time.Now().UTC()

	const lockedUser = "41"
	for range DefaultTOTPAttemptsLimit {
		totp.AddFailure(evictionTestSecret, "203.0.113.7", lockedUser, base)
	}
	const partialUser = "42"
	partial := DefaultTOTPAttemptsLimit - 2
	for range partial {
		totp.AddFailure(evictionTestSecret, "203.0.113.8", partialUser, base)
	}

	after := churnFreshKeys(login, base, evictAboveSize+1)

	if !totp.TooManyRecent(evictionTestSecret, "203.0.113.7", lockedUser, after) {
		t.Fatal("login churn lifted the TOTP lockout")
	}
	// The partial budget must still hold its count: the remaining two failures
	// must be the ones that lock the account, not the first two of a fresh five.
	for range DefaultTOTPAttemptsLimit - partial {
		if totp.TooManyRecent(evictionTestSecret, "203.0.113.8", partialUser, after) {
			t.Fatal("the partial TOTP budget locked out before its remaining failures were spent")
		}
		totp.AddFailure(evictionTestSecret, "203.0.113.8", partialUser, after)
	}
	if !totp.TooManyRecent(evictionTestSecret, "203.0.113.8", partialUser, after) {
		t.Fatalf("login churn erased the partial TOTP budget: %d earlier failures were forgotten", partial)
	}
}

// TestAttemptLimiterSweepRespectsEachEntryWindow: the stale sweep used to judge
// every entry by the window of the policy whose failure triggered it. A login
// failure (15 minutes) then erased a recovery lockout (1 hour) whose newest
// failure was twenty minutes old — forty minutes early.
func TestAttemptLimiterSweepRespectsEachEntryWindow(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	login := NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow)
	// The hour is what bootstrap configures from RATE_LIMIT_FORGOT_PASSWORD_WINDOW's
	// default; the package constant is the shorter pre-configuration value.
	recovery := NewAuthAttemptPolicy("recovery", limiter, DefaultRecoveryAttemptsLimit, time.Hour)

	now := time.Now().UTC()
	recordedAt := now.Add(-(DefaultLoginAttemptsWindow + 5*time.Minute))
	for range DefaultRecoveryAttemptsLimit {
		recovery.AddFailure(evictionTestSecret, "203.0.113.7", "victim@example.com", recordedAt)
	}

	// Enough login failures at `now` to cross the sweep counter, well below
	// the size cap so the trim plays no part.
	for index := range evictEveryN {
		login.AddFailure(evictionTestSecret, "198.51.100.1", fmt.Sprintf("sweep-%03d@example.com", index), now)
	}

	if !recovery.TooManyRecent(evictionTestSecret, "203.0.113.7", "victim@example.com", now) {
		t.Fatal("a login-window sweep erased the recovery lockout forty minutes before its own window lapsed")
	}
}

// TestAttemptLimiterSweepStillDropsExpiredEntries proves the per-entry expiry
// did not disarm the sweep: an entry whose own window has lapsed goes, and at
// the boundary — newest failure exactly one window ago — it is expired.
func TestAttemptLimiterSweepStillDropsExpiredEntries(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	now := time.Now().UTC()
	window := 15 * time.Minute
	budget := AttemptBudget{Limit: 1, Window: window}

	limiter.attempts["expired:boundary"] = attemptEntry{times: []time.Time{now.Add(-window)}, budget: budget}
	limiter.attempts["expired:long-ago"] = attemptEntry{times: []time.Time{now.Add(-2 * window)}, budget: budget}
	limiter.attempts["live:just-inside"] = attemptEntry{times: []time.Time{now.Add(-window + time.Second)}, budget: budget}
	limiter.addCallsN = evictEveryN - 1

	limiter.AddFailureAll([]string{"live:trigger"}, now, budget)

	limiter.mu.Lock()
	_, boundaryPresent := limiter.attempts["expired:boundary"]
	_, longAgoPresent := limiter.attempts["expired:long-ago"]
	_, insidePresent := limiter.attempts["live:just-inside"]
	limiter.mu.Unlock()

	if boundaryPresent || longAgoPresent {
		t.Fatalf("expired entries survived the sweep: boundary=%t long-ago=%t", boundaryPresent, longAgoPresent)
	}
	if !insidePresent {
		t.Fatal("an entry one second inside its own window was swept")
	}
}

// TestAttemptLimiterBelowCapNothingIsTrimmed: under the cap the sweep only
// drops expired entries, so a churn that stays below evictAboveSize leaves
// every live entry — lockout or not — in place.
func TestAttemptLimiterBelowCapNothingIsTrimmed(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	login := NewAuthAttemptPolicy("login", limiter, DefaultLoginAttemptsLimit, DefaultLoginAttemptsWindow)
	base := time.Now().UTC()

	login.AddFailure(evictionTestSecret, "203.0.113.7", "victim@example.com", base)
	// Each churn call adds a client key and an identity key; stay below the
	// cap counting both, with room for the victim's pair.
	churned := (evictAboveSize - 4) / 2
	after := churnFreshKeys(login, base, churned)

	limiter.mu.Lock()
	size := len(limiter.attempts)
	limiter.mu.Unlock()
	if size > evictAboveSize {
		t.Fatalf("test setup: %d tracked keys exceed the cap %d", size, evictAboveSize)
	}
	// One more failure must be the second of two, not the first of a fresh count.
	login.AddFailure(evictionTestSecret, "203.0.113.7", "victim@example.com", after)
	if !limiter.TooManyRecentAny(login.keys(evictionTestSecret, "203.0.113.7", "victim@example.com"), after, 2, DefaultLoginAttemptsWindow) {
		t.Fatal("a churn below the cap erased a single-failure entry")
	}
}

// TestAttemptLimiterPinBoundaryIsTheLimit pins where the pin starts. An entry
// with limit-1 failures is not a lockout, so a flood of fresher keys in its
// scope evicts it; one more failure makes it a lockout the same flood cannot
// touch.
func TestAttemptLimiterPinBoundaryIsTheLimit(t *testing.T) {
	t.Parallel()

	const limit = 3
	budget := AttemptBudget{Scope: "login", Limit: limit, Window: time.Hour}

	flood := func(limiter *AttemptLimiter, from time.Time) time.Time {
		at := from
		for index := range evictAboveSize + 1 {
			at = from.Add(time.Duration(index+1) * time.Millisecond)
			limiter.AddFailureAll([]string{fmt.Sprintf("flood:%05d", index)}, at, budget)
		}
		return at
	}

	t.Run("one below the limit is evictable", func(t *testing.T) {
		t.Parallel()
		limiter := NewAttemptLimiter()
		base := time.Now().UTC()
		for range limit - 1 {
			limiter.AddFailureAll([]string{"victim"}, base, budget)
		}
		after := flood(limiter, base)

		limiter.mu.Lock()
		_, present := limiter.attempts["victim"]
		size := len(limiter.attempts)
		limiter.mu.Unlock()
		if present {
			t.Fatal("an entry below its limit was pinned; the cap could not hold under a flood of partial budgets")
		}
		if size > evictAboveSize {
			t.Fatalf("tracked keys = %d, want <= %d", size, evictAboveSize)
		}
		if limiter.TooManyRecentAny([]string{"victim"}, after, limit, budget.Window) {
			t.Fatal("an evicted entry still reads as locked out")
		}
	})

	t.Run("at the limit is pinned", func(t *testing.T) {
		t.Parallel()
		limiter := NewAttemptLimiter()
		base := time.Now().UTC()
		for range limit {
			limiter.AddFailureAll([]string{"victim"}, base, budget)
		}
		after := flood(limiter, base)

		if !limiter.TooManyRecentAny([]string{"victim"}, after, limit, budget.Window) {
			t.Fatal("the flood evicted an entry at its limit and lifted the lockout")
		}
	})
}

// TestAttemptLimiterCapIsPerScope: a flood in one scope trims that scope only.
// The other scope's entries are neither counted toward the flood's cap nor
// evicted by it, however cold they are.
func TestAttemptLimiterCapIsPerScope(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	base := time.Now().UTC()
	totp := AttemptBudget{Scope: "totp", Limit: DefaultTOTPAttemptsLimit, Window: DefaultTOTPAttemptsWindow}
	login := AttemptBudget{Scope: "login", Limit: DefaultLoginAttemptsLimit, Window: DefaultLoginAttemptsWindow}

	const totpKeys = 50
	for index := range totpKeys {
		limiter.AddFailureAll([]string{fmt.Sprintf("totp:client:%d", index)}, base, totp)
	}
	for index := range evictAboveSize + 100 {
		limiter.AddFailureAll([]string{fmt.Sprintf("login:identity:%05d", index)}, base.Add(time.Duration(index+1)*time.Millisecond), login)
	}

	limiter.mu.Lock()
	kept := 0
	for key := range limiter.attempts {
		if len(key) > 5 && key[:5] == "totp:" {
			kept++
		}
	}
	size := len(limiter.attempts)
	limiter.mu.Unlock()

	if kept != totpKeys {
		t.Fatalf("a login flood evicted totp entries: %d of %d kept", kept, totpKeys)
	}
	if size-kept > evictAboveSize {
		t.Fatalf("login scope holds %d unpinned keys, want <= %d", size-kept, evictAboveSize)
	}
}

// TestAttemptLimiterPinnedEntriesMayExceedTheCap documents the trade-off the
// pin makes: when every entry is a lockout there is nothing the trim may
// evict, so the map holds all of them until their own windows lapse. Each such
// entry cost the attacker `limit` refused requests, which is what bounds it —
// and a fresh key still accumulates in that state, so a saturated map does not
// stop a new lockout from forming.
func TestAttemptLimiterPinnedEntriesMayExceedTheCap(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	now := time.Now().UTC()
	budget := AttemptBudget{Scope: "login", Limit: 2, Window: 15 * time.Minute}
	total := evictAboveSize + 10

	for index := range total {
		key := []string{fmt.Sprintf("locked:%05d", index)}
		for range budget.Limit {
			limiter.AddFailureAll(key, now, budget)
		}
	}

	limiter.mu.Lock()
	size := len(limiter.attempts)
	limiter.mu.Unlock()
	if size != total {
		t.Fatalf("tracked keys = %d, want all %d lockouts kept", size, total)
	}

	// Once their windows lapse the next sweep reclaims them all.
	limiter.addCallsN = evictEveryN - 1
	limiter.AddFailureAll([]string{"later"}, now.Add(budget.Window+time.Second), budget)
	limiter.mu.Lock()
	size = len(limiter.attempts)
	limiter.mu.Unlock()
	if size != 1 {
		t.Fatalf("tracked keys after the windows lapsed = %d, want 1", size)
	}
}
