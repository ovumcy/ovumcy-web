package db

import (
	"context"
	"errors"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// TestAuthEmailRenormalizerAgainstRealRepositories drives the boot-time email
// repair end to end on a migrated SQLite database: a row stored by the old
// normalizer with a display-name-decorated email is reduced to its bare
// address and becomes reachable by normalized lookup again; a decorated row
// whose bare address is already another account's stays untouched (the
// audit's two-accounts-one-mailbox case); a case-only row is lowered without
// tripping over itself in the uniqueness check; auth_session_version never
// moves; the marker makes a second pass a no-op.
func TestAuthEmailRenormalizerAgainstRealRepositories(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	appState := NewAppStateRepository(repo.database)
	ctx := context.Background()

	keep := createUserForTimezoneTest(t, repo, "keep@example.com")
	dupOwner := createUserForTimezoneTest(t, repo, "dup@example.com")
	decorated := createUserForTimezoneTest(t, repo, "decorated-placeholder@example.com")
	collider := createUserForTimezoneTest(t, repo, "collider-placeholder@example.com")
	mixedCase := createUserForTimezoneTest(t, repo, "mixed-placeholder@example.com")

	// Corrupt the rows the way the pre-strict normalizer persisted them: the
	// whole decorated input, verbatim. Raw updates on purpose — the current
	// code paths can no longer produce these values.
	corrupt := func(userID uint, email string) {
		t.Helper()
		if err := repo.database.Model(&models.User{}).Where("id = ?", userID).
			Update("email", email).Error; err != nil {
			t.Fatalf("corrupt email for %d: %v", userID, err)
		}
	}
	corrupt(decorated.ID, "jane doe <solo@example.com>")
	corrupt(collider.ID, "second account <dup@example.com>")
	corrupt(mixedCase.ID, "MiXeD@Example.Com")

	before := reloadUserForCalendarFeed(t, repo, mixedCase.ID)

	outcome, err := services.NewAuthEmailRenormalizer(appState, repo).Run(ctx)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Renormalized != 2 || outcome.SkippedConflicts != 1 || outcome.SkippedUnrenormalizable != 0 {
		t.Fatalf("unexpected outcome %+v", outcome)
	}

	// The decorated solo row is reachable by its bare address again.
	found, err := repo.FindByNormalizedEmail(ctx, "solo@example.com")
	if err != nil || found.ID != decorated.ID {
		t.Fatalf("expected solo@example.com to resolve user %d, got %d (err=%v)", decorated.ID, found.ID, err)
	}

	// The collider stays as stored: its bare address belongs to dupOwner.
	colliderRow := reloadUserForCalendarFeed(t, repo, collider.ID)
	if colliderRow.Email != "second account <dup@example.com>" {
		t.Fatalf("collider row must stay untouched, got %q", colliderRow.Email)
	}
	owner, err := repo.FindByNormalizedEmail(ctx, "dup@example.com")
	if err != nil || owner.ID != dupOwner.ID {
		t.Fatalf("dup@example.com must still resolve its original owner %d, got %d (err=%v)", dupOwner.ID, owner.ID, err)
	}

	// Case-only row lowered; identity untouched otherwise.
	mixedRow := reloadUserForCalendarFeed(t, repo, mixedCase.ID)
	if mixedRow.Email != "mixed@example.com" {
		t.Fatalf("case-only row must be lowered, got %q", mixedRow.Email)
	}
	if mixedRow.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("renormalization must not bump auth_session_version: before=%d after=%d", before.AuthSessionVersion, mixedRow.AuthSessionVersion)
	}

	// Untouched clean row stays byte-identical.
	keepRow := reloadUserForCalendarFeed(t, repo, keep.ID)
	if keepRow.Email != "keep@example.com" {
		t.Fatalf("clean row must stay untouched, got %q", keepRow.Email)
	}

	// Marker written; second pass is a no-op.
	if _, done, err := appState.Get(ctx, models.AppStateKeyAuthEmailRenormalizeV1); err != nil || !done {
		t.Fatalf("expected the done-marker after the pass (done=%v, err=%v)", done, err)
	}
	second, err := services.NewAuthEmailRenormalizer(appState, repo).Run(ctx)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	if !second.AlreadyDone {
		t.Fatalf("second pass must report AlreadyDone, got %+v", second)
	}
}

// TestFindByIDOptionalSeparatesAMissingRowFromAFailedRead pins the two
// not-found answers apart: an id nobody carries is a legitimate "no", while a
// storage failure must reach the caller as an error — reported as "no account
// carries this id", it would send an operator hunting for a row that is there.
func TestFindByIDOptionalSeparatesAMissingRowFromAFailedRead(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()

	present := createUserForTimezoneTest(t, repo, "present@example.com")
	if user, found, err := repo.FindByIDOptional(ctx, present.ID); err != nil || !found || user.ID != present.ID {
		t.Fatalf("expected the seeded row (found=%v, err=%v)", found, err)
	}
	if _, found, err := repo.FindByIDOptional(ctx, present.ID+4242); err != nil || found {
		t.Fatalf("an absent id is a plain no (found=%v, err=%v)", found, err)
	}

	sqlDB, err := repo.database.DB()
	if err != nil {
		t.Fatalf("database.DB(): %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
	if _, found, err := repo.FindByIDOptional(ctx, present.ID); err == nil || found {
		t.Fatalf("a failed read must surface as an error, got found=%v err=%v", found, err)
	}
}

// TestFindAllByNormalizedEmailSurfacesAFailedRead mirrors
// TestFindByIDOptionalSeparatesAMissingRowFromAFailedRead for
// FindAllByNormalizedEmail: a normalized address nobody holds is a legitimate
// empty slice, while a storage failure must reach the caller as an error —
// ForceResetPasswordByEmail's ErrAuthUserNotFound treats a nil/empty slice as
// "no such account", so a query failure reported the same way would send an
// operator chasing an address that is actually just unreachable right now.
func TestFindAllByNormalizedEmailSurfacesAFailedRead(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()

	present := createUserForTimezoneTest(t, repo, "present-all@example.com")
	if users, err := repo.FindAllByNormalizedEmail(ctx, "present-all@example.com"); err != nil || len(users) != 1 || users[0].ID != present.ID {
		t.Fatalf("expected the seeded row (users=%v, err=%v)", users, err)
	}
	if users, err := repo.FindAllByNormalizedEmail(ctx, "absent-all@example.com"); err != nil || len(users) != 0 {
		t.Fatalf("an absent address is a plain empty slice (users=%v, err=%v)", users, err)
	}

	sqlDB, err := repo.database.DB()
	if err != nil {
		t.Fatalf("database.DB(): %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
	if users, err := repo.FindAllByNormalizedEmail(ctx, "present-all@example.com"); err == nil || users != nil {
		t.Fatalf("a failed read must surface as an error with a nil slice, got users=%v err=%v", users, err)
	}
}

// TestSetUserEmailByIDAndRevokeSessionsRepairsALeftoverRow covers the operator
// repair the pass above cannot perform: the row it left standing is re-homed by
// id. Three properties, each on its own row state — the write bumps
// auth_session_version (the address IS the login identity, unlike the pure
// renormalization above, which must not bump); a stale from-address matches
// zero rows instead of clobbering what stands there now; and the unique index,
// not the caller's pre-check, is what refuses an address another account
// already answers to.
func TestSetUserEmailByIDAndRevokeSessionsRepairsALeftoverRow(t *testing.T) {
	repo := openCalendarFeedRepoForTest(t)
	ctx := context.Background()

	winner := createUserForTimezoneTest(t, repo, "dup@example.com")
	leftover := createUserForTimezoneTest(t, repo, "leftover-placeholder@example.com")

	const stored = "second account <dup@example.com>"
	if err := repo.database.Model(&models.User{}).Where("id = ?", leftover.ID).
		Update("email", stored).Error; err != nil {
		t.Fatalf("store legacy email: %v", err)
	}
	if err := repo.SaveCalendarFeedToken(ctx, leftover.ID, models.CalendarFeedTokenColumns{
		Selector:     "SELECTOR16CHARSXX",
		VerifierHash: "verifier-hash",
		VerifierMAC:  "verifier-mac",
	}); err != nil {
		t.Fatalf("arm calendar feed: %v", err)
	}
	before := reloadUserForCalendarFeed(t, repo, leftover.ID)

	// A stale from-address: the row no longer carries it, so nothing moves.
	changed, err := repo.SetUserEmailByIDAndRevokeSessions(ctx, leftover.ID, "leftover-placeholder@example.com", "second@example.com")
	if err != nil {
		t.Fatalf("stale CAS: %v", err)
	}
	if changed {
		t.Fatalf("a stale from-address must match zero rows")
	}
	if stale := reloadUserForCalendarFeed(t, repo, leftover.ID); stale.Email != stored || stale.AuthSessionVersion != before.AuthSessionVersion {
		t.Fatalf("a refused CAS must change nothing: email=%q version=%d", stale.Email, stale.AuthSessionVersion)
	}

	// An address another account answers to is refused by the unique index.
	if _, err := repo.SetUserEmailByIDAndRevokeSessions(ctx, leftover.ID, stored, "dup@example.com"); err == nil {
		t.Fatalf("expected the unique index to refuse an address already in use")
	} else {
		var uniqueErr *UniqueConstraintError
		if !errors.As(err, &uniqueErr) {
			t.Fatalf("expected a UniqueConstraintError, got %T: %v", err, err)
		}
	}

	changed, err = repo.SetUserEmailByIDAndRevokeSessions(ctx, leftover.ID, stored, "second@example.com")
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !changed {
		t.Fatalf("expected the repair to move exactly one row")
	}

	after := reloadUserForCalendarFeed(t, repo, leftover.ID)
	if after.Email != "second@example.com" {
		t.Fatalf("expected the repaired address, got %q", after.Email)
	}
	if after.AuthSessionVersion != before.AuthSessionVersion+1 {
		t.Fatalf("re-homing must revoke sessions: before=%d after=%d", before.AuthSessionVersion, after.AuthSessionVersion)
	}
	// The feed is a capability of the account, not of the address: a repair is
	// not a compromise event, so it stays armed for the owner to rotate.
	if after.CalendarFeedSelector != before.CalendarFeedSelector || after.CalendarFeedVerifierHash != before.CalendarFeedVerifierHash {
		t.Fatalf("re-homing must leave the feed columns alone")
	}

	winnerRow := reloadUserForCalendarFeed(t, repo, winner.ID)
	if winnerRow.Email != "dup@example.com" || winnerRow.AuthSessionVersion != 1 {
		t.Fatalf("the other account must be untouched: email=%q version=%d", winnerRow.Email, winnerRow.AuthSessionVersion)
	}
}
