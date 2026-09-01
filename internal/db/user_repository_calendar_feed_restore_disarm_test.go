package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestDisarmAllCalendarFeedTokensClearsEveryArmedRowIncludingMACCarriers is the
// half of the restore fence its sibling disarm cannot do, and the assertion
// order says why. It first demonstrates the finding at the repository level: a
// MAC-carrying row minted under the SAME key that is still in force verifies
// its token perfectly — which is exactly the state a restored backup hands the
// app, since a restore changes no key. DisarmCalendarFeedTokensWithoutMAC
// leaves that row standing on purpose (a rotation breaks it on its own); under
// a restore nothing breaks it, so this method has to take it.
func TestDisarmAllCalendarFeedTokensClearsEveryArmedRowIncludingMACCarriers(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()

	// One key throughout: a restore is precisely the case where SECRET_KEY did
	// not change, so every stored verifier is still valid.
	key := []byte("restore-fence-test-secret-key-01")

	modern := createUserForTimezoneTest(t, repo, "feed-restore-modern@example.com")
	legacy := createUserForTimezoneTest(t, repo, "feed-restore-legacy@example.com")
	off := createUserForTimezoneTest(t, repo, "feed-restore-off@example.com")

	modernToken, modernColumns, err := services.GenerateCalendarFeedToken(key)
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken(modern): %v", err)
	}
	if err := repo.SaveCalendarFeedToken(ctx, modern.ID, modernColumns); err != nil {
		t.Fatalf("SaveCalendarFeedToken(modern): %v", err)
	}

	// The pre-032 shape too, so the wider predicate is proved over both
	// generations rather than only the one this method was written for.
	_, legacyColumns, err := services.GenerateCalendarFeedToken(key)
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken(legacy): %v", err)
	}
	if err := repo.SaveCalendarFeedToken(ctx, legacy.ID, legacyColumns); err != nil {
		t.Fatalf("SaveCalendarFeedToken(legacy): %v", err)
	}
	if err := repo.database.Model(&models.User{}).Where("id = ?", legacy.ID).
		Update("calendar_feed_verifier_mac", "").Error; err != nil {
		t.Fatalf("blank legacy MAC: %v", err)
	}

	modernBefore := reloadUserForCalendarFeed(t, repo, modern.ID)
	// The finding, demonstrated: under the unchanged key this row's token is
	// still good, so nothing about a restore would have refused it.
	if !services.VerifyCalendarFeedToken(key, modernToken, models.CalendarFeedTokenColumns{
		Selector:     modernBefore.CalendarFeedSelector,
		VerifierHash: modernBefore.CalendarFeedVerifierHash,
		VerifierMAC:  modernBefore.CalendarFeedVerifierMAC,
	}) {
		t.Fatal("precondition lost: a MAC-carrying row no longer verifies under its own key, so this disarm is measuring nothing")
	}
	// And the narrow sibling leaves it exactly there — the gap this method fills.
	if narrow, err := repo.DisarmCalendarFeedTokensWithoutMAC(ctx); err != nil {
		t.Fatalf("DisarmCalendarFeedTokensWithoutMAC: %v", err)
	} else if narrow != 1 {
		t.Fatalf("the narrow disarm must take only the legacy row, got %d", narrow)
	}
	if still := reloadUserForCalendarFeed(t, repo, modern.ID); still.CalendarFeedSelector != modernBefore.CalendarFeedSelector {
		t.Fatal("precondition lost: the narrow disarm already cleared the MAC-carrying row, so the wide one proves nothing")
	}

	disarmed, err := repo.DisarmAllCalendarFeedTokens(ctx)
	if err != nil {
		t.Fatalf("DisarmAllCalendarFeedTokens: %v", err)
	}
	if disarmed != 1 {
		t.Fatalf("expected the one still-armed row disarmed, got %d", disarmed)
	}

	modernAfter := reloadUserForCalendarFeed(t, repo, modern.ID)
	if modernAfter.CalendarFeedSelector != "" || modernAfter.CalendarFeedVerifierHash != "" || modernAfter.CalendarFeedVerifierMAC != "" {
		t.Fatalf("the MAC-carrying row must be fully disarmed, got selector=%q hash=%q mac=%q",
			modernAfter.CalendarFeedSelector, modernAfter.CalendarFeedVerifierHash, modernAfter.CalendarFeedVerifierMAC)
	}
	if modernAfter.AuthSessionVersion != modernBefore.AuthSessionVersion {
		t.Fatalf("disarm must not bump auth_session_version: before=%d after=%d", modernBefore.AuthSessionVersion, modernAfter.AuthSessionVersion)
	}
	if _, ok, err := repo.FindByCalendarFeedSelector(ctx, modernBefore.CalendarFeedSelector); err != nil || ok {
		t.Fatalf("the disarmed selector must no longer resolve (ok=%v, err=%v)", ok, err)
	}

	offAfter := reloadUserForCalendarFeed(t, repo, off.ID)
	if offAfter.CalendarFeedSelector != "" || offAfter.CalendarFeedVerifierHash != "" || offAfter.CalendarFeedVerifierMAC != "" {
		t.Fatalf("a feed-off row must stay off, got selector=%q hash=%q mac=%q",
			offAfter.CalendarFeedSelector, offAfter.CalendarFeedVerifierHash, offAfter.CalendarFeedVerifierMAC)
	}

	again, err := repo.DisarmAllCalendarFeedTokens(ctx)
	if err != nil {
		t.Fatalf("second DisarmAllCalendarFeedTokens: %v", err)
	}
	if again != 0 {
		t.Fatalf("second pass must be a zero-row no-op, got %d", again)
	}
}
