package services

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// evictEveryN triggers a full-map stale-key sweep every N AddFailureAll calls.
	// Keys are partly attacker-influenced (identity:<hmac>), so without periodic
	// eviction the map grows unboundedly until process restart. N=128 keeps the
	// sweep rare enough to be O(1) amortised while bounding residual memory.
	evictEveryN = 128

	// evictAboveSize caps the tracked keys PER SCOPE that are not enforcing a
	// lockout. Exceeding it in total triggers a sweep on every add, and after
	// the stale sweep each scope is trimmed back to this many unpinned entries —
	// a stale sweep alone cannot shrink the map when an attacker keeps minting
	// fresh keys inside the window. Two things the cap deliberately does not
	// count: a lockout (see attemptEntry.blocked), because evicting one lifts
	// it, and another scope's entries, because the cheapest flood — the login
	// form accepts any email — must not be able to erase a TOTP or recovery
	// budget. What bounds the rest is the attacker's own spend: a lockout costs
	// `limit` refused requests inside one window, and the scopes are the fixed
	// handful bootstrap wires.
	evictAboveSize = 1024
)

// AttemptBudget is what a policy judges its keys by. The limiter stores it on
// every entry recorded under that policy, because it is shared by policies with
// different budgets (login 8/15m, recovery 8/1h, totp 5/15m): a sweep or a
// trim that judged every entry by the calling policy's window would erase a
// budget that was still live under its own.
type AttemptBudget struct {
	Scope  string
	Limit  int
	Window time.Duration
}

type attemptEntry struct {
	// times is ordered oldest-first; pruneLocked keeps only the failures inside
	// the caller's window, so the newest is always last.
	times  []time.Time
	budget AttemptBudget
}

func (entry attemptEntry) newest() time.Time {
	return entry.times[len(entry.times)-1]
}

// expired reports whether the entry's own window has lapsed since its newest
// failure, so nothing in it can still count toward a lockout.
func (entry attemptEntry) expired(now time.Time) bool {
	return !entry.newest().Add(entry.budget.Window).After(now)
}

// blocked reports whether the entry currently enforces a lockout: at least
// `Limit` failures inside its own window. A blocked entry is pinned — neither
// the stale sweep nor the size-cap trim removes it — because evicting it would
// let the next attempt through, and minting fresh keys is the cheapest thing an
// attacker can do to this map.
func (entry attemptEntry) blocked(now time.Time) bool {
	if entry.budget.Limit < 1 {
		return false
	}
	threshold := now.Add(-entry.budget.Window)
	live := 0
	for _, value := range entry.times {
		if value.After(threshold) {
			live++
		}
	}
	return live >= entry.budget.Limit
}

type AttemptLimiter struct {
	mu        sync.Mutex
	attempts  map[string]attemptEntry
	addCallsN int // counts AddFailureAll invocations for eviction pacing
}

func NewAttemptLimiter() *AttemptLimiter {
	return &AttemptLimiter{
		attempts: make(map[string]attemptEntry),
	}
}

func NormalizeLimiterKey(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "unknown"
	}
	return key
}

func (limiter *AttemptLimiter) TooManyRecentAny(keys []string, now time.Time, limit int, window time.Duration) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for _, key := range normalizeLimiterKeys(keys) {
		if len(limiter.pruneLocked(key, now, window)) >= limit {
			return true
		}
	}
	return false
}

// AddFailureAll records one failure at `now` under every key, judged by budget.
func (limiter *AttemptLimiter) AddFailureAll(keys []string, now time.Time, budget AttemptBudget) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for _, key := range normalizeLimiterKeys(keys) {
		pruned := limiter.pruneLocked(key, now, budget.Window)
		limiter.attempts[key] = attemptEntry{
			times:  append(pruned, now),
			budget: budget,
		}
	}

	limiter.addCallsN++
	limiter.maybeEvictStaleLocked(now)
}

// maybeEvictStaleLocked performs an opportunistic full-map sweep to remove
// entries whose own window has lapsed, then trims every scope back to the
// evictAboveSize cap. It fires when either the call counter reaches evictEveryN
// or the map exceeds evictAboveSize entries. The sweep is O(n) in map size but
// runs rarely below the cap, keeping amortised cost negligible.
// Must be called with limiter.mu held.
func (limiter *AttemptLimiter) maybeEvictStaleLocked(now time.Time) {
	if limiter.addCallsN < evictEveryN && len(limiter.attempts) < evictAboveSize {
		return
	}
	limiter.addCallsN = 0

	for key, entry := range limiter.attempts {
		if len(entry.times) == 0 {
			delete(limiter.attempts, key) // codecov:ignore -- defensive; pruneLocked never stores an empty slice in the map
			continue
		}
		if entry.expired(now) {
			delete(limiter.attempts, key)
		}
	}

	limiter.enforceSizeCapLocked(now)
}

// enforceSizeCapLocked bounds every scope at evictAboveSize entries that are
// not enforcing a lockout, evicting the ones with the oldest most-recent
// failure first. Evicting the coldest loses the least: they are the closest to
// ageing out naturally, while the key an attacker is working on is among the
// freshest and is evicted last — so lifting one partial budget costs a full
// scope's worth of fresher keys, again for every guess. Must be called with
// limiter.mu held.
func (limiter *AttemptLimiter) enforceSizeCapLocked(now time.Time) {
	if len(limiter.attempts) <= evictAboveSize {
		return
	}

	type candidate struct {
		key    string
		newest time.Time
	}
	perScope := make(map[string][]candidate)
	for key, entry := range limiter.attempts {
		if entry.blocked(now) {
			continue
		}
		perScope[entry.budget.Scope] = append(perScope[entry.budget.Scope], candidate{key: key, newest: entry.newest()})
	}
	for _, candidates := range perScope {
		excess := len(candidates) - evictAboveSize
		if excess <= 0 {
			continue
		}
		sort.Slice(candidates, func(i, j int) bool {
			return candidates[i].newest.Before(candidates[j].newest)
		})
		for _, entry := range candidates[:excess] {
			delete(limiter.attempts, entry.key)
		}
	}
}

func (limiter *AttemptLimiter) ResetAll(keys []string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for _, key := range normalizeLimiterKeys(keys) {
		delete(limiter.attempts, key)
	}
}

func normalizeLimiterKeys(keys []string) []string {
	if len(keys) == 0 {
		return []string{NormalizeLimiterKey("")}
	}

	seen := make(map[string]struct{}, len(keys))
	normalized := make([]string, 0, len(keys))
	for _, key := range keys {
		candidate := NormalizeLimiterKey(key)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		normalized = append(normalized, candidate)
	}
	if len(normalized) == 0 {
		return []string{NormalizeLimiterKey("")}
	}
	return normalized
}

// pruneLocked drops the failures outside the caller's window and returns what
// is left, keeping the entry's recorded budget. Keys are scope-prefixed by the
// policy that owns them, so the caller's window is the entry's own.
func (limiter *AttemptLimiter) pruneLocked(key string, now time.Time, window time.Duration) []time.Time {
	entry, ok := limiter.attempts[key]
	if !ok || len(entry.times) == 0 {
		return []time.Time{}
	}

	threshold := now.Add(-window)
	pruned := make([]time.Time, 0, len(entry.times))
	for _, value := range entry.times {
		if value.After(threshold) {
			pruned = append(pruned, value)
		}
	}

	if len(pruned) == 0 {
		delete(limiter.attempts, key)
		return []time.Time{}
	}

	entry.times = pruned
	limiter.attempts[key] = entry
	return pruned
}
