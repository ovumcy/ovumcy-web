package services

import (
	"context"
	"errors"
	"maps"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestCalendarFeedRestoreFenceEnforceRequiredWritesNothingOnAnAnchorFailure
// pins WEB-427 on every path that reaches the unanchored outcome: a boot that
// will be refused for it disarms nothing, revokes nothing and leaves both
// app_state keys exactly as it found them, so the boot after the operator
// fixes the mount sees the same database the refused one did.
func TestCalendarFeedRestoreFenceEnforceRequiredWritesNothingOnAnAnchorFailure(t *testing.T) {
	broken := errors.New("permission denied")
	testCases := []struct {
		name   string
		anchor *stubFenceAnchor
		values map[string]string
	}{
		{name: "unreadable anchor", anchor: &stubFenceAnchor{readErr: broken}, values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "token"}},
		{name: "first boot, unwritable anchor", anchor: &stubFenceAnchor{writeErr: broken}, values: map[string]string{}},
		{name: "unanchored history, unwritable anchor", anchor: &stubFenceAnchor{writeErr: broken}, values: map[string]string{models.AppStateKeyCalendarFeedFenceUnanchored: "stamp"}},
		{name: "agreeing halves, unwritable anchor", anchor: &stubFenceAnchor{value: "token", found: true, writeErr: broken}, values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "token"}},
		{name: "disagreeing halves, unwritable anchor", anchor: &stubFenceAnchor{value: "file", found: true, writeErr: broken}, values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "row"}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			appState := &stubFenceAppState{values: maps.Clone(testCase.values)}
			users := &stubFenceUserStore{disarmed: 3}

			outcome, err := NewCalendarFeedRestoreFence(appState, users, testCase.anchor).EnforceRequired(context.Background())
			if err != nil {
				t.Fatalf("an anchor failure is an outcome, not an error: %v", err)
			}
			if !outcome.Unanchored || !errors.Is(outcome.UnanchoredCause, broken) || outcome.DisarmedFeeds != 0 {
				t.Fatalf("expected an unanchored outcome with nothing disarmed, got %+v", outcome)
			}
			if users.callCount != 0 || users.revokeCalls != 0 {
				t.Fatalf("a refused boot must not disarm or revoke (disarm=%d, revoke=%d)", users.callCount, users.revokeCalls)
			}
			if !maps.Equal(appState.values, testCase.values) {
				t.Fatalf("a refused boot must leave app_state as it found it: before %v, after %v", testCase.values, appState.values)
			}
		})
	}
}
