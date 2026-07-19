package services

import (
	"fmt"
	"testing"
	"time"
)

// TestMR3Auth_AddFailureAllSweepCounterIncrements pins
// attempt_limiter.go:89:19 `limiter.addCallsN++` in AddFailureAll. With
// addCallsN one below the sweep threshold, a single AddFailureAll call must
// cross the threshold and trigger the stale-key sweep that removes the
// pre-seeded stale keys, leaving only the just-refreshed live key.
//
// Mutant kill: ++→-- leaves addCallsN below the threshold, so no sweep fires
// and the stale keys survive (map size != 1).
func TestMR3Auth_AddFailureAllSweepCounterIncrements(t *testing.T) {
	t.Parallel()

	window := time.Hour
	now := time.Now().UTC()
	past := now.Add(-2 * window) // most-recent failure well before the threshold

	limiter := NewAttemptLimiter()

	// One below the sweep threshold: the single AddFailureAll below must push
	// addCallsN to exactly evictEveryN and fire the sweep.
	limiter.addCallsN = evictEveryN - 1

	// Pre-seed stale keys directly so we don't perturb the call counter.
	staleCount := evictEveryN - 1
	for i := range staleCount {
		limiter.attempts[fmt.Sprintf("mr3auth-stale-%d", i)] = []time.Time{past}
	}

	liveKey := "mr3auth-live"
	limiter.AddFailureAll([]string{liveKey}, now, window)

	limiter.mu.Lock()
	mapLen := len(limiter.attempts)
	_, livePresent := limiter.attempts[liveKey]
	limiter.mu.Unlock()

	if mapLen != 1 {
		t.Fatalf("expected sweep to leave 1 key, got %d (kills ++→--, which skips the sweep)", mapLen)
	}
	if !livePresent {
		t.Fatal("expected the live key to survive the sweep")
	}
}

// TestMR3Auth_SweepSizeGuardBoundary pins the size term of the sweep-skip
// guard at attempt_limiter.go:100:62: `len(limiter.attempts) < evictAboveSize`.
// We construct map state where, at the guard check, addCallsN is low (so the
// counter term can never force a sweep) and len == evictAboveSize exactly.
// The original `<` makes `len < evictAboveSize` false → the guard is false →
// the sweep proceeds and removes the stale keys. The mutant `<=` makes it true
// → the whole guard is true → early return → stale keys survive.
func TestMR3Auth_SweepSizeGuardBoundary(t *testing.T) {
	t.Parallel()

	window := time.Hour
	now := time.Now().UTC()
	past := now.Add(-2 * window)

	limiter := NewAttemptLimiter()

	// Keep the call-counter term low so only the size term governs the guard.
	// AddFailureAll increments it to 1, still well below evictEveryN.
	limiter.addCallsN = 0

	liveKey := "mr3auth-live"
	// Seed exactly evictAboveSize keys: the live key (refreshed by the call
	// below, so it stays after the sweep) plus evictAboveSize-1 stale keys.
	// Because the live key already exists, AddFailureAll updates it in place
	// and does NOT grow the map, so len stays == evictAboveSize at the guard.
	limiter.attempts[liveKey] = []time.Time{past}
	staleCount := evictAboveSize - 1
	for i := range staleCount {
		limiter.attempts[fmt.Sprintf("mr3auth-stale-%05d", i)] = []time.Time{past}
	}

	if got := len(limiter.attempts); got != evictAboveSize {
		t.Fatalf("test setup invariant: expected %d seeded keys, got %d", evictAboveSize, got)
	}

	limiter.AddFailureAll([]string{liveKey}, now, window)

	limiter.mu.Lock()
	mapLen := len(limiter.attempts)
	_, livePresent := limiter.attempts[liveKey]
	limiter.mu.Unlock()

	// Original `<`: guard false at len==evictAboveSize → sweep removes all the
	// stale keys, leaving only the refreshed live key.
	if mapLen != 1 {
		t.Fatalf("expected the sweep to leave 1 key, got %d (kills <→<=, which early-returns at len==cap)", mapLen)
	}
	if !livePresent {
		t.Fatal("expected the refreshed live key to survive the sweep")
	}
}
