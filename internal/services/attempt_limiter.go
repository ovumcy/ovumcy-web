package services

import (
	"strings"
	"sync"
	"time"
)

type AttemptLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
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
}

func (limiter *AttemptLimiter) AddFailureAll(keys []string, now time.Time, window time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	for _, key := range normalizeLimiterKeys(keys) {
		pruned := limiter.pruneLocked(key, now, window)
		pruned = append(pruned, now)
		limiter.attempts[key] = pruned
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
