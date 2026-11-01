package services

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// WEB-399: a boot that sees a restore revokes every session and clears every
// webhook endpoint beside the feed disarm, before the new token is recorded;
// every other outcome revokes nothing.

func TestCalendarFeedRestoreFenceRevokesSessionsAndWebhooksOnEveryBrokenContinuity(t *testing.T) {
	revoked := models.RestoreRevocationCounts{Sessions: 4, WebhookEndpoints: 2}
	cases := map[string]func() (*stubFenceAppState, *stubFenceAnchor){
		"the halves disagree": func() (*stubFenceAppState, *stubFenceAnchor) {
			return &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation"}},
				&stubFenceAnchor{value: fenceTestToken, found: true}
		},
		"first fenced boot over an unanchored history": func() (*stubFenceAppState, *stubFenceAnchor) {
			return &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedFenceUnanchored: "stamp"}},
				&stubFenceAnchor{}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			appState, anchor := setup()
			users := &stubFenceUserStore{disarmed: 1, revoked: revoked}
			outcome, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background())
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if !outcome.ContinuityBroken || users.revokeCalls != 1 || !users.revokeAfterDisarm {
				t.Fatalf("expected one revocation after the disarm on broken continuity, got calls=%d afterDisarm=%v outcome=%+v", users.revokeCalls, users.revokeAfterDisarm, outcome)
			}
			if outcome.Revoked != revoked {
				t.Fatalf("the outcome must report what was revoked, got %+v", outcome.Revoked)
			}
		})
	}
}

func TestCalendarFeedRestoreFenceRevokesNothingWithoutARestore(t *testing.T) {
	cases := map[string]func() (*stubFenceAppState, *stubFenceAnchor){
		"first boot": func() (*stubFenceAppState, *stubFenceAnchor) {
			return &stubFenceAppState{values: map[string]string{}}, &stubFenceAnchor{}
		},
		"the halves agree": func() (*stubFenceAppState, *stubFenceAnchor) {
			return &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: fenceTestToken}},
				&stubFenceAnchor{value: fenceTestToken, found: true}
		},
		"no usable fence": func() (*stubFenceAppState, *stubFenceAnchor) {
			return &stubFenceAppState{values: map[string]string{}}, &stubFenceAnchor{readErr: errors.New("CALENDAR_FEED_FENCE_PATH is not set")}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			appState, anchor := setup()
			users := &stubFenceUserStore{}
			outcome, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background())
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if users.revokeCalls != 0 || outcome.Revoked != (models.RestoreRevocationCounts{}) {
				t.Fatalf("a boot without a restore must revoke nothing, got calls=%d outcome=%+v", users.revokeCalls, outcome)
			}
		})
	}
}

func TestCalendarFeedRestoreFenceRecordsNothingWhenTheRevocationFails(t *testing.T) {
	appState := &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation"}}
	anchor := &stubFenceAnchor{value: fenceTestToken, found: true}
	injected := errors.New("revocation failed")
	users := &stubFenceUserStore{revokeErr: injected}

	if _, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background()); !errors.Is(err, injected) {
		t.Fatalf("a failed revocation must fail the boot, got %v", err)
	}
	// The file half moves first (WEB-408); the database half must not, so the
	// halves disagree and the next boot re-runs the revocation.
	if stored := appState.values[models.AppStateKeyCalendarFeedRestoreFence]; stored != "an-older-generation" || stored == anchor.value {
		t.Fatalf("a failed revocation must leave the halves disagreeing, so the next boot re-runs it; file %q, app_state %q", anchor.value, stored)
	}
}

// TestCalendarFeedRestoreFenceRevokesNothingWhenTheFenceCannotRecord pins the
// WEB-408 ordering: an anchor that reads but cannot be written degrades to the
// unanchored outcome BEFORE any session, endpoint or password is revoked. The
// revocation used to run first, and a configured-but-missing fence volume
// re-enters this pass on every start, so every start revoked everything again —
// with local passwords in the revocation, every owner locked out on every start.
func TestCalendarFeedRestoreFenceRevokesNothingWhenTheFenceCannotRecord(t *testing.T) {
	cases := map[string]func() (*stubFenceAppState, *stubFenceAnchor){
		"the halves disagree": func() (*stubFenceAppState, *stubFenceAnchor) {
			return &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedRestoreFence: "an-older-generation"}},
				&stubFenceAnchor{value: fenceTestToken, found: true, writeErr: errors.New("read-only file system")}
		},
		"first fenced boot over an unanchored history": func() (*stubFenceAppState, *stubFenceAnchor) {
			return &stubFenceAppState{values: map[string]string{models.AppStateKeyCalendarFeedFenceUnanchored: "stamp"}},
				&stubFenceAnchor{writeErr: errors.New("no such file or directory")}
		},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			appState, anchor := setup()
			users := &stubFenceUserStore{disarmed: 1, revoked: models.RestoreRevocationCounts{Sessions: 4, LocalPasswords: 3}}
			outcome, err := NewCalendarFeedRestoreFence(appState, users, anchor).Enforce(context.Background())
			if err != nil {
				t.Fatalf("Enforce: %v", err)
			}
			if !outcome.Unanchored || outcome.ContinuityBroken {
				t.Fatalf("an unwritable fence must end unanchored, got %+v", outcome)
			}
			if users.revokeCalls != 0 || outcome.Revoked != (models.RestoreRevocationCounts{}) {
				t.Fatalf("an unwritable fence must revoke nothing, got calls=%d outcome=%+v", users.revokeCalls, outcome)
			}
			if outcome.DisarmedFeeds != 1 {
				t.Fatalf("an unanchored boot still disarms every feed, got %d", outcome.DisarmedFeeds)
			}
		})
	}
}
