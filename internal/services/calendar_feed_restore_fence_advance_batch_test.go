package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

// commitMarker is the commit a services test hands an advance when it has no
// row of its own to write: it records the marker exactly as the writer's
// transaction would, through the fence's own app_state double.
func commitMarker(appState calendarFeedRestoreFenceAppState) CalendarFeedFenceCommit {
	return func(marker string) error {
		if marker == "" {
			return nil
		}
		return appState.Set(context.Background(), models.AppStateKeyCalendarFeedRestoreFence, marker)
	}
}

// advanceConfirmedForTest is the operator CLI's sequence with no row of its
// own: ConfirmAdvances at the gate, then one advance whose commit records the
// marker. Its errors are the ones the CLI classifies.
func advanceConfirmedForTest(fence *CalendarFeedRestoreFence, ctx context.Context) error {
	if err := fence.ConfirmAdvances(ctx); err != nil {
		return err
	}
	return fence.Advance(ctx, commitMarker(fence.appState))
}

// advanceForms are the three server-side advances, by name.
var advanceForms = map[string]func(*CalendarFeedRestoreFence, context.Context, CalendarFeedFenceCommit) error{
	"AdvanceOrRefuse":  (*CalendarFeedRestoreFence).AdvanceOrRefuse,
	"AdvanceOrDiverge": (*CalendarFeedRestoreFence).AdvanceOrDiverge,
	"Advance":          (*CalendarFeedRestoreFence).Advance,
}

// TestEveryAdvanceHandsItsWriterTheTokenTheFileAlreadyHolds is WEB-505's
// contract at the fence: the file half is written BEFORE commit runs, commit
// receives that same token to record beside its row, and the fence records no
// database half of its own. A fence that wrote the marker itself — in a commit
// apart from the writer's — is the window a live backup restores as
// continuity.
func TestEveryAdvanceHandsItsWriterTheTokenTheFileAlreadyHolds(t *testing.T) {
	for name, advance := range advanceForms {
		t.Run(name, func(t *testing.T) {
			appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
			anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
			fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

			commits := 0
			err := advance(fence, context.Background(), func(marker string) error {
				commits++
				if marker == "" || marker == fenceTestToken {
					t.Fatalf("commit must receive a fresh token, got %q", marker)
				}
				if anchor.value != marker {
					t.Fatalf("the file must hold the token before the writer's transaction runs: file %q, marker %q", anchor.value, marker)
				}
				if stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; stored != fenceTestToken {
					t.Fatalf("the fence must not record the database half itself, ahead of the writer's transaction: stored %q", stored)
				}
				return nil
			})
			if err != nil || commits != 1 {
				t.Fatalf("advance: err %v, commits %d", err, commits)
			}
			if appState.values[models.AppStateKeyCalendarFeedRestoreFence] != fenceTestToken {
				t.Fatal("a commit that recorded nothing must leave the database half where it was: only the writer's transaction records it")
			}
		})
	}
}

// TestAFailedCommitRecordsTheTokenAloneSoTheHalvesAgree: a writer whose
// transaction rolls back after the file moved must not leave the file ahead —
// the next boot would answer that with the full restore revocation, every
// session and local password, over a write that never happened. The token is
// recorded alone, the writer's own error comes back unwrapped, and only a
// failure of that record as well is reported as a half advance.
func TestAFailedCommitRecordsTheTokenAloneSoTheHalvesAgree(t *testing.T) {
	rolledBack := errors.New("compare-and-set lost")
	for name, advance := range advanceForms {
		t.Run(name, func(t *testing.T) {
			appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
			anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
			fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

			err := advance(fence, context.Background(), func(string) error { return rolledBack })
			if !errors.Is(err, rolledBack) || errors.Is(err, ErrCalendarFeedFenceHalfAdvanced) {
				t.Fatalf("the writer's own error must come back as itself, got %v", err)
			}
			if stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; stored != anchor.value || stored == fenceTestToken {
				t.Fatalf("after a rolled-back commit the halves must agree on the new token: file %q, database %q", anchor.value, stored)
			}

			appState.setErr = errors.New("database is locked")
			err = advance(fence, context.Background(), func(string) error { return rolledBack })
			if !errors.Is(err, rolledBack) || !errors.Is(err, ErrCalendarFeedFenceHalfAdvanced) {
				t.Fatalf("a rolled-back commit whose record also failed leaves the file ahead and must say so, got %v", err)
			}
		})
	}
}

// TestEveryConfirmedAdvanceAdvancesInsideItsOwnWrite replaces WEB-429's
// "covered" advance: the operator CLI confirms the fence once, and every write
// that follows advances it itself — file first, marker in the write's
// transaction — with nothing written at the gate. A file that cannot be
// written refuses with nothing written in either half and commit never run.
func TestEveryConfirmedAdvanceAdvancesInsideItsOwnWrite(t *testing.T) {
	for name, advance := range advanceForms {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
			anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
			fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

			if err := fence.ConfirmAdvances(ctx); err != nil {
				t.Fatalf("ConfirmAdvances: %v", err)
			}
			if anchor.value != fenceTestToken || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != fenceTestToken {
				t.Fatal("the gate must write neither half: the advance belongs inside the operator's write")
			}

			for round := range 2 {
				before := anchor.value
				if err := advance(fence, ctx, commitMarker(appState)); err != nil {
					t.Fatalf("round %d: %v", round, err)
				}
				if anchor.value == before || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != anchor.value {
					t.Fatalf("round %d: each confirmed write must advance both halves to one fresh token", round)
				}
			}

			anchor.writeErr = security.ErrCalendarFeedFenceNotConfigured
			ran := false
			err := advance(fence, ctx, func(string) error { ran = true; return nil })
			if !errors.Is(err, ErrCalendarFeedFenceUnreachable) || ran {
				t.Fatalf("a confirmed fence must refuse an unwritable file without running the write: err %v, ran %v", err, ran)
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
	defer anchor.mu.Unlock()
	anchor.value = value
	return nil
}

// TestConcurrentAdvanceOrRefuseCallsEachTakeTheirOwnToken: WEB-454 coalesced
// concurrent sign-outs into one advance, whose members then wrote their rows
// after the shared token was already in the database — the backup window
// WEB-505 closes. Every caller now writes its own token and runs its own
// commit while the file still holds exactly that token.
func TestConcurrentAdvanceOrRefuseCallsEachTakeTheirOwnToken(t *testing.T) {
	ctx := context.Background()
	appState := &lockedFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
	anchor := &gatedFenceAnchor{value: fenceTestToken, entered: make(chan struct{}), release: make(chan struct{})}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	const callers = 5
	var markers sync.Map
	commit := func(marker string) error {
		if file, _, _ := anchor.Read(); file != marker {
			t.Errorf("a commit ran while the file held another caller's token: file %q, marker %q", file, marker)
		}
		markers.Store(marker, true)
		return commitMarker(appState)(marker)
	}
	errs := make(chan error, callers)
	go func() { errs <- fence.AdvanceOrRefuse(ctx, commit) }()
	<-anchor.entered
	for i := 1; i < callers; i++ {
		go func() { errs <- fence.AdvanceOrRefuse(ctx, commit) }()
	}
	time.Sleep(50 * time.Millisecond)
	close(anchor.release)
	for range callers {
		if err := <-errs; err != nil {
			t.Fatalf("AdvanceOrRefuse: %v", err)
		}
	}
	distinct := 0
	markers.Range(func(any, any) bool { distinct++; return true })
	if distinct != callers || anchor.writes != callers {
		t.Fatalf("each caller must take its own token: %d distinct markers, %d file writes, %d callers", distinct, anchor.writes, callers)
	}
	if file, _, _ := anchor.Read(); appState.get(models.AppStateKeyCalendarFeedRestoreFence) != file {
		t.Fatal("the halves must agree on the last token")
	}
}

// panickingFenceAnchor panics on its first Write and writes normally after.
type panickingFenceAnchor struct {
	stubFenceAnchor
	panicked bool
}

func (anchor *panickingFenceAnchor) Write(value string) error {
	if !anchor.panicked {
		anchor.panicked = true
		panic("fence volume driver fault")
	}
	return anchor.stubFenceAnchor.Write(value)
}

// TestAPanickingAdvanceReleasesTheFence: a panic mid-advance, recovered by the
// request's own handler, must leave the fence usable. Holding writing across
// it would block every later sign-out, password change and feed write on the
// instance for good.
func TestAPanickingAdvanceReleasesTheFence(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
	anchor := &panickingFenceAnchor{stubFenceAnchor: stubFenceAnchor{value: fenceTestToken, found: true}}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	func() {
		defer func() { _ = recover() }()
		_ = fence.AdvanceOrRefuse(context.Background(), commitMarker(appState))
	}()

	done := make(chan error, 1)
	go func() { done <- fence.AdvanceOrRefuse(context.Background(), commitMarker(appState)) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the advance after a recovered panic: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the fence stayed locked after a panicking advance")
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
