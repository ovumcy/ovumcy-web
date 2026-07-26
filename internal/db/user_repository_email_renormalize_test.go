package db

import (
	"context"
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
