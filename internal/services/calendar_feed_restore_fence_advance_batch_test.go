package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestAConfirmedAdvanceCoversTheOperatorWriteThatFollowsIt is WEB-429: the
// operator CLI confirms and advances the fence, and the repository write that
// follows advances the SAME fence again. A file error on that second advance
// used to move the database half alone, so the next boot revoked every
// session, webhook and local password, while the CLI reported both halves in
// agreement. The confirmed advance now covers exactly one following advance,
// of either form, and the one after it writes again.
func TestAConfirmedAdvanceCoversTheOperatorWriteThatFollowsIt(t *testing.T) {
	forms := map[string]func(*CalendarFeedRestoreFence, context.Context) error{
		"AdvanceOrDiverge": (*CalendarFeedRestoreFence).AdvanceOrDiverge,
		"Advance":          (*CalendarFeedRestoreFence).Advance,
	}
	for name, advance := range forms {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
			anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
			fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

			if err := fence.AdvanceConfirmed(ctx); err != nil {
				t.Fatalf("AdvanceConfirmed: %v", err)
			}
			confirmed := anchor.value
			if confirmed == fenceTestToken || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != confirmed {
				t.Fatalf("fixture: the confirmed advance should leave both halves on a fresh token")
			}

			anchor.writeErr = errors.New("no space left on device")
			if err := advance(fence, ctx); err != nil {
				t.Fatalf("the write the confirmed advance covers must not fail on the fence: %v", err)
			}
			if anchor.value != confirmed || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != confirmed {
				t.Fatalf("a covered advance must write neither half: file %q, database %q, confirmed %q",
					anchor.value, appState.values[models.AppStateKeyCalendarFeedRestoreFence], confirmed)
			}

			anchor.writeErr = nil
			if err := advance(fence, ctx); err != nil {
				t.Fatalf("the next advance: %v", err)
			}
			if anchor.value == confirmed {
				t.Fatal("the confirmed advance must cover one write only; the next advance wrote nothing")
			}
		})
	}
}

// gatedFenceAnchor blocks its first Write until released, so a test can hold
// fence.writing while more callers arrive.
type gatedFenceAnchor struct {
	mu      sync.Mutex
	value   string
	writes  int
	entered chan struct{}
	release chan struct{}
}

func (anchor *gatedFenceAnchor) Read() (string, bool, error) {
	anchor.mu.Lock()
	defer anchor.mu.Unlock()
	return anchor.value, anchor.value != "", nil
}

func (anchor *gatedFenceAnchor) Write(value string) error {
	anchor.mu.Lock()
	anchor.writes++
	first := anchor.writes == 1
	anchor.mu.Unlock()
	if first {
		close(anchor.entered)
		<-anchor.release
	}
	anchor.mu.Lock()
	anchor.value = value
	anchor.mu.Unlock()
	return nil
}

// TestConcurrentAdvanceOrDivergeCallsShareOneAdvance is WEB-454's sign-out
// cost: every sign-out advances the fence under one instance-wide lock, each
// with a synced file write. Callers that arrive while an advance is in flight
// now share the next one, so N of them cost two writes rather than N+1, and
// the halves still agree on the last token.
func TestConcurrentAdvanceOrDivergeCallsShareOneAdvance(t *testing.T) {
	const waiting = 8
	ctx := context.Background()
	appState := &lockedFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
	anchor := &gatedFenceAnchor{value: fenceTestToken, entered: make(chan struct{}), release: make(chan struct{})}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	var calls sync.WaitGroup
	errs := make(chan error, waiting+1)
	calls.Add(1)
	go func() {
		defer calls.Done()
		errs <- fence.AdvanceOrDiverge(ctx)
	}()
	<-anchor.entered

	for range waiting {
		calls.Add(1)
		go func() {
			defer calls.Done()
			errs <- fence.AdvanceOrDiverge(ctx)
		}()
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		fence.batching.Lock()
		joined := 0
		if fence.pending != nil {
			joined = fence.pending.members
		}
		fence.batching.Unlock()
		if joined == waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("only %d of %d callers joined the waiting batch", joined, waiting)
		}
		time.Sleep(time.Millisecond)
	}
	close(anchor.release)
	calls.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("AdvanceOrDiverge: %v", err)
		}
	}

	if anchor.writes != 2 {
		t.Fatalf("expected the in-flight advance and one shared advance, got %d file writes for %d callers", anchor.writes, waiting+1)
	}
	if stored := appState.get(models.AppStateKeyCalendarFeedRestoreFence); stored != anchor.value || stored == fenceTestToken {
		t.Fatalf("the halves must agree on a fresh token: file %q, database %q", anchor.value, stored)
	}
}

// lockedFenceAppState is stubFenceAppState for a test whose callers run on
// several goroutines.
type lockedFenceAppState struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *lockedFenceAppState) Get(_ context.Context, key string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	return value, ok, nil
}

func (s *lockedFenceAppState) Set(_ context.Context, key string, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = value
	return nil
}

func (s *lockedFenceAppState) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	return nil
}

func (s *lockedFenceAppState) get(key string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[key]
}
