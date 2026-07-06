package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func openCalendarFeedRepoForTest(t *testing.T) *UserRepository {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "calendar-feed.db"))
	return NewUserRepository(database)
}

func reloadUserForCalendarFeed(t *testing.T, repo *UserRepository, userID uint) models.User {
	t.Helper()
	var reloaded models.User
	if err := repo.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user %d: %v", userID, err)
	}
	return reloaded
}

// TestSaveCalendarFeedTokenPersistsAndLooksUpBySelector proves the narrow write
// stores exactly the two feed-token columns and that FindByCalendarFeedSelector
// resolves the owner by the stored selector, carrying the verifier hash back so
// the caller can verify without a second read. auth_session_version must be
// untouched — a feed capability is not a login credential.
func TestSaveCalendarFeedTokenPersistsAndLooksUpBySelector(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-persist@example.com")

	before := reloadUserForCalendarFeed(t, repo, user.ID)
	if before.CalendarFeedSelector != "" || before.CalendarFeedVerifierHash != "" {
		t.Fatalf("expected fresh user to have empty feed columns, got selector=%q hash=%q", before.CalendarFeedSelector, before.CalendarFeedVerifierHash)
	}

	const selector = "SELECTOR16CHARSXX"
	const verifierHash = "opaque-bcrypt-hash-stand-in"
	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     selector,
		VerifierHash: verifierHash,
	}); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}

	after := reloadUserForCalendarFeed(t, repo, user.ID)
	if after.CalendarFeedSelector != selector {
		t.Fatalf("expected selector %q persisted, got %q", selector, after.CalendarFeedSelector)
	}
	if after.CalendarFeedVerifierHash != verifierHash {
		t.Fatalf("expected verifier hash %q persisted verbatim, got %q", verifierHash, after.CalendarFeedVerifierHash)
	}
	if after.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("SaveCalendarFeedToken must not bump auth_session_version: before=%d after=%d", before.AuthSessionVersion, after.AuthSessionVersion)
	}

	found, ok, err := repo.FindByCalendarFeedSelector(context.Background(), selector)
	if err != nil {
		t.Fatalf("FindByCalendarFeedSelector: %v", err)
	}
	if !ok {
		t.Fatal("expected the stored selector to resolve a user")
	}
	if found.ID != user.ID {
		t.Fatalf("expected selector to resolve user %d, got %d", user.ID, found.ID)
	}
	if found.CalendarFeedVerifierHash != verifierHash {
		t.Fatalf("expected lookup to carry the verifier hash %q, got %q", verifierHash, found.CalendarFeedVerifierHash)
	}
}

// TestFindByCalendarFeedSelectorMissingIsNotFound proves an unknown selector and
// an empty selector both return the not-found shape (zero user, ok=false, nil
// error) — the same outcome a wrong verifier will later produce, so the feed
// endpoint has no existence oracle.
func TestFindByCalendarFeedSelectorMissingIsNotFound(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	// A user exists but with the feed off (both columns NULL): its NULL selector
	// must not be matched by any lookup, including an empty-string lookup.
	createUserForTimezoneTest(t, repo, "feed-off@example.com")

	for _, selector := range []string{"NO-SUCH-SELECTOR", ""} {
		found, ok, err := repo.FindByCalendarFeedSelector(context.Background(), selector)
		if err != nil {
			t.Fatalf("FindByCalendarFeedSelector(%q): %v", selector, err)
		}
		if ok {
			t.Fatalf("expected selector %q to be not-found, got user %d", selector, found.ID)
		}
		if found.ID != 0 {
			t.Fatalf("expected zero user on not-found for %q, got id %d", selector, found.ID)
		}
	}
}

// TestSaveCalendarFeedTokenRotationReplacesRow proves a second save overwrites
// the previous selector/hash for the same user, so the OLD selector stops
// resolving and only the NEW selector resolves. This is the persistence arm of
// "rotate revokes the old feed URL".
func TestSaveCalendarFeedTokenRotationReplacesRow(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-rotate@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "OLDSELECTOR16XXX",
		VerifierHash: "old-hash",
	}); err != nil {
		t.Fatalf("seed old token: %v", err)
	}
	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "NEWSELECTOR16XXX",
		VerifierHash: "new-hash",
	}); err != nil {
		t.Fatalf("rotate token: %v", err)
	}

	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "OLDSELECTOR16XXX"); err != nil || ok {
		t.Fatalf("expected old selector to no longer resolve after rotation (ok=%v err=%v)", ok, err)
	}
	found, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "NEWSELECTOR16XXX")
	if err != nil || !ok {
		t.Fatalf("expected new selector to resolve after rotation (ok=%v err=%v)", ok, err)
	}
	if found.CalendarFeedVerifierHash != "new-hash" {
		t.Fatalf("expected rotated verifier hash 'new-hash', got %q", found.CalendarFeedVerifierHash)
	}
}

// TestClearCalendarFeedTokenRevokes proves the revoke path NULLs both columns so
// the selector stops resolving (the feed URL would 404). The columns end empty on
// reload and the lookup is not-found.
func TestClearCalendarFeedTokenRevokes(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-revoke@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "REVOKESELECT16XX",
		VerifierHash: "hash-to-revoke",
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if err := repo.ClearCalendarFeedToken(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearCalendarFeedToken: %v", err)
	}

	got := reloadUserForCalendarFeed(t, repo, user.ID)
	if got.CalendarFeedSelector != "" || got.CalendarFeedVerifierHash != "" {
		t.Fatalf("expected feed columns cleared after revoke, got selector=%q hash=%q", got.CalendarFeedSelector, got.CalendarFeedVerifierHash)
	}
	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "REVOKESELECT16XX"); err != nil || ok {
		t.Fatalf("expected revoked selector to be not-found (ok=%v err=%v)", ok, err)
	}
}

// TestSaveCalendarFeedTokenScopedToUser proves the write is strictly scoped to the
// target user id: setting owner A's feed token never touches owner B's row (the
// household multi-owner isolation boundary).
func TestSaveCalendarFeedTokenScopedToUser(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "feed-owner@example.com")
	other := createUserForTimezoneTest(t, repo, "feed-other@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), other.ID, models.CalendarFeedTokenColumns{
		Selector:     "OTHERSELECT16XXX",
		VerifierHash: "other-hash",
	}); err != nil {
		t.Fatalf("seed other owner token: %v", err)
	}
	if err := repo.SaveCalendarFeedToken(context.Background(), owner.ID, models.CalendarFeedTokenColumns{
		Selector:     "OWNERSELECT16XXX",
		VerifierHash: "owner-hash",
	}); err != nil {
		t.Fatalf("SaveCalendarFeedToken owner: %v", err)
	}

	gotOther := reloadUserForCalendarFeed(t, repo, other.ID)
	if gotOther.CalendarFeedSelector != "OTHERSELECT16XXX" || gotOther.CalendarFeedVerifierHash != "other-hash" {
		t.Fatalf("other owner row was mutated by owner save: %+v", gotOther)
	}

	// Revoking the owner's token must likewise leave the other owner intact.
	if err := repo.ClearCalendarFeedToken(context.Background(), owner.ID); err != nil {
		t.Fatalf("ClearCalendarFeedToken owner: %v", err)
	}
	gotOtherAfter := reloadUserForCalendarFeed(t, repo, other.ID)
	if gotOtherAfter.CalendarFeedSelector != "OTHERSELECT16XXX" {
		t.Fatalf("owner revoke must not clear the other owner's selector, got %q", gotOtherAfter.CalendarFeedSelector)
	}
}

// TestClearAllDataResetsCalendarFeedColumns proves a clear-data wipe revokes the
// feed: both columns are NULLed so a previously-issued feed URL 404s against the
// freshly emptied account (its selector no longer resolves).
func TestClearAllDataResetsCalendarFeedColumns(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-clear@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "CLEARSELECT16XXX",
		VerifierHash: "hash-to-clear",
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForCalendarFeed(t, repo, user.ID)
	if got.CalendarFeedSelector != "" || got.CalendarFeedVerifierHash != "" {
		t.Fatalf("expected feed columns cleared after clear-data, got selector=%q hash=%q", got.CalendarFeedSelector, got.CalendarFeedVerifierHash)
	}
	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "CLEARSELECT16XXX"); err != nil || ok {
		t.Fatalf("expected cleared selector to be not-found after clear-data (ok=%v err=%v)", ok, err)
	}
}

// TestCalendarFeedTokenHashAtRestInDBRow proves the persisted verifier_hash row
// value is a bcrypt hash produced by the service — NOT the shown-once verifier
// plaintext. The plaintext verifier substring must be absent from the stored
// column, and the stored hash must verify against the real verifier. This is the
// end-to-end hash-at-rest check at the persistence boundary, using the real
// service generator to produce the storables.
func TestCalendarFeedTokenHashAtRestInDBRow(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-hashrest@example.com")

	fullToken, selector, verifierHash, err := services.GenerateCalendarFeedToken()
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	_, verifier, ok := services.SplitCalendarFeedToken(fullToken)
	if !ok {
		t.Fatal("expected generated token to split")
	}

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     selector,
		VerifierHash: verifierHash,
	}); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}

	got := reloadUserForCalendarFeed(t, repo, user.ID)
	// The stored hash must not be, or contain, the secret verifier plaintext, and
	// must not contain the full shown-once token.
	if got.CalendarFeedVerifierHash == verifier {
		t.Fatal("stored verifier hash equals the verifier plaintext")
	}
	if strings.Contains(got.CalendarFeedVerifierHash, verifier) {
		t.Fatalf("stored verifier hash %q contains the verifier plaintext", got.CalendarFeedVerifierHash)
	}
	if strings.Contains(got.CalendarFeedVerifierHash, fullToken) {
		t.Fatal("stored verifier hash contains the full token")
	}
	// The presented full token verifies against the stored storables end to end.
	if !services.VerifyCalendarFeedToken(fullToken, got.CalendarFeedSelector, got.CalendarFeedVerifierHash) {
		t.Fatal("expected the full token to verify against the persisted storables")
	}
}

// TestFindByCalendarFeedSelectorReturnsErrorOnQueryFailure exercises the
// non-not-found error branch of FindByCalendarFeedSelector: when the underlying
// SELECT fails for a reason other than gorm.ErrRecordNotFound (here because the
// users table has been dropped), the method must propagate the error and report
// ok=false with a zero user — distinct from the clean "not found" path, which
// returns a nil error. Mirrors the drop-table technique in
// TestListAllForNotifyReturnsErrorOnQueryFailure.
func TestFindByCalendarFeedSelectorReturnsErrorOnQueryFailure(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)

	if err := repo.database.Exec("DROP TABLE users").Error; err != nil {
		t.Fatalf("drop users table: %v", err)
	}

	user, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "ANYSELECTOR16XXX")
	if err == nil {
		t.Fatal("expected FindByCalendarFeedSelector to error when the users table is missing")
	}
	if ok {
		t.Fatal("expected ok=false on a query failure")
	}
	if user.ID != 0 {
		t.Fatalf("expected zero user on a query failure, got id %d", user.ID)
	}
}
