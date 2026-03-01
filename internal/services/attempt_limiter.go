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

func (limiter *AttemptLimiter) AddFailure(key string, now time.Time, window time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	pruned := limiter.pruneLocked(key, now, window)
	pruned = append(pruned, now)
	limiter.attempts[key] = pruned
}

func (limiter *AttemptLimiter) Reset(key string) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	delete(limiter.attempts, key)
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
