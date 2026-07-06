package reminders

import (
	"testing"
	"time"
)

// TestDrainReturnsWhenDoneClosesFirst covers the normal drain: done closes
// before the budget, so Drain returns promptly without waiting the full budget.
func TestDrainReturnsWhenDoneClosesFirst(t *testing.T) {
	done := make(chan struct{})
	close(done)

	start := time.Now()
	Drain(done, time.Hour) // huge budget; must return immediately since done is closed
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Drain should return immediately when done is already closed, took %s", elapsed)
	}
}

// TestDrainTimesOutWhenDoneNeverCloses covers the timeout branch: done never
// closes, so Drain returns after the (small) budget and logs — proving shutdown
// proceeds even if a pass is stuck, keeping "DB closes on both exit paths" true.
func TestDrainTimesOutWhenDoneNeverCloses(t *testing.T) {
	done := make(chan struct{}) // never closed

	start := time.Now()
	Drain(done, 20*time.Millisecond)
	elapsed := time.Since(start)
	if elapsed < 20*time.Millisecond {
		t.Fatalf("Drain returned before its budget elapsed: %s", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Drain overran its budget substantially: %s", elapsed)
	}
}
