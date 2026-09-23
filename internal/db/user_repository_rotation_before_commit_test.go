package db

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// The three recovery-code rotations run their caller's delivery hook inside
// the write's transaction, before it commits (WEB-58). A hook that fails must
// leave the users row exactly as it was — the old code, the old password, the
// old session version, the calendar feed and its reveal marks — and must not
// advance the calendar-feed fence for a revocation that never happened. A hook
// that succeeds must see the version the row now holds, re-read from it.

type recoveryRotationUnderTest struct {
	name string
	// advancesFence is whether the committed write clears the calendar feed and
	// therefore advances the restore fence.
	advancesFence bool
	rotate        func(repo *UserRepository, user models.User, beforeCommit func(int) error) error
}

func recoveryRotationsUnderTest() []recoveryRotationUnderTest {
	ctx := context.Background()
	return []recoveryRotationUnderTest{
		{
			name:          "regenerate",
			advancesFence: true,
			rotate: func(repo *UserRepository, user models.User, beforeCommit func(int) error) error {
				return repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, user.ID, "rotated-recovery", beforeCommit)
			},
		},
		{
			name: "local password setup",
			rotate: func(repo *UserRepository, user models.User, beforeCommit func(int) error) error {
				return repo.UpdatePasswordRecoveryCodeAndRevokeSessions(ctx, user.ID, "rotated-hash", "rotated-recovery", false, beforeCommit)
			},
		},
		{
			name:          "reset",
			advancesFence: true,
			rotate: func(repo *UserRepository, user models.User, beforeCommit func(int) error) error {
				return repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, user.ID, user.PasswordHash, user.AuthSessionVersion, "rotated-hash", "rotated-recovery", beforeCommit)
			},
		},
	}
}

func seedRotationUserForTest(t *testing.T, repo *UserRepository, email string) models.User {
	t.Helper()
	user := createUserForRevealMarkTest(t, repo, email)
	revealedAt := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	if err := repo.database.Model(&models.User{}).Where("id = ?", user.ID).Updates(map[string]any{
		"calendar_feed_selector":      "seeded-selector",
		"calendar_feed_verifier_hash": "seeded-verifier-hash",
		"calendar_feed_verifier_mac":  "seeded-verifier-mac",
		"recovery_code_revealed_at":   revealedAt,
		"auth_session_version":        3,
	}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return reloadUserForRevealMarkTest(t, repo, user.ID)
}

func TestRecoveryCodeRotationWritesNothingWhenItsDeliveryFails(t *testing.T) {
	t.Parallel()

	for _, rotation := range recoveryRotationsUnderTest() {
		t.Run(rotation.name, func(t *testing.T) {
			t.Parallel()

			repo := openRevealMarkRepoForTest(t)
			fence := &refusingCalendarFeedFence{}
			repo.calendarFeedFence = fence
			before := seedRotationUserForTest(t, repo, "rotation-rollback@example.com")

			refused := errors.New("delivery could not be sealed")
			hookCalls := 0
			err := rotation.rotate(repo, before, func(int) error {
				hookCalls++
				return refused
			})
			if !errors.Is(err, refused) {
				t.Fatalf("expected the delivery error back, got %v", err)
			}
			if hookCalls != 1 {
				t.Fatalf("expected the delivery hook to run once, got %d", hookCalls)
			}

			after := reloadUserForRevealMarkTest(t, repo, before.ID)
			if !reflect.DeepEqual(before, after) {
				t.Fatalf("a rotation whose delivery failed changed the row:\nbefore %+v\nafter  %+v", before, after)
			}
			if fence.calls != 0 {
				t.Fatalf("a rolled-back rotation advanced the calendar-feed fence %d time(s)", fence.calls)
			}
		})
	}
}

func TestRecoveryCodeRotationHandsItsHookTheStoredSessionVersion(t *testing.T) {
	t.Parallel()

	for _, rotation := range recoveryRotationsUnderTest() {
		t.Run(rotation.name, func(t *testing.T) {
			t.Parallel()

			repo := openRevealMarkRepoForTest(t)
			fence := &refusingCalendarFeedFence{}
			repo.calendarFeedFence = fence
			before := seedRotationUserForTest(t, repo, "rotation-commit@example.com")

			seen := 0
			if err := rotation.rotate(repo, before, func(sessionVersion int) error {
				seen = sessionVersion
				return nil
			}); err != nil {
				t.Fatalf("rotate: %v", err)
			}

			after := reloadUserForRevealMarkTest(t, repo, before.ID)
			if after.RecoveryCodeHash != "rotated-recovery" {
				t.Fatalf("expected the rotation to commit, recovery hash is %q", after.RecoveryCodeHash)
			}
			if seen != after.AuthSessionVersion || seen != before.AuthSessionVersion+1 {
				t.Fatalf("hook saw version %d, row holds %d (was %d)", seen, after.AuthSessionVersion, before.AuthSessionVersion)
			}
			wantFence := 0
			if rotation.advancesFence {
				wantFence = 1
			}
			if fence.calls != wantFence {
				t.Fatalf("expected %d fence advance(s) after the commit, got %d", wantFence, fence.calls)
			}
		})
	}
}

func TestRecoveryCodeRotationRefusesAZeroOwnerBeforeItsHook(t *testing.T) {
	t.Parallel()

	for _, rotation := range recoveryRotationsUnderTest() {
		t.Run(rotation.name, func(t *testing.T) {
			t.Parallel()

			repo := openRevealMarkRepoForTest(t)
			fence := &refusingCalendarFeedFence{}
			repo.calendarFeedFence = fence
			user := seedRotationUserForTest(t, repo, "rotation-zero@example.com")
			user.ID = 0

			hookCalls := 0
			err := rotation.rotate(repo, user, func(int) error {
				hookCalls++
				return nil
			})
			if !errors.Is(err, ErrUserOwnerRequired) {
				t.Fatalf("a rotation for owner id 0 must be refused as ErrUserOwnerRequired, got %v", err)
			}
			if hookCalls != 0 || fence.calls != 0 {
				t.Fatalf("a refused zero-owner rotation ran its hook %d time(s) and the fence %d time(s)", hookCalls, fence.calls)
			}
		})
	}
}
