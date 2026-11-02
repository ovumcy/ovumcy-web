package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// flakyFenceAnchor fails exactly the write numbered failOn (1-based) and
// passes every other, so a test can break one step of a multi-write pass.
type flakyFenceAnchor struct {
	stubFenceAnchor
	failOn int
	writes int
}

func (s *flakyFenceAnchor) Write(value string) error {
	s.writes++
	if s.writes == s.failOn {
		return errors.New("disk full")
	}
	return s.stubFenceAnchor.Write(value)
}

// flakyFenceAppState fails the first failSets Set calls and passes the rest.
type flakyFenceAppState struct {
	stubFenceAppState
	failSets int
}

func (s *flakyFenceAppState) Set(ctx context.Context, key string, value string) error {
	if s.failSets > 0 {
		s.failSets--
		return errors.New("database is locked")
	}
	return s.stubFenceAppState.Set(ctx, key, value)
}

func assertNothingRevoked(t *testing.T, outcome CalendarFeedRestoreFenceOutcome, users *stubFenceUserStore) {
	t.Helper()
	if !outcome.FirstBoot || outcome.ContinuityBroken {
		t.Fatalf("expected the retried first boot, got %+v", outcome)
	}
	if users.callCount != 0 || users.revokeCalls != 0 || outcome.Revoked.LocalSignIns != 0 {
		t.Fatalf("a first boot that never served must revoke nothing (disarm=%d, revoke=%d, outcome=%+v)", users.callCount, users.revokeCalls, outcome)
	}
}

func assertHalvesAgree(t *testing.T, appState *stubFenceAppState, anchor *stubFenceAnchor) {
	t.Helper()
	stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]
	if !anchor.found || anchor.value == "" || anchor.value != stored {
		t.Fatalf("the halves must agree after a completed first boot: file %q, app_state %q", anchor.value, stored)
	}
}

// TestCalendarFeedRestoreFenceFirstBootDatabaseFailureIsRetriedNotRevoked pins
// WEB-456: a first boot that wrote the file and then failed to store the
// database half served nothing, so the next boot must retry the first boot
// instead of reading the lone file token as a restore and revoking every
// session, webhook and local password.
func TestCalendarFeedRestoreFenceFirstBootDatabaseFailureIsRetriedNotRevoked(t *testing.T) {
	for _, required := range []bool{false, true} {
		appState := &flakyFenceAppState{stubFenceAppState: stubFenceAppState{values: map[string]string{}}, failSets: 1}
		users := &stubFenceUserStore{disarmed: 4, revoked: models.RestoreRevocationCounts{LocalSignIns: 9}}
		anchor := &flakyFenceAnchor{}
		fence := NewCalendarFeedRestoreFence(appState, users, anchor)
		enforce := fence.Enforce
		if required {
			enforce = fence.EnforceRequired
		}

		if _, err := enforce(context.Background()); err == nil {
			t.Fatalf("required=%t: the failed database half must fail the boot", required)
		}
		outcome, err := enforce(context.Background())
		if err != nil {
			t.Fatalf("required=%t: retried boot: %v", required, err)
		}
		assertNothingRevoked(t, outcome, users)
		assertHalvesAgree(t, &appState.stubFenceAppState, &anchor.stubFenceAnchor)
	}
}

// TestCalendarFeedRestoreFenceFirstBootFinalFileFailureIsCompletedNotRevoked
// covers the last step of the first boot: both halves hold the token but the
// file still carries it as pending. Under EnforceRequired the boot is refused
// and the next one finishes the pair without revoking anything.
func TestCalendarFeedRestoreFenceFirstBootFinalFileFailureIsCompletedNotRevoked(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{}}
	users := &stubFenceUserStore{revoked: models.RestoreRevocationCounts{LocalSignIns: 9}}
	anchor := &flakyFenceAnchor{failOn: 2}
	fence := NewCalendarFeedRestoreFence(appState, users, anchor)

	refused, err := fence.EnforceRequired(context.Background())
	if err != nil || !refused.Unanchored {
		t.Fatalf("a failed final file write must refuse the boot as unanchored, got %+v, %v", refused, err)
	}
	outcome, err := fence.EnforceRequired(context.Background())
	if err != nil {
		t.Fatalf("completing boot: %v", err)
	}
	assertNothingRevoked(t, outcome, users)
	assertHalvesAgree(t, appState, &anchor.stubFenceAnchor)
}

// TestCalendarFeedRestoreFencePendingFirstBootStillContainsARestore keeps the
// containment side of WEB-456: a pending file token only stands for "no file
// token yet", so a database that disagrees with it, or that carries the
// unanchored stamp, still takes the full ContinuityBroken pass, and a first
// boot that completed is no longer pending, so restoring a pre-fence backup
// under it is still a restore.
func TestCalendarFeedRestoreFencePendingFirstBootStillContainsARestore(t *testing.T) {
	pending := calendarFeedFenceFirstBootPrefix + fenceTestToken
	testCases := []struct {
		name              string
		anchor            *stubFenceAnchor
		values            map[string]string
		unanchoredHistory bool
	}{
		{name: "pending file, other database token", anchor: &stubFenceAnchor{value: pending, found: true}, values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "row"}},
		{name: "pending file, stamped empty database", anchor: &stubFenceAnchor{value: pending, found: true}, values: map[string]string{models.AppStateKeyCalendarFeedFenceUnanchored: "stamp"}, unanchoredHistory: true},
		{name: "completed file, pre-fence backup", anchor: &stubFenceAnchor{value: fenceTestToken, found: true}, values: map[string]string{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			appState := &stubFenceAppState{values: testCase.values}
			users := &stubFenceUserStore{revoked: models.RestoreRevocationCounts{LocalSignIns: 9}}

			outcome, err := NewCalendarFeedRestoreFence(appState, users, testCase.anchor).EnforceRequired(context.Background())
			if err != nil {
				t.Fatalf("EnforceRequired: %v", err)
			}
			if !outcome.ContinuityBroken || outcome.UnanchoredHistory != testCase.unanchoredHistory || users.revokeCalls != 1 {
				t.Fatalf("expected the full revocation pass, got %+v (revoke=%d)", outcome, users.revokeCalls)
			}
			assertHalvesAgree(t, appState, testCase.anchor)
		})
	}
}

// TestCalendarFeedRestoreFencePendingFileRefusesOperatorAdvance keeps the
// operator CLI out of a first boot the server has not finished: a pending file
// is not a known-agreeing pair even when the database holds its token.
func TestCalendarFeedRestoreFencePendingFileRefusesOperatorAdvance(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}}
	anchor := &stubFenceAnchor{value: calendarFeedFenceFirstBootPrefix + fenceTestToken, found: true}

	err := NewCalendarFeedRestoreFence(appState, &stubFenceUserStore{}, anchor).AdvanceConfirmed(context.Background())
	var continuity *CalendarFeedFenceContinuityError
	if !errors.As(err, &continuity) || continuity.AnchorFound {
		t.Fatalf("expected a continuity refusal reporting no file token, got %v", err)
	}
	if anchor.written != "" {
		t.Fatalf("a refusal writes nothing, file got %q", anchor.written)
	}
}
