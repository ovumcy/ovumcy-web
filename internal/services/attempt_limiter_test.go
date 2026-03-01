package services

import (
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
