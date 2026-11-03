package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// countdownFenceAnchor fails its next failures Writes, then writes normally.
type countdownFenceAnchor struct {
	stubFenceAnchor
	failures int
	attempts int
}

func (anchor *countdownFenceAnchor) Write(value string) error {
	anchor.attempts++
	if anchor.failures > 0 {
		anchor.failures--
		return errors.New("no space left on device")
	}
	anchor.value = value
	anchor.found = true
	return nil
}

func agreeingFenceForRetryTest(failures int) (*CalendarFeedRestoreFence, *stubFenceAppState, *countdownFenceAnchor) {
	appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
	anchor := &countdownFenceAnchor{stubFenceAnchor: stubFenceAnchor{value: fenceTestToken, found: true}, failures: failures}
	return NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor), appState, anchor
}

// TestAdvanceOrRefuseRetriesATransientFileFailure is WEB-464's transient arm:
// a fence file that fails a few writes and then takes one costs the sign-out
// nothing, and both halves end on the same fresh token.
func TestAdvanceOrRefuseRetriesATransientFileFailure(t *testing.T) {
	fence, appState, anchor := agreeingFenceForRetryTest(len(calendarFeedFenceWriteRetryDelays))

	if err := fence.AdvanceOrRefuse(context.Background()); err != nil {
		t.Fatalf("a transient fence-file failure must be retried away, got %v", err)
	}
	if anchor.attempts != len(calendarFeedFenceWriteRetryDelays)+1 {
		t.Fatalf("expected %d attempts, got %d", len(calendarFeedFenceWriteRetryDelays)+1, anchor.attempts)
	}
	if stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; stored != anchor.value || stored == fenceTestToken {
		t.Fatalf("the halves must agree on a fresh token: file %q, database %q", anchor.value, stored)
	}
}

// TestAdvanceOrRefuseRefusesAPersistentFileFailure is WEB-464's persistent
// arm: when every attempt fails, the advance refuses as Advance does, with
// neither half written. Moving the database half alone would leave the file on
// the token a pre-write backup also carries, and restoring that backup before
// the next boot would read as continuity.
func TestAdvanceOrRefuseRefusesAPersistentFileFailure(t *testing.T) {
	fence, appState, anchor := agreeingFenceForRetryTest(len(calendarFeedFenceWriteRetryDelays) + 1)

	err := fence.AdvanceOrRefuse(context.Background())
	if !errors.Is(err, ErrCalendarFeedFenceWriteFailed) {
		t.Fatalf("a persistent fence-file failure must refuse, got %v", err)
	}
	if anchor.attempts != len(calendarFeedFenceWriteRetryDelays)+1 {
		t.Fatalf("expected %d attempts before refusing, got %d", len(calendarFeedFenceWriteRetryDelays)+1, anchor.attempts)
	}
	if anchor.value != fenceTestToken || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != fenceTestToken {
		t.Fatalf("a refused advance must write neither half: file %q, database %q", anchor.value, appState.values[models.AppStateKeyCalendarFeedRestoreFence])
	}
}

// TestAdvanceOrRefuseStopsRetryingOnADoneContext pins the retry's bound by the
// caller: a request that is gone does not hold writing through the backoff.
func TestAdvanceOrRefuseStopsRetryingOnADoneContext(t *testing.T) {
	fence, appState, anchor := agreeingFenceForRetryTest(len(calendarFeedFenceWriteRetryDelays) + 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := fence.AdvanceOrRefuse(ctx); !errors.Is(err, ErrCalendarFeedFenceWriteFailed) {
		t.Fatalf("a cancelled advance over a failing file must refuse, got %v", err)
	}
	if anchor.attempts != 1 || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != fenceTestToken {
		t.Fatalf("a cancelled advance must stop after one attempt and write nothing, got %d attempt(s)", anchor.attempts)
	}
}

// TestAdvanceOrRefuseOverDisagreeingHalvesMovesTheDatabaseHalf pins the one
// state where the refusal has no continuity to keep, as Advance does: halves
// that already disagree send the next boot down the full pass whatever this
// write records, so a sign-out there proceeds.
func TestAdvanceOrRefuseOverDisagreeingHalvesMovesTheDatabaseHalf(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "older-token"}}
	anchor := &countdownFenceAnchor{stubFenceAnchor: stubFenceAnchor{value: fenceTestToken, found: true}, failures: len(calendarFeedFenceWriteRetryDelays) + 1}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	if err := fence.AdvanceOrRefuse(context.Background()); err != nil {
		t.Fatalf("over halves that already disagree the advance must proceed, got %v", err)
	}
	if stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; stored == "older-token" || stored == anchor.value {
		t.Fatalf("the database half must move alone, got database %q file %q", stored, anchor.value)
	}
}

// TestAdvanceOrRefuseRefusesOverAFenceFileThatReadsAbsent pins that a missing
// file is not a disagreement that waives the refusal: an unmounted fence
// volume reads as absent and fails the write, and once it is mounted again its
// old token agrees with a restored pre-write backup.
func TestAdvanceOrRefuseRefusesOverAFenceFileThatReadsAbsent(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
	anchor := &countdownFenceAnchor{failures: len(calendarFeedFenceWriteRetryDelays) + 1}
	fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

	if err := fence.AdvanceOrRefuse(context.Background()); !errors.Is(err, ErrCalendarFeedFenceWriteFailed) {
		t.Fatalf("an absent, unwritable fence file must refuse, got %v", err)
	}
	if stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; stored != fenceTestToken {
		t.Fatalf("a refused advance must leave the database half alone, got %q", stored)
	}
}

// TestAdvanceOrRefuseOverAnUnreadableFenceFileRefusesOnlyWhileATokenRemains
// pins the read-failure arm: a fence that cannot be read is refused while the
// database still holds a token a restore could bring back to agreement, and
// proceeds once an unanchored opt-out boot has dropped it.
func TestAdvanceOrRefuseOverAnUnreadableFenceFileRefusesOnlyWhileATokenRemains(t *testing.T) {
	broken := errors.New("permission denied")
	cases := map[string]struct {
		stored  map[string]string
		refuses bool
	}{
		"token held":    {stored: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}, refuses: true},
		"token dropped": {stored: map[string]string{}, refuses: false},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			appState := &stubFenceAppState{values: testCase.stored}
			anchor := &stubFenceAnchor{readErr: broken, writeErr: broken}
			fence := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor)

			err := fence.AdvanceOrRefuse(context.Background())
			if refused := errors.Is(err, ErrCalendarFeedFenceWriteFailed); refused != testCase.refuses {
				t.Fatalf("refused = %t, want %t (err %v)", refused, testCase.refuses, err)
			}
		})
	}
}

// TestAdvanceOrRefuseTypesADatabaseFailureAfterTheFile pins that the one
// refusal after which something changed is told apart from a caller's own
// write error.
func TestAdvanceOrRefuseTypesADatabaseFailureAfterTheFile(t *testing.T) {
	fence, appState, _ := agreeingFenceForRetryTest(0)
	appState.setErr = errors.New("database is locked")

	if err := fence.AdvanceOrRefuse(context.Background()); !errors.Is(err, ErrCalendarFeedFenceHalfAdvanced) {
		t.Fatalf("a database failure after the file moved must wrap ErrCalendarFeedFenceHalfAdvanced, got %v", err)
	}
}

// TestAdvanceOrDivergeRetriesBeforeItDiverges pins that an erasure pays the
// divergence only for a failure that outlasts the retries.
func TestAdvanceOrDivergeRetriesBeforeItDiverges(t *testing.T) {
	fence, appState, anchor := agreeingFenceForRetryTest(1)

	if err := fence.AdvanceOrDiverge(context.Background()); err != nil {
		t.Fatalf("AdvanceOrDiverge: %v", err)
	}
	if stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; stored != anchor.value || stored == fenceTestToken {
		t.Fatalf("a transient failure must end with the halves agreeing: file %q, database %q", anchor.value, stored)
	}
}
