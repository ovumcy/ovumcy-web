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
	if anchor.written != "" || appState.values[models.AppStateKeyCalendarFeedRestoreFence] != "an-older-generation" {
		t.Fatalf("a failed revocation must record no new token, so the next boot re-runs it; file %q, app_state %q", anchor.written, appState.values[models.AppStateKeyCalendarFeedRestoreFence])
	}
}
