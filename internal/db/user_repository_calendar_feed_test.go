package db

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// calendarFeedRepoTestSecretKey keys the verifier MACs minted by the tests in
// this file that drive the real service generator.
const calendarFeedRepoTestSecretKey = "calendar-feed-db-test-key"

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
// stores exactly the feed-token columns and that FindByCalendarFeedSelector
// resolves the owner by the stored selector, carrying BOTH verifier columns back
// so the caller can verify without a second read. auth_session_version must be
// untouched — a feed capability is not a login credential.
func TestSaveCalendarFeedTokenPersistsAndLooksUpBySelector(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-persist@example.com")

	before := reloadUserForCalendarFeed(t, repo, user.ID)
	if before.CalendarFeedSelector != "" || before.CalendarFeedVerifierHash != "" || before.CalendarFeedVerifierMAC != "" {
		t.Fatalf("expected fresh user to have empty feed columns, got selector=%q hash=%q mac=%q",
			before.CalendarFeedSelector, before.CalendarFeedVerifierHash, before.CalendarFeedVerifierMAC)
	}

	const selector = "SELECTOR16CHARSXX"
	const verifierHash = "opaque-bcrypt-hash-stand-in"
	const verifierMAC = "opaque-keyed-mac-stand-in"
	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     selector,
		VerifierHash: verifierHash,
		VerifierMAC:  verifierMAC,
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
	if after.CalendarFeedVerifierMAC != verifierMAC {
		t.Fatalf("expected verifier MAC %q persisted verbatim, got %q", verifierMAC, after.CalendarFeedVerifierMAC)
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
	if found.CalendarFeedVerifierMAC != verifierMAC {
		t.Fatalf("expected lookup to carry the verifier MAC %q, got %q", verifierMAC, found.CalendarFeedVerifierMAC)
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
		VerifierMAC:  "old-mac",
	}); err != nil {
		t.Fatalf("seed old token: %v", err)
	}
	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "NEWSELECTOR16XXX",
		VerifierHash: "new-hash",
		VerifierMAC:  "new-mac",
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
	if found.CalendarFeedVerifierMAC != "new-mac" {
		t.Fatalf("expected rotated verifier MAC 'new-mac', got %q", found.CalendarFeedVerifierMAC)
	}
}

// TestClearCalendarFeedTokenRevokes proves the revoke path NULLs every feed
// column so the selector stops resolving (the feed URL would 404). The columns end
// empty on reload and the lookup is not-found. Leaving the MAC behind would strand
// an authenticator for a token the row no longer names.
func TestClearCalendarFeedTokenRevokes(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-revoke@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "REVOKESELECT16XX",
		VerifierHash: "hash-to-revoke",
		VerifierMAC:  "mac-to-revoke",
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if err := repo.ClearCalendarFeedToken(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearCalendarFeedToken: %v", err)
	}

	got := reloadUserForCalendarFeed(t, repo, user.ID)
	if got.CalendarFeedSelector != "" || got.CalendarFeedVerifierHash != "" || got.CalendarFeedVerifierMAC != "" {
		t.Fatalf("expected feed columns cleared after revoke, got selector=%q hash=%q mac=%q",
			got.CalendarFeedSelector, got.CalendarFeedVerifierHash, got.CalendarFeedVerifierMAC)
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
// feed: every feed column is NULLed so a previously-issued feed URL 404s against
// the freshly emptied account (its selector no longer resolves).
func TestClearAllDataResetsCalendarFeedColumns(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-clear@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "CLEARSELECT16XXX",
		VerifierHash: "hash-to-clear",
		VerifierMAC:  "mac-to-clear",
	}); err != nil {
		t.Fatalf("seed token: %v", err)
	}

	if err := repo.ClearAllDataAndResetSettings(context.Background(), user.ID); err != nil {
		t.Fatalf("ClearAllDataAndResetSettings: %v", err)
	}

	got := reloadUserForCalendarFeed(t, repo, user.ID)
	if got.CalendarFeedSelector != "" || got.CalendarFeedVerifierHash != "" || got.CalendarFeedVerifierMAC != "" {
		t.Fatalf("expected feed columns cleared after clear-data, got selector=%q hash=%q mac=%q",
			got.CalendarFeedSelector, got.CalendarFeedVerifierHash, got.CalendarFeedVerifierMAC)
	}
	if _, ok, err := repo.FindByCalendarFeedSelector(context.Background(), "CLEARSELECT16XXX"); err != nil || ok {
		t.Fatalf("expected cleared selector to be not-found after clear-data (ok=%v err=%v)", ok, err)
	}
}

// TestCalendarFeedTokenHashAtRestInDBRow proves NEITHER persisted verifier column
// holds the shown-once verifier plaintext: the plaintext substring must be absent
// from both, and the stored triple must verify the presented token. This is the
// end-to-end hash-at-rest check at the persistence boundary, using the real
// service generator to produce the storables.
func TestCalendarFeedTokenHashAtRestInDBRow(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-hashrest@example.com")

	fullToken, columns, err := services.GenerateCalendarFeedToken([]byte(calendarFeedRepoTestSecretKey))
	if err != nil {
		t.Fatalf("GenerateCalendarFeedToken: %v", err)
	}
	_, verifier, ok := services.SplitCalendarFeedToken(fullToken)
	if !ok {
		t.Fatal("expected generated token to split")
	}

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, columns); err != nil {
		t.Fatalf("SaveCalendarFeedToken: %v", err)
	}

	got := reloadUserForCalendarFeed(t, repo, user.ID)
	// Neither stored verifier column may be, or contain, the secret verifier
	// plaintext, and neither may contain the full shown-once token.
	for name, stored := range map[string]string{"hash": got.CalendarFeedVerifierHash, "mac": got.CalendarFeedVerifierMAC} {
		if stored == "" {
			t.Fatalf("expected the stored verifier %s to be persisted", name)
		}
		if stored == verifier {
			t.Fatalf("stored verifier %s equals the verifier plaintext", name)
		}
		if strings.Contains(stored, verifier) {
			t.Fatalf("stored verifier %s %q contains the verifier plaintext", name, stored)
		}
		if strings.Contains(stored, fullToken) {
			t.Fatalf("stored verifier %s contains the full token", name)
		}
	}
	// The presented full token verifies against the stored storables end to end.
	stored := models.CalendarFeedTokenColumns{
		Selector:     got.CalendarFeedSelector,
		VerifierHash: got.CalendarFeedVerifierHash,
		VerifierMAC:  got.CalendarFeedVerifierMAC,
	}
	if !services.VerifyCalendarFeedToken([]byte(calendarFeedRepoTestSecretKey), fullToken, stored) {
		t.Fatal("expected the full token to verify against the persisted storables")
	}
}

// TestBackfillCalendarFeedVerifierMACWritesOnlyIntoAnEmptyColumn covers the lazy
// migration write for a row stored before migration 032: the MAC lands, and a
// second attempt with a DIFFERENT value is refused because the column is no longer
// empty. The one-way guard is what keeps this path from ever "repairing" a MAC
// that mismatches because SECRET_KEY was rotated.
func TestBackfillCalendarFeedVerifierMACWritesOnlyIntoAnEmptyColumn(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-backfill@example.com")

	// A pre-032 row: selector + bcrypt hash, no MAC.
	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "LEGACYSELECT16XX",
		VerifierHash: "legacy-hash",
	}); err != nil {
		t.Fatalf("seed pre-032 token: %v", err)
	}

	if err := repo.BackfillCalendarFeedVerifierMAC(context.Background(), user.ID, "LEGACYSELECT16XX", "fresh-mac"); err != nil {
		t.Fatalf("BackfillCalendarFeedVerifierMAC: %v", err)
	}
	got := reloadUserForCalendarFeed(t, repo, user.ID)
	if got.CalendarFeedVerifierMAC != "fresh-mac" {
		t.Fatalf("expected the MAC backfilled into the empty column, got %q", got.CalendarFeedVerifierMAC)
	}
	if got.CalendarFeedVerifierHash != "legacy-hash" || got.CalendarFeedSelector != "LEGACYSELECT16XX" {
		t.Fatalf("the backfill must touch only the MAC column, got selector=%q hash=%q",
			got.CalendarFeedSelector, got.CalendarFeedVerifierHash)
	}

	// Idempotent and one-way: an existing MAC is never overwritten.
	if err := repo.BackfillCalendarFeedVerifierMAC(context.Background(), user.ID, "LEGACYSELECT16XX", "second-mac"); err != nil {
		t.Fatalf("second BackfillCalendarFeedVerifierMAC: %v", err)
	}
	again := reloadUserForCalendarFeed(t, repo, user.ID)
	if again.CalendarFeedVerifierMAC != "fresh-mac" {
		t.Fatalf("an existing MAC must never be overwritten, got %q", again.CalendarFeedVerifierMAC)
	}
}

// TestBackfillCalendarFeedVerifierMACRefusesAfterRotation is the race the CAS
// predicate exists for: the feed rotates between the read that verified the old
// token and the backfill write. Without the selector predicate the old token's MAC
// would be paired with the new token's hash, and the fresh subscribe URL would
// stop verifying until the owner rotated again.
func TestBackfillCalendarFeedVerifierMACRefusesAfterRotation(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	user := createUserForTimezoneTest(t, repo, "feed-backfill-race@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "OLDSELECTOR16XXX",
		VerifierHash: "old-hash",
	}); err != nil {
		t.Fatalf("seed pre-032 token: %v", err)
	}
	// The owner rotates: a fresh triple replaces the row the request had read.
	if err := repo.SaveCalendarFeedToken(context.Background(), user.ID, models.CalendarFeedTokenColumns{
		Selector:     "NEWSELECTOR16XXX",
		VerifierHash: "new-hash",
		VerifierMAC:  "new-mac",
	}); err != nil {
		t.Fatalf("rotate token: %v", err)
	}

	// The in-flight backfill still carries the OLD selector — it must not land.
	if err := repo.BackfillCalendarFeedVerifierMAC(context.Background(), user.ID, "OLDSELECTOR16XXX", "stale-mac"); err != nil {
		t.Fatalf("a zero-row CAS outcome is expected, not an error: %v", err)
	}
	got := reloadUserForCalendarFeed(t, repo, user.ID)
	if got.CalendarFeedVerifierMAC != "new-mac" {
		t.Fatalf("a backfill for a rotated-away selector must not land, got mac=%q", got.CalendarFeedVerifierMAC)
	}
}

// TestBackfillCalendarFeedVerifierMACScopedToUser proves the write cannot reach
// another owner's row even when that row is the one bearing the selector — the
// household multi-owner isolation boundary, on the one write the read path
// performs.
func TestBackfillCalendarFeedVerifierMACScopedToUser(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	owner := createUserForTimezoneTest(t, repo, "feed-backfill-owner@example.com")
	other := createUserForTimezoneTest(t, repo, "feed-backfill-other@example.com")

	if err := repo.SaveCalendarFeedToken(context.Background(), other.ID, models.CalendarFeedTokenColumns{
		Selector:     "OTHERSELECT16XXX",
		VerifierHash: "other-hash",
	}); err != nil {
		t.Fatalf("seed other owner token: %v", err)
	}

	// The other owner's selector, but this owner's id: nothing may change.
	if err := repo.BackfillCalendarFeedVerifierMAC(context.Background(), owner.ID, "OTHERSELECT16XXX", "cross-owner-mac"); err != nil {
		t.Fatalf("a zero-row CAS outcome is expected, not an error: %v", err)
	}
	gotOther := reloadUserForCalendarFeed(t, repo, other.ID)
	if gotOther.CalendarFeedVerifierMAC != "" {
		t.Fatalf("the backfill must never write into another owner's row, got mac=%q", gotOther.CalendarFeedVerifierMAC)
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
