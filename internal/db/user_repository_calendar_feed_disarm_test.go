package db

import (
	"context"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestDisarmCalendarFeedTokensWithoutMACOnlyTouchesLegacyArmedRows first
// demonstrates the defect the boot sentinel exists for — a pre-032 row (bcrypt
// hash, no MAC) STILL VERIFIES its token under a rotated SECRET_KEY — and then
// proves the bulk disarm removes exactly that row and nothing else: an armed
// row with a MAC and a feed-off row stay byte-identical, auth_session_version
// is never bumped, and a second pass is a zero-row no-op.
func TestDisarmCalendarFeedTokensWithoutMACOnlyTouchesLegacyArmedRows(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()

	oldKey := []byte("disarm-test-old-secret-key-01234")
	rotatedKey := []byte("disarm-test-new-secret-key-01234")

	legacy := createUserForTimezoneTest(t, repo, "feed-disarm-legacy@example.com")
	modern := createUserForTimezoneTest(t, repo, "feed-disarm-modern@example.com")
	off := createUserForTimezoneTest(t, repo, "feed-disarm-off@example.com")

	// Arm the legacy row with a REAL token minted under the old key, then blank
	// the MAC column — exactly the shape migration 032 left for rows minted
	// before it (bcrypt hash present, MAC absent).
	legacyToken, legacyColumns, err := services.GenerateCalendarFeedToken(oldKey)
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

	_, modernColumns, err := services.GenerateCalendarFeedToken(rotatedKey)
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken(modern): %v", err)
	}
	if err := repo.SaveCalendarFeedToken(ctx, modern.ID, modernColumns); err != nil {
		t.Fatalf("SaveCalendarFeedToken(modern): %v", err)
	}

	// The live defect, demonstrated: the legacy row's token verifies under the
	// ROTATED key, because the bcrypt branch never consults SECRET_KEY. This is
	// the assertion that fails ("false") the moment someone "fixes" the disarm
	// by re-keying bcrypt instead — the sentinel exists precisely because this
	// stays true.
	legacyRow := reloadUserForCalendarFeed(t, repo, legacy.ID)
	if !services.VerifyCalendarFeedToken(rotatedKey, legacyToken, models.CalendarFeedTokenColumns{
		Selector:     legacyRow.CalendarFeedSelector,
		VerifierHash: legacyRow.CalendarFeedVerifierHash,
		VerifierMAC:  legacyRow.CalendarFeedVerifierMAC,
	}) {
		t.Fatal("precondition lost: a pre-032 row no longer verifies under a rotated key — if bcrypt fallback was removed, this disarm (and the sentinel) may be obsolete")
	}
	modernBefore := reloadUserForCalendarFeed(t, repo, modern.ID)

	disarmed, err := repo.DisarmCalendarFeedTokensWithoutMAC(ctx)
	if err != nil {
		t.Fatalf("DisarmCalendarFeedTokensWithoutMAC: %v", err)
	}
	if disarmed != 1 {
		t.Fatalf("expected exactly the one legacy row disarmed, got %d", disarmed)
	}

	legacyAfter := reloadUserForCalendarFeed(t, repo, legacy.ID)
	if legacyAfter.CalendarFeedSelector != "" || legacyAfter.CalendarFeedVerifierHash != "" || legacyAfter.CalendarFeedVerifierMAC != "" {
		t.Fatalf("legacy row must be fully disarmed, got selector=%q hash=%q mac=%q",
			legacyAfter.CalendarFeedSelector, legacyAfter.CalendarFeedVerifierHash, legacyAfter.CalendarFeedVerifierMAC)
	}
	if legacyAfter.AuthSessionVersion != legacyRow.AuthSessionVersion {
		t.Fatalf("disarm must not bump auth_session_version: before=%d after=%d", legacyRow.AuthSessionVersion, legacyAfter.AuthSessionVersion)
	}
	if _, ok, err := repo.FindByCalendarFeedSelector(ctx, legacyRow.CalendarFeedSelector); err != nil || ok {
		t.Fatalf("disarmed selector must no longer resolve (ok=%v, err=%v)", ok, err)
	}

	modernAfter := reloadUserForCalendarFeed(t, repo, modern.ID)
	if modernAfter.CalendarFeedSelector != modernBefore.CalendarFeedSelector ||
		modernAfter.CalendarFeedVerifierHash != modernBefore.CalendarFeedVerifierHash ||
		modernAfter.CalendarFeedVerifierMAC != modernBefore.CalendarFeedVerifierMAC {
		t.Fatal("a MAC-carrying row must survive the disarm untouched (it fails closed on its own)")
	}

	offAfter := reloadUserForCalendarFeed(t, repo, off.ID)
	if offAfter.CalendarFeedSelector != "" || offAfter.CalendarFeedVerifierHash != "" || offAfter.CalendarFeedVerifierMAC != "" {
		t.Fatalf("a feed-off row must stay off, got selector=%q hash=%q mac=%q",
			offAfter.CalendarFeedSelector, offAfter.CalendarFeedVerifierHash, offAfter.CalendarFeedVerifierMAC)
	}

	again, err := repo.DisarmCalendarFeedTokensWithoutMAC(ctx)
	if err != nil {
		t.Fatalf("second DisarmCalendarFeedTokensWithoutMAC: %v", err)
	}
	if again != 0 {
		t.Fatalf("second pass must be a zero-row no-op, got %d", again)
	}
}
