package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// calendar_feed_key_epoch (migration 039) records WHICH verification regime a
// feed token was minted under. It is the one health question this row can answer
// honestly — the verifier is not stored in the clear and its MAC cannot be
// recomputed — and it is what decides whether an owner can still retire a URL
// they handed out. Everything here defends its single-writer shape.

// TestSaveCalendarFeedTokenStampsTheKeyEpochWithTheTokenItself proves the stamp
// rides the mint: it is written in the same statement as the token triple, and
// a rotation re-stamps it. Arming a token and recording what armed it are one
// event or the row can disagree with itself.
func TestSaveCalendarFeedTokenStampsTheKeyEpochWithTheTokenItself(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-epoch-stamp@example.com")
	ctx := context.Background()

	if before := reloadUserForCalendarFeed(t, repo, user.ID); before.CalendarFeedKeyEpoch != "" {
		t.Fatalf("expected a fresh row to carry no key epoch, got %q", before.CalendarFeedKeyEpoch)
	}

	_, columns, err := services.GenerateCalendarFeedToken([]byte(calendarFeedRepoTestSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	if columns.KeyEpoch == "" {
		t.Fatal("the generator produced no key epoch: an empty stamp reads as \"issued before this was recorded\", which a fresh mint must never claim")
	}
	if err := repo.SaveCalendarFeedToken(ctx, user.ID, columns); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}

	minted := reloadUserForCalendarFeed(t, repo, user.ID)
	if minted.CalendarFeedKeyEpoch != columns.KeyEpoch {
		t.Fatalf("expected the stamp %q beside the token, got %q", columns.KeyEpoch, minted.CalendarFeedKeyEpoch)
	}
	if minted.CalendarFeedSelector != columns.Selector {
		t.Fatal("the token and its stamp did not land together")
	}

	// A rotation is a second mint, and it re-stamps: under a key that has changed
	// since, the row must stop claiming the previous regime.
	_, rotated, err := services.GenerateCalendarFeedToken([]byte("a-different-calendar-feed-db-test-key"))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken (rotation): %v", err)
	}
	if rotated.KeyEpoch == columns.KeyEpoch {
		t.Fatal("the two keys derived the same epoch: this case can no longer show a re-stamp")
	}
	if err := repo.SaveCalendarFeedToken(ctx, user.ID, rotated); err != nil {
		t.Fatalf("SaveCalendarFeedToken (rotation): %v", err)
	}
	if after := reloadUserForCalendarFeed(t, repo, user.ID); after.CalendarFeedKeyEpoch != rotated.KeyEpoch {
		t.Fatalf("expected the rotation to re-stamp the epoch to %q, got %q", rotated.KeyEpoch, after.CalendarFeedKeyEpoch)
	}
}

// TestRevokeAndDisarmLeaveTheCalendarFeedKeyEpochAlone pins the other half of
// the single-writer shape. Revoke and both bulk disarms clear the token, and a
// stamp without a token asserts nothing — so none of them may write this column.
// Leaving it to the mint is what keeps SaveCalendarFeedToken the sole writer and
// the restore-fence completeness guard free of a new exemption.
func TestRevokeAndDisarmLeaveTheCalendarFeedKeyEpochAlone(t *testing.T) {
	ctx := context.Background()

	for _, testCase := range []struct {
		name  string
		clear func(t *testing.T, repo *UserRepository, userID uint)
	}{
		{
			name: "revoke",
			clear: func(t *testing.T, repo *UserRepository, userID uint) {
				if err := repo.ClearCalendarFeedToken(ctx, userID); err != nil {
					t.Fatalf("ClearCalendarFeedToken: %v", err)
				}
			},
		},
		{
			name: "the restore fence's bulk disarm",
			clear: func(t *testing.T, repo *UserRepository, _ uint) {
				if _, err := repo.DisarmAllCalendarFeedTokens(ctx); err != nil {
					t.Fatalf("DisarmAllCalendarFeedTokens: %v", err)
				}
			},
		},
		{
			name: "the key-rotation sentinel's bulk disarm",
			clear: func(t *testing.T, repo *UserRepository, _ uint) {
				if _, err := repo.DisarmCalendarFeedTokensWithoutMAC(ctx); err != nil {
					t.Fatalf("DisarmCalendarFeedTokensWithoutMAC: %v", err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			repo := openCalendarFeedRepoForTest(t)
			user := createUserForTimezoneTest(t, repo, "feed-epoch-revoke@example.com")

			_, columns, err := services.GenerateCalendarFeedToken([]byte(calendarFeedRepoTestSecretKey))
			if err != nil {
				t.Fatalf("GenerateCalendarFeedToken: %v", err)
			}
			// The sentinel's disarm only reaches MAC-less rows, so its case seeds one.
			if testCase.name == "the key-rotation sentinel's bulk disarm" {
				columns.VerifierMAC = ""
			}
			if err := repo.SaveCalendarFeedToken(ctx, user.ID, columns); err != nil {
				t.Fatalf("SaveCalendarFeedToken: %v", err)
			}

			testCase.clear(t, repo, user.ID)

			after := reloadUserForCalendarFeed(t, repo, user.ID)
			if after.CalendarFeedSelector != "" {
				t.Fatalf("the fixture did not actually clear the token (selector %q), so the assertion below would be vacuous", after.CalendarFeedSelector)
			}
			if after.CalendarFeedKeyEpoch != columns.KeyEpoch {
				t.Fatalf("clearing the token also wrote calendar_feed_key_epoch (%q -> %q): only a mint may stamp it", columns.KeyEpoch, after.CalendarFeedKeyEpoch)
			}
		})
	}
}

// TestEveryMintedCalendarFeedTokenCarriesAKeyEpoch guards the mint's fail-closed
// direction as an invariant over what it RETURNS, which is the form that can
// actually be violated. An empty stamp is not a neutral value: the row reads it
// as "issued before this was recorded", so a token minted with one would be
// permanently unable to say it can be retired.
//
// The keyless case is not the test: a nil key is refused a step earlier, by the
// verifier MAC, so a mint that dropped the epoch derivation entirely would still
// fail it. This asserts the successful path instead.
func TestEveryMintedCalendarFeedTokenCarriesAKeyEpoch(t *testing.T) {
	for _, key := range []string{calendarFeedRepoTestSecretKey, "another-calendar-feed-db-test-key"} {
		_, columns, err := services.GenerateCalendarFeedToken([]byte(key))
		if err != nil {
			t.Fatalf("GenerateCalendarFeedToken: %v", err)
		}
		if columns.KeyEpoch == "" {
			t.Fatal("a mint returned an empty key epoch, which the row reads as \"issued before this was recorded\"")
		}
	}
}

// TestSaveCalendarFeedTokenAddsNoFeedAccessWriter states in one place what the
// restore-fence completeness guard would otherwise only imply: the epoch stamp
// added no writer of a feed access column, so exemptCalendarFeedWriters needs no
// new entry. An exemption added "because the guard complained" is how that guard
// turns into an allowlist.
func TestSaveCalendarFeedTokenAddsNoFeedAccessWriter(t *testing.T) {
	if _, exempt := exemptCalendarFeedWriters["SaveCalendarFeedToken"]; exempt {
		t.Fatal("SaveCalendarFeedToken is exempt from the restore fence: the epoch stamp rides an existing revocation write and must not have bought an exemption")
	}
	for _, column := range calendarFeedAccessColumns {
		if column == `"calendar_feed_key_epoch"` {
			t.Fatal("calendar_feed_key_epoch was added to the feed ACCESS columns: it records what minted a token and grants no access on its own")
		}
	}
	// Anti-vacuity: the model must still carry the field this test is about.
	if (models.CalendarFeedTokenColumns{KeyEpoch: "x"}).KeyEpoch != "x" {
		t.Fatal("CalendarFeedTokenColumns no longer carries KeyEpoch")
	}
}
