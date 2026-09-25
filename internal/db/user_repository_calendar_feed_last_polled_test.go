package db

import (
	"context"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// calendar_feed_last_polled_on (migration 040, WEB-46) records, at most once
// per owner calendar day, that a successful token-verified poll served the
// feed. Everything here defends the properties that make it worth rendering:
// it moves only forward, only under the token it was minted for, and it never
// outlives the token it was about. Cited by SECURITY.md's Calendar Feed
// Subscription rows.

func polledOnOf(t *testing.T, repo *UserRepository, userID uint) *time.Time {
	t.Helper()
	user, err := repo.FindByID(context.Background(), userID)
	if err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return user.CalendarFeedLastPolledOn
}

func seedArmedFeedForPolledTest(t *testing.T, repo *UserRepository, email string) (userID uint, selector string) {
	t.Helper()
	user := createUserForTimezoneTest(t, repo, email)
	selector = "sel-" + email
	if err := repo.database.WithContext(context.Background()).
		Model(&models.User{}).
		Where("id = ?", user.ID).
		Updates(map[string]any{"calendar_feed_selector": selector}).Error; err != nil {
		t.Fatalf("seed armed feed selector: %v", err)
	}
	return user.ID, selector
}

// TestMarkCalendarFeedPolledWritesOnceThenNoOpsTheSameDay pins the write-once
// shape the design constraint asks for: a repeated poll on the same owner-day
// must not cost a second write's worth of change (RowsAffected==0 on retry),
// and the stored date must not move.
func TestMarkCalendarFeedPolledWritesOnceThenNoOpsTheSameDay(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	ctx := context.Background()
	userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-polled-once@example.com")

	day := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.MarkCalendarFeedPolled(ctx, userID, selector, day); err != nil {
		t.Fatalf("first MarkCalendarFeedPolled: %v", err)
	}
	mark := polledOnOf(t, repo, userID)
	if mark == nil || !mark.Equal(day) {
		t.Fatalf("expected the mark at %s, got %v", day, mark)
	}

	// A second poll the same day is a no-op write: the predicate matches zero
	// rows (already holds today), and the stored value does not move.
	if err := repo.MarkCalendarFeedPolled(ctx, userID, selector, day); err != nil {
		t.Fatalf("same-day MarkCalendarFeedPolled: %v", err)
	}
	if mark := polledOnOf(t, repo, userID); mark == nil || !mark.Equal(day) {
		t.Fatalf("same-day poll moved the mark: %v", mark)
	}
}

// TestMarkCalendarFeedPolledAdvancesOnANewDay proves the write is not stuck
// once it lands: the next owner-day's poll moves the mark forward.
func TestMarkCalendarFeedPolledAdvancesOnANewDay(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	ctx := context.Background()
	userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-polled-nextday@example.com")

	first := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	second := time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC)
	if err := repo.MarkCalendarFeedPolled(ctx, userID, selector, first); err != nil {
		t.Fatalf("first day: %v", err)
	}
	if err := repo.MarkCalendarFeedPolled(ctx, userID, selector, second); err != nil {
		t.Fatalf("second day: %v", err)
	}
	if mark := polledOnOf(t, repo, userID); mark == nil || !mark.Equal(second) {
		t.Fatalf("expected the mark to advance to %s, got %v", second, mark)
	}
}

// TestMarkCalendarFeedPolledRefusesAnEarlierDay pins monotonicity: a
// late-returning or replayed call presenting an earlier day than the one
// already recorded must not walk the mark backwards.
func TestMarkCalendarFeedPolledRefusesAnEarlierDay(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	ctx := context.Background()
	userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-polled-earlier@example.com")

	later := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	earlier := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	if err := repo.MarkCalendarFeedPolled(ctx, userID, selector, later); err != nil {
		t.Fatalf("later day: %v", err)
	}
	if err := repo.MarkCalendarFeedPolled(ctx, userID, selector, earlier); err != nil {
		t.Fatalf("earlier day: %v", err)
	}
	if mark := polledOnOf(t, repo, userID); mark == nil || !mark.Equal(later) {
		t.Fatalf("expected the later mark %s to stand, got %v", later, mark)
	}
}

// TestMarkCalendarFeedPolledRefusesAStaleSelector proves the write is pinned
// to the token that was actually verified: a rotation landing between the
// verify and this write must not let the poll re-arm a mark under the OLD
// selector, and must not touch the new one either.
func TestMarkCalendarFeedPolledRefusesAStaleSelector(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	ctx := context.Background()
	userID, oldSelector := seedArmedFeedForPolledTest(t, repo, "feed-polled-stale-selector@example.com")

	// The owner rotates in between: the row now carries a different selector.
	newSelector := "sel-rotated-" + oldSelector
	if err := repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{"calendar_feed_selector": newSelector}).Error; err != nil {
		t.Fatalf("simulate rotation: %v", err)
	}

	if err := repo.MarkCalendarFeedPolled(ctx, userID, oldSelector, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("MarkCalendarFeedPolled against a stale selector: %v", err)
	}
	if mark := polledOnOf(t, repo, userID); mark != nil {
		t.Fatalf("a poll presenting a superseded selector wrote a mark: %v", mark)
	}
}

// TestMarkCalendarFeedPolledRefusesAZeroOwnerID pins the same owner-required
// floor every other zero-row-silent feed writer holds: an absent id is
// invalid input, never a wildcard.
func TestMarkCalendarFeedPolledRefusesAZeroOwnerID(t *testing.T) {
	repo := openWebhookRepoForTest(t)
	if err := repo.MarkCalendarFeedPolled(context.Background(), 0, "any-selector", time.Now().UTC()); err == nil {
		t.Fatal("expected a zero owner id to be refused")
	}
}

// TestEveryCalendarFeedSelectorClearingSiteAlsoClearsTheLastPolledMark walks
// each of the eight sites that NULL calendar_feed_selector and proves the
// mark goes with it, one subtest per site so a failure names exactly which
// one regressed (the AST guard in calendar_feed_fence_writers_guard_test.go
// pins that the SET is complete; this pins that each MEMBER actually behaves).
func TestEveryCalendarFeedSelectorClearingSiteAlsoClearsTheLastPolledMark(t *testing.T) {
	ctx := context.Background()
	seedMark := func(t *testing.T, repo *UserRepository, userID uint, selector string) {
		t.Helper()
		if err := repo.MarkCalendarFeedPolled(ctx, userID, selector, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)); err != nil {
			t.Fatalf("seed the mark: %v", err)
		}
		if polledOnOf(t, repo, userID) == nil {
			t.Fatal("the mark did not take: the clearing assertion below would pass vacuously")
		}
	}

	t.Run("SaveCalendarFeedToken (rotate)", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-save@example.com")
		seedMark(t, repo, userID, selector)
		if err := repo.SaveCalendarFeedToken(ctx, userID, models.CalendarFeedTokenColumns{
			Selector:     "rotated-" + selector,
			VerifierHash: "bcrypt-hash",
			VerifierMAC:  "verifier-mac",
		}); err != nil {
			t.Fatalf("SaveCalendarFeedToken: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("rotate left the mark standing: %v", mark)
		}
	})

	t.Run("ClearCalendarFeedToken (revoke)", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-revoke@example.com")
		seedMark(t, repo, userID, selector)
		if err := repo.ClearCalendarFeedToken(ctx, userID); err != nil {
			t.Fatalf("ClearCalendarFeedToken: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("revoke left the mark standing: %v", mark)
		}
	})

	t.Run("DisarmCalendarFeedTokensWithoutMAC", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-disarm-nomac@example.com")
		seedMark(t, repo, userID, selector)
		if _, err := repo.DisarmCalendarFeedTokensWithoutMAC(ctx); err != nil {
			t.Fatalf("DisarmCalendarFeedTokensWithoutMAC: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("the no-MAC disarm left the mark standing: %v", mark)
		}
	})

	t.Run("DisarmAllCalendarFeedTokens", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-disarm-all@example.com")
		seedMark(t, repo, userID, selector)
		if _, err := repo.DisarmAllCalendarFeedTokens(ctx); err != nil {
			t.Fatalf("DisarmAllCalendarFeedTokens: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("the bulk disarm left the mark standing: %v", mark)
		}
	})

	t.Run("UpdateRecoveryCodeHashAndRevokeSessions", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-recovery-rotate@example.com")
		seedMark(t, repo, userID, selector)
		version := storedSessionVersionForTest(t, repo, userID)
		if err := repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, userID, version, "new-recovery-hash", nil); err != nil {
			t.Fatalf("UpdateRecoveryCodeHashAndRevokeSessions: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("recovery-code rotation left the mark standing: %v", mark)
		}
	})

	t.Run("ForceResetPasswordAndRevokeSessions", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-force-reset@example.com")
		seedMark(t, repo, userID, selector)
		if err := repo.ForceResetPasswordAndRevokeSessions(ctx, userID, "new-password-hash"); err != nil {
			t.Fatalf("ForceResetPasswordAndRevokeSessions: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("the operator-forced reset left the mark standing: %v", mark)
		}
	})

	t.Run("UpdatePasswordRecoveryCodeAndRevokeSessionsCAS", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-recovery-cas@example.com")
		seedMark(t, repo, userID, selector)
		user, err := repo.FindByID(ctx, userID)
		if err != nil {
			t.Fatalf("reload user: %v", err)
		}
		version := storedSessionVersionForTest(t, repo, userID)
		if err := repo.UpdatePasswordRecoveryCodeAndRevokeSessionsCAS(ctx, userID, user.PasswordHash, version, "new-password-hash", "new-recovery-hash", nil); err != nil {
			t.Fatalf("UpdatePasswordRecoveryCodeAndRevokeSessionsCAS: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("the recovery-code reset CAS left the mark standing: %v", mark)
		}
	})

	t.Run("ClearAllDataAndResetSettings", func(t *testing.T) {
		repo := openWebhookRepoForTest(t)
		userID, selector := seedArmedFeedForPolledTest(t, repo, "feed-clear-clear-data@example.com")
		seedMark(t, repo, userID, selector)
		version := storedSessionVersionForTest(t, repo, userID)
		if err := repo.ClearAllDataAndResetSettings(ctx, userID, version); err != nil {
			t.Fatalf("ClearAllDataAndResetSettings: %v", err)
		}
		if mark := polledOnOf(t, repo, userID); mark != nil {
			t.Fatalf("the clear-data wipe left the mark standing: %v", mark)
		}
	})
}
