package services

import (
	"fmt"
	"testing"
	"time"
)

func TestAttemptLimiterWindowAndReset(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	key := "127.0.0.1"
	window := time.Hour
	now := time.Now().UTC()

	limiter.AddFailure(key, now.Add(-2*time.Hour), window)
	if limiter.TooManyRecent(key, now, 1, window) {
		t.Fatal("expected old attempt to be pruned from active window")
	}

	limiter.AddFailure(key, now.Add(-30*time.Minute), window)
	if !limiter.TooManyRecent(key, now, 1, window) {
		t.Fatal("expected one recent attempt to hit limit 1")
	}

	limiter.Reset(key)
	if limiter.TooManyRecent(key, now, 1, window) {
		t.Fatal("expected no attempts after reset")
	}
}

func TestAttemptLimiterMultiKeyOperations(t *testing.T) {
	t.Parallel()

	limiter := NewAttemptLimiter()
	now := time.Now().UTC()
	window := time.Hour
	keys := []string{"127.0.0.1", " owner@example.com ", "127.0.0.1"}

	limiter.AddFailureAll(keys, now, window)
	if !limiter.TooManyRecentAny([]string{"127.0.0.1"}, now, 1, window) {
		t.Fatal("expected client limiter entry to be recorded")
	}
	if !limiter.TooManyRecentAny([]string{"owner@example.com"}, now, 1, window) {
		t.Fatal("expected identity limiter entry to be recorded")
	}

	limiter.ResetAll(keys)
	if limiter.TooManyRecentAny([]string{"127.0.0.1", "owner@example.com"}, now, 1, window) {
		t.Fatal("expected no attempts after multi-key reset")
	}
}

// TestAttemptLimiterStaleKeyEviction is a regression test for F7 (opportunistic
// global eviction). After the window elapses, AddFailure on a live key should
// trigger a sweep that removes all stale keys, leaving only the live one.
func TestAttemptLimiterStaleKeyEviction(t *testing.T) {
	t.Parallel()

	window := time.Hour
	past := time.Now().UTC().Add(-2 * window) // well outside the window
	live := time.Now().UTC()

	limiter := NewAttemptLimiter()

	// Force addCallsN to just below the sweep threshold so the next
	// AddFailure call crosses it and triggers the sweep.
	limiter.addCallsN = evictEveryN - 1

	// Populate evictEveryN-1 stale keys (failures recorded at 'past').
	// Use a sub-window so pruneLocked doesn't evict them on touch here
	// (they were added with window=time.Hour at time 'past', which is
	// before the threshold relative to 'live'; however we add them directly
	// to the map to avoid triggering the sweep counter prematurely).
	staleCount := evictEveryN - 1
	for i := 0; i < staleCount; i++ {
		key := fmt.Sprintf("stale-key-%d", i)
		limiter.attempts[key] = []time.Time{past}
	}

	// Add a live key via the normal path — this increments addCallsN to
	// evictEveryN and triggers the sweep.
	liveKey := "live-key"
	limiter.AddFailure(liveKey, live, window)

	// All stale keys should be gone.
	limiter.mu.Lock()
	mapLen := len(limiter.attempts)
	_, livePresent := limiter.attempts[liveKey]
	limiter.mu.Unlock()

	if mapLen != 1 {
		t.Fatalf("expected 1 key after stale eviction, got %d", mapLen)
	}
	if !livePresent {
		t.Fatal("expected live key to survive eviction sweep")
	}

	// Confirm the live key is still recognized as having a recent failure.
	if !limiter.TooManyRecent(liveKey, live, 1, window) {
		t.Fatal("live key should still register as having a recent failure after eviction sweep")
	}
}

func TestNormalizeLimiterKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "keeps valid key", raw: "127.0.0.1", want: "127.0.0.1"},
		{name: "trims surrounding spaces", raw: "  10.0.0.1  ", want: "10.0.0.1"},
		{name: "empty becomes unknown", raw: "", want: "unknown"},
		{name: "whitespace becomes unknown", raw: "   ", want: "unknown"},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeLimiterKey(testCase.raw); got != testCase.want {
				t.Fatalf("NormalizeLimiterKey(%q) = %q, want %q", testCase.raw, got, testCase.want)
			}
		})
	}
}

// TestAttemptLimiterSizeCapUnderFreshKeyFlood pins the hard memory bound: a
// stale sweep cannot shrink the map while an attacker keeps minting fresh
// keys inside the window, so after every sweep the map is trimmed back to
// evictAboveSize by evicting the keys with the oldest most-recent failure.
func TestAttemptLimiterSizeCapUnderFreshKeyFlood(t *testing.T) {
	limiter := NewAttemptLimiter()
	now := time.Now()
	window := 15 * time.Minute

	// Flood with fresh, distinct keys — far more than the cap, all inside
	// the window so the stale sweep removes none of them.
	total := evictAboveSize * 2
	for index := 0; index < total; index++ {
		limiter.AddFailure(fmt.Sprintf("identity:flood-%05d", index), now.Add(time.Duration(index)*time.Millisecond), window)
	}

	limiter.mu.Lock()
	size := len(limiter.attempts)
	limiter.mu.Unlock()
	if size > evictAboveSize {
		t.Fatalf("tracked keys = %d, want <= %d (hard cap must hold under a fresh-key flood)", size, evictAboveSize)
	}
}

// TestAttemptLimiterSizeCapEvictsColdestKeysFirst proves the trim removes
// the keys with the oldest most-recent failure, so an actively brute-forced
// key (the freshest) survives the eviction pass.
func TestAttemptLimiterSizeCapEvictsColdestKeysFirst(t *testing.T) {
	limiter := NewAttemptLimiter()
	base := time.Now()
	window := time.Hour

	// The victim key is the oldest entry but stays inside the window…
	limiter.AddFailure("identity:victim-hot", base, window)
	// …and is refreshed continuously while the flood runs, making it one of
	// the newest entries by last-failure time.
	for index := 0; index < evictAboveSize+200; index++ {
		limiter.AddFailure(fmt.Sprintf("identity:cold-%05d", index), base.Add(time.Duration(index)*time.Millisecond), window)
		if index%100 == 0 {
			limiter.AddFailure("identity:victim-hot", base.Add(time.Duration(index)*time.Millisecond+time.Second), window)
		}
	}

	limiter.mu.Lock()
	_, victimSurvives := limiter.attempts["identity:victim-hot"]
	_, coldestSurvives := limiter.attempts["identity:cold-00000"]
	size := len(limiter.attempts)
	limiter.mu.Unlock()

	if !victimSurvives {
		t.Fatal("actively refreshed key must survive the size-cap eviction")
	}
	if coldestSurvives {
		t.Fatal("coldest flooded key must be evicted first by the size-cap trim")
	}
	if size > evictAboveSize {
		t.Fatalf("tracked keys = %d, want <= %d", size, evictAboveSize)
	}
}
