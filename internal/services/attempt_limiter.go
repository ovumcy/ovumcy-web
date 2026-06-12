package services

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// evictEveryN triggers a full-map stale-key sweep every N AddFailure calls.
	// Keys are partly attacker-influenced (identity:<hmac>), so without periodic
	// eviction the map grows unboundedly until process restart. N=128 keeps the
	// sweep rare enough to be O(1) amortised while bounding residual memory.
	evictEveryN = 128

	// evictAboveSize is the hard cap on tracked keys. Exceeding it triggers a
	// sweep on every add, and after the stale sweep the map is trimmed back to
	// this size by evicting the keys with the oldest most-recent failure — a
	// stale sweep alone cannot shrink the map when an attacker keeps minting
	// fresh keys inside the window. Evicting the coldest keys loses the least:
	// they are the closest to ageing out naturally, while an actively
	// brute-forced key is among the freshest and is evicted last.
	evictAboveSize = 1024
)

type AttemptLimiter struct {
	mu        sync.Mutex
	attempts  map[string][]time.Time
	addCallsN int // counts AddFailure/AddFailureAll invocations for eviction pacing
}

func NewAttemptLimiter() *AttemptLimiter {
	return &AttemptLimiter{
		attempts: make(map[string][]time.Time),
	}
}

func NormalizeLimiterKey(raw string) string {
	key := strings.TrimSpace(raw)
	if key == "" {
		return "unknown"
	}
	return key
}

func (limiter *AttemptLimiter) TooManyRecent(key string, now time.Time, limit int, window time.Duration) bool {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	pruned := limiter.pruneLocked(key, now, window)
	return len(pruned) >= limit
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

func (limiter *AttemptLimiter) AddFailure(key string, now time.Time, window time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	pruned := limiter.pruneLocked(key, now, window)
	pruned = append(pruned, now)
	limiter.attempts[key] = pruned

	limiter.addCallsN++
	limiter.maybeEvictStaleLocked(now, window)
}

func (limiter *AttemptLimiter) AddFailureAll(keys []string, now time.Time, window time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for _, key := range normalizeLimiterKeys(keys) {
		pruned := limiter.pruneLocked(key, now, window)
		pruned = append(pruned, now)
		limiter.attempts[key] = pruned
	}

	limiter.addCallsN++
	limiter.maybeEvictStaleLocked(now, window)
}

// maybeEvictStaleLocked performs an opportunistic full-map sweep to remove keys
// whose most-recent attempt is older than window, then trims the map back to
// the evictAboveSize hard cap. It fires when either the call counter reaches
// evictEveryN or the map exceeds evictAboveSize entries. The sweep is O(n) in
// map size but runs rarely below the cap, keeping amortised cost negligible.
// Must be called with limiter.mu held.
func (limiter *AttemptLimiter) maybeEvictStaleLocked(now time.Time, window time.Duration) {
	if limiter.addCallsN < evictEveryN && len(limiter.attempts) < evictAboveSize {
		return
	}
	limiter.addCallsN = 0

	threshold := now.Add(-window)
	for key, times := range limiter.attempts {
		if len(times) == 0 {
			delete(limiter.attempts, key) // codecov:ignore -- defensive; pruneLocked never stores an empty slice in the map
			continue
		}
		// times is ordered oldest-first after pruneLocked; newest is last.
		if times[len(times)-1].Before(threshold) || times[len(times)-1].Equal(threshold) {
			delete(limiter.attempts, key)
		}
	}

	limiter.enforceSizeCapLocked()
}

// enforceSizeCapLocked bounds the map at evictAboveSize keys by evicting the
// keys with the oldest most-recent failure first. The stale sweep alone only
// removes keys whose window has lapsed, so under a sustained attack that
// keeps minting fresh keys (identity:<hmac> material is attacker-influenced)
// the map would otherwise grow without bound for the full window length.
// Must be called with limiter.mu held.
func (limiter *AttemptLimiter) enforceSizeCapLocked() {
	excess := len(limiter.attempts) - evictAboveSize
	if excess <= 0 {
		return
	}

	type keyAge struct {
		key    string
		newest time.Time
	}
	ages := make([]keyAge, 0, len(limiter.attempts))
	for key, times := range limiter.attempts {
		ages = append(ages, keyAge{key: key, newest: times[len(times)-1]})
	}
	sort.Slice(ages, func(i, j int) bool {
		return ages[i].newest.Before(ages[j].newest)
	})
	for _, entry := range ages[:excess] {
		delete(limiter.attempts, entry.key)
	}
}

func (limiter *AttemptLimiter) Reset(key string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	delete(limiter.attempts, key)
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

func (limiter *AttemptLimiter) pruneLocked(key string, now time.Time, window time.Duration) []time.Time {
	values := limiter.attempts[key]
	if len(values) == 0 {
		return []time.Time{}
	}

	threshold := now.Add(-window)
	pruned := make([]time.Time, 0, len(values))
	for _, value := range values {
		if value.After(threshold) {
			pruned = append(pruned, value)
		}
	}

	if len(pruned) == 0 {
		delete(limiter.attempts, key)
		return []time.Time{}
	}

	limiter.attempts[key] = pruned
	return pruned
}
