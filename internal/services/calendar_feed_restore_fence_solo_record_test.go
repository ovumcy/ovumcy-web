package services

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// soloRecordObservingAppState answers the one Set a failed commit leaves —
// the marker recorded alone — with what its context looked like, and then
// fails it the way a record whose context ended fails.
type soloRecordObservingAppState struct {
	*stubFenceAppState
	sets        int
	hadDeadline bool
	remaining   time.Duration
	ctxErr      error
}

func (s *soloRecordObservingAppState) Set(ctx context.Context, _ string, _ string) error {
	s.sets++
	deadline, ok := ctx.Deadline()
	s.hadDeadline = ok
	if ok {
		s.remaining = time.Until(deadline)
	}
	s.ctxErr = ctx.Err()
	return fmt.Errorf("record the fence marker: %w", context.DeadlineExceeded)
}

// TestTheSoloRecordAfterAFailedCommitIsBoundedAndFailsClosed: the record a
// rolled-back commit leaves runs on the caller's context — a service never
// detaches from its caller — under a deadline of its own, since the caller
// holds fence.writing. A record that fails, a cancelled caller included,
// leaves the file ahead, and the result must say so.
func TestTheSoloRecordAfterAFailedCommitIsBoundedAndFailsClosed(t *testing.T) {
	rolledBack := errors.New("request cancelled mid-transaction")
	for name, advance := range advanceForms {
		t.Run(name, func(t *testing.T) {
			appState := &soloRecordObservingAppState{stubFenceAppState: &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}}
			anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
			fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			err := advance(fence, ctx, func(string) error { cancel(); return rolledBack })

			if appState.sets != 1 {
				t.Fatalf("a failed commit must record the marker alone exactly once, saw %d records", appState.sets)
			}
			if !errors.Is(appState.ctxErr, context.Canceled) {
				t.Fatalf("the solo record must inherit the caller's cancellation, got %v", appState.ctxErr)
			}
			if !appState.hadDeadline || appState.remaining > calendarFeedFenceSoloRecordTimeout {
				t.Fatalf("the solo record must run under a deadline of at most %v, had deadline %v with %v left", calendarFeedFenceSoloRecordTimeout, appState.hadDeadline, appState.remaining)
			}
			if !errors.Is(err, rolledBack) || !errors.Is(err, ErrCalendarFeedFenceHalfAdvanced) {
				t.Fatalf("a solo record that failed leaves the file ahead and must be reported as a half advance, got %v", err)
			}
		})
	}
}
