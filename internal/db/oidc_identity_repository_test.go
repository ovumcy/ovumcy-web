package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

func TestOIDCIdentityRepositoryUsesCanonicalTableName(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "oidc-identities.db"))

	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at, local_auth_enabled) VALUES (?, ?, ?, CURRENT_TIMESTAMP, 1)`,
		"oidc-owner@example.com",
		"hash",
		"owner",
	).Error; err != nil {
		t.Fatalf("insert owner user: %v", err)
	}

	repository := NewOIDCIdentityRepository(database)
	createdAt := time.Now().UTC()
	identity := models.OIDCIdentity{
		UserID:    1,
		Issuer:    "https://id.example.com",
		Subject:   "subject-1",
		CreatedAt: createdAt,
	}

	if err := repository.Create(context.Background(), &identity); err != nil {
		t.Fatalf("create oidc identity: %v", err)
	}
	if identity.ID == 0 {
		t.Fatal("expected oidc identity ID to be assigned")
	}

	stored, found, err := repository.FindByIssuerSubject(context.Background(), identity.Issuer, identity.Subject)
	if err != nil {
		t.Fatalf("find oidc identity: %v", err)
	}
	if !found {
		t.Fatal("expected oidc identity lookup to find stored record")
	}
	if stored.UserID != identity.UserID {
		t.Fatalf("expected oidc identity user_id %d, got %d", identity.UserID, stored.UserID)
	}
}

func seedOIDCRepositoryOwners(t *testing.T) (*OIDCIdentityRepository, func(uint) int) {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "oidc-identities-revoke.db"))
	for _, email := range []string{"oidc-one@example.com", "oidc-two@example.com"} {
		if err := database.Exec(
			`INSERT INTO users (email, password_hash, role, created_at, local_auth_enabled, auth_session_version) VALUES (?, ?, ?, CURRENT_TIMESTAMP, 1, 1)`,
			email, "hash", "owner",
		).Error; err != nil {
			t.Fatalf("insert owner user: %v", err)
		}
	}
	version := func(userID uint) int {
		var user models.User
		if err := database.First(&user, userID).Error; err != nil {
			t.Fatalf("load user %d: %v", userID, err)
		}
		return user.AuthSessionVersion
	}
	return NewOIDCIdentityRepository(database), version
}

// A link to an existing account and an unlink both bump that account's
// session version in the same write, and only that account's.
func TestOIDCIdentityRepositoryLinkAndUnlinkRevokeTheOwnersSessions(t *testing.T) {
	repository, version := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	identity := models.OIDCIdentity{UserID: 1, Issuer: "https://id.example.com", Subject: "linked", CreatedAt: time.Now().UTC()}
	if err := repository.CreateAndRevokeSessions(ctx, &identity, 1); err != nil {
		t.Fatalf("CreateAndRevokeSessions: %v", err)
	}
	if got := version(1); got != 2 {
		t.Fatalf("expected the link to bump owner 1 to version 2, got %d", got)
	}

	// Another owner's id: nothing deleted, nobody's version moves.
	deleted, err := repository.DeleteForUserAndRevokeSessions(ctx, 2, identity.ID, version(2), true)
	if err != nil || deleted {
		t.Fatalf("expected a foreign-owner unlink to delete nothing, got deleted=%v err=%v", deleted, err)
	}
	if version(1) != 2 || version(2) != 1 {
		t.Fatalf("expected no version change on a refused unlink, got %d/%d", version(1), version(2))
	}
	if _, found, _ := repository.FindByIssuerSubject(ctx, identity.Issuer, identity.Subject); !found {
		t.Fatal("expected the identity to survive another owner's unlink")
	}

	deleted, err = repository.DeleteForUserAndRevokeSessions(ctx, 1, identity.ID, version(1), true)
	if err != nil || !deleted {
		t.Fatalf("expected the owner's unlink to delete, got deleted=%v err=%v", deleted, err)
	}
	if got := version(1); got != 3 {
		t.Fatalf("expected the unlink to bump owner 1 to version 3, got %d", got)
	}
	if _, found, _ := repository.FindByIssuerSubject(ctx, identity.Issuer, identity.Subject); found {
		t.Fatal("expected the identity to be gone after unlink")
	}
}

// The "a sign-in method remains" rule is enforced inside the delete
// transaction: removing the last identity of an account with no usable local
// password rolls back — row kept, no version bump — and says why.
func TestOIDCIdentityRepositoryRefusesToDeleteTheLastSignInMethod(t *testing.T) {
	repository, version := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	first := models.OIDCIdentity{UserID: 1, Issuer: "https://id.example.com", Subject: "first", CreatedAt: time.Now().UTC()}
	second := models.OIDCIdentity{UserID: 1, Issuer: "https://id.example.com", Subject: "second", CreatedAt: time.Now().UTC()}
	for _, identity := range []*models.OIDCIdentity{&first, &second} {
		if err := repository.Create(ctx, identity); err != nil {
			t.Fatalf("Create: %v", err)
		}
	}

	// One of two may go even with local sign-in closed.
	deleted, err := repository.DeleteForUserAndRevokeSessions(ctx, 1, first.ID, version(1), false)
	if err != nil || !deleted {
		t.Fatalf("expected one of two identities to be removable, got deleted=%v err=%v", deleted, err)
	}
	if got := version(1); got != 2 {
		t.Fatalf("expected the unlink to bump owner 1 to version 2, got %d", got)
	}

	// The last one may not while local sign-in is closed, although the account
	// holds a password.
	deleted, err = repository.DeleteForUserAndRevokeSessions(ctx, 1, second.ID, version(1), false)
	if !errors.Is(err, models.ErrOIDCUnlinkLastSignIn) || deleted {
		t.Fatalf("expected ErrOIDCUnlinkLastSignIn for the last identity, got deleted=%v err=%v", deleted, err)
	}
	if got := version(1); got != 2 {
		t.Fatalf("expected the refused unlink to roll back its bump, got version %d", got)
	}
	if _, found, _ := repository.FindByIssuerSubject(ctx, second.Issuer, second.Subject); !found {
		t.Fatal("expected the last identity to survive the refused unlink")
	}

	// With local sign-in open and a stored password, the account keeps a way in.
	deleted, err = repository.DeleteForUserAndRevokeSessions(ctx, 1, second.ID, version(1), true)
	if err != nil || !deleted {
		t.Fatalf("expected the last identity to be removable with a usable password, got deleted=%v err=%v", deleted, err)
	}
}

// Two concurrent unlinks of an account's two identities, with no local
// sign-in to fall back on: each pre-read sees the other identity, so no
// caller-side read can keep one. Both were verified against the same session
// version, so the first commit moves it and the second is refused by the
// version compare-and-set before it counts anything — that refusal, every
// round, not whichever one the schedule happens to produce. Retried from the
// version the winner left, which the compare-and-set no longer refuses, the
// loser meets the last-sign-in check inside the delete transaction instead.
// Exactly one delete may win, every round.
func TestOIDCIdentityRepositoryConcurrentUnlinksKeepOneSignInMethod(t *testing.T) {
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "oidc-unlink-race.db")})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	if err := database.Exec(
		`INSERT INTO users (email, password_hash, role, created_at, local_auth_enabled, auth_session_version) VALUES (?, ?, ?, CURRENT_TIMESTAMP, 0, 1)`,
		"oidc-race@example.com", "", "owner",
	).Error; err != nil {
		t.Fatalf("insert owner user: %v", err)
	}
	var userID uint
	if err := database.Raw(`SELECT id FROM users WHERE email = ?`, "oidc-race@example.com").Scan(&userID).Error; err != nil || userID == 0 {
		t.Fatalf("resolve owner id: %v (id %d)", err, userID)
	}
	repository := NewOIDCIdentityRepository(database)
	ctx := context.Background()

	for round := range 20 {
		pair := make([]models.OIDCIdentity, 2)
		for index := range pair {
			pair[index] = models.OIDCIdentity{UserID: userID, Issuer: "https://id.example.com", Subject: fmt.Sprintf("r%d-%d", round, index), CreatedAt: time.Now().UTC()}
			if err := repository.Create(ctx, &pair[index]); err != nil {
				t.Fatalf("round %d: Create: %v", round, err)
			}
		}

		var from int
		if err := database.Raw(`SELECT auth_session_version FROM users WHERE id = ?`, userID).Scan(&from).Error; err != nil {
			t.Fatalf("round %d: load version: %v", round, err)
		}
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]error, len(pair))
		deleted := make([]bool, len(pair))
		for index := range pair {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				deleted[index], results[index] = repository.DeleteForUserAndRevokeSessions(ctx, userID, pair[index].ID, from, false)
			}(index)
		}
		close(start)
		wg.Wait()

		wins, loser := 0, -1
		for index := range pair {
			switch {
			case results[index] == nil && deleted[index]:
				wins++
			case errors.Is(results[index], models.ErrAuthSessionVersionChanged) && !deleted[index]:
				loser = index
			default:
				t.Fatalf("round %d: expected a win or a session-version refusal, got deleted=%v err=%v", round, deleted[index], results[index])
			}
		}
		if wins != 1 || loser < 0 {
			t.Fatalf("round %d: expected one win and one session-version refusal, got %d wins (loser %d)", round, wins, loser)
		}
		var moved int
		if err := database.Raw(`SELECT auth_session_version FROM users WHERE id = ?`, userID).Scan(&moved).Error; err != nil {
			t.Fatalf("round %d: reload version: %v", round, err)
		}
		if moved != from+1 {
			t.Fatalf("round %d: expected the winner alone to move the version to %d, got %d", round, from+1, moved)
		}

		// A removal the version no longer refuses still may not take the last
		// identity: the check inside the delete transaction refuses it and
		// rolls its bump back.
		retried, err := repository.DeleteForUserAndRevokeSessions(ctx, userID, pair[loser].ID, moved, false)
		if !errors.Is(err, models.ErrOIDCUnlinkLastSignIn) || retried {
			t.Fatalf("round %d: expected the retry from version %d to meet the last-sign-in check, got deleted=%v err=%v", round, moved, retried, err)
		}
		remaining, err := repository.ListByUser(ctx, userID)
		if err != nil || len(remaining) != 1 || remaining[0].ID != pair[loser].ID {
			t.Fatalf("round %d: expected only the loser's identity %d left, got %v (err %v)", round, pair[loser].ID, remaining, err)
		}
		var after int
		if err := database.Raw(`SELECT auth_session_version FROM users WHERE id = ?`, userID).Scan(&after).Error; err != nil || after != moved {
			t.Fatalf("round %d: expected the refused retry to roll its bump back to %d, got %d (err %v)", round, moved, after, err)
		}
		// Reset for the next round: drop the survivor directly.
		if err := database.Where("user_id = ?", userID).Delete(&models.OIDCIdentity{}).Error; err != nil {
			t.Fatalf("round %d: reset: %v", round, err)
		}
	}
}

// A link naming an account that does not exist rolls back: no identity row
// may outlive the bump that was meant to accompany it.
func TestOIDCIdentityRepositoryRevokingLinkRollsBackForAMissingOwner(t *testing.T) {
	repository, _ := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	identity := models.OIDCIdentity{UserID: 99, Issuer: "https://id.example.com", Subject: "orphan", CreatedAt: time.Now().UTC()}
	if err := repository.CreateAndRevokeSessions(ctx, &identity, 1); err == nil {
		t.Fatal("expected a link to a missing account to fail")
	}
	if _, found, _ := repository.FindByIssuerSubject(ctx, identity.Issuer, identity.Subject); found {
		t.Fatal("expected no identity row after the rolled-back link")
	}
}

// A nil identity or a zero UserID names no owner to bump: CreateAndRevokeSessions
// refuses before opening a transaction, rather than writing an identity row no
// session-version bump accompanies.
func TestOIDCIdentityRepositoryCreateAndRevokeSessionsRequiresAnOwner(t *testing.T) {
	repository, _ := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	if err := repository.CreateAndRevokeSessions(ctx, nil, 1); !errors.Is(err, errOIDCIdentityOwnerRequired) {
		t.Fatalf("expected errOIDCIdentityOwnerRequired for a nil identity, got %v", err)
	}
	identity := models.OIDCIdentity{Issuer: "https://id.example.com", Subject: "no-owner"}
	if err := repository.CreateAndRevokeSessions(ctx, &identity, 1); !errors.Is(err, errOIDCIdentityOwnerRequired) {
		t.Fatalf("expected errOIDCIdentityOwnerRequired for a zero UserID, got %v", err)
	}
}

// A nil identity or a zero UserID names no owner: Create refuses it rather
// than writing a row that no owner-scoped read and no account erasure can
// ever reach.
func TestOIDCIdentityRepositoryCreateRefusesZeroOwner(t *testing.T) {
	repository, _ := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	if err := repository.Create(ctx, nil); !errors.Is(err, errOIDCIdentityOwnerRequired) {
		t.Fatalf("expected errOIDCIdentityOwnerRequired for a nil identity, got %v", err)
	}
	identity := models.OIDCIdentity{Issuer: "https://id.example.com", Subject: "no-owner-create"}
	if err := repository.Create(ctx, &identity); !errors.Is(err, errOIDCIdentityOwnerRequired) {
		t.Fatalf("expected errOIDCIdentityOwnerRequired for a zero UserID, got %v", err)
	}
	if _, found, _ := repository.FindByIssuerSubject(ctx, identity.Issuer, identity.Subject); found {
		t.Fatal("expected no identity row after the refused create")
	}
}

// A zero userID lists nothing rather than running the query.
func TestOIDCIdentityRepositoryListByUserWithZeroIDListsNothing(t *testing.T) {
	repository, _ := seedOIDCRepositoryOwners(t)
	identities, err := repository.ListByUser(context.Background(), 0)
	if err != nil || identities != nil {
		t.Fatalf("expected (nil, nil) for a zero user id, got %v, %v", identities, err)
	}
}

// DeleteForUserAndRevokeSessions refuses a zero user or identity id before
// opening a transaction, and an unlink naming a userID with no user row maps
// the owner-bump's errOIDCIdentityOwnerRequired to the same not-deleted
// outcome a foreign-owner unlink reports — the two are indistinguishable to
// the caller by design (models.go: another owner's id reads as not-found).
func TestOIDCIdentityRepositoryDeleteRefusesAZeroIDOrAMissingOwner(t *testing.T) {
	repository, _ := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	if deleted, err := repository.DeleteForUserAndRevokeSessions(ctx, 0, 5, 1, true); err != nil || deleted {
		t.Fatalf("expected a zero user id to delete nothing, got deleted=%v err=%v", deleted, err)
	}
	if deleted, err := repository.DeleteForUserAndRevokeSessions(ctx, 1, 0, 1, true); err != nil || deleted {
		t.Fatalf("expected a zero identity id to delete nothing, got deleted=%v err=%v", deleted, err)
	}
	if deleted, err := repository.DeleteForUserAndRevokeSessions(ctx, 99, 5, 1, true); err != nil || deleted {
		t.Fatalf("expected an unlink naming a missing owner to delete nothing, got deleted=%v err=%v", deleted, err)
	}
}

// A blank issuer or subject names no identity, even when a row was stored
// with that blank value.
func TestOIDCIdentityRepositoryBlankKeyFindsNothing(t *testing.T) {
	repository, _ := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	blank := models.OIDCIdentity{UserID: 1, Issuer: "https://id.example.com", Subject: "", CreatedAt: time.Now().UTC()}
	if err := repository.Create(ctx, &blank); err != nil {
		t.Fatalf("seed a legacy blank-subject row: %v", err)
	}
	for _, key := range [][2]string{{"https://id.example.com", ""}, {"https://id.example.com", "  "}, {"", "sub"}} {
		if _, found, err := repository.FindByIssuerSubject(ctx, key[0], key[1]); err != nil || found {
			t.Fatalf("key %q: expected not-found, got found=%v err=%v", key, found, err)
		}
	}
}

// TestOIDCIdentityRepositoryTouchLastUsedScopedToOwner proves TouchLastUsed
// combines identityID with the caller's own userID in the query, rather than
// trusting identityID alone: owner two presenting owner one's identity id
// must be a clean no-op, never a touch of the other owner's row. Without the
// user_id term in the WHERE clause, a stale or foreign identityID — read once
// and reused after the identity moved, or handed to the wrong owner's call —
// would update a row that id belongs to, regardless of who is asking.
func TestOIDCIdentityRepositoryTouchLastUsedScopedToOwner(t *testing.T) {
	repository, _ := seedOIDCRepositoryOwners(t)
	ctx := context.Background()

	ownerOneIdentity := models.OIDCIdentity{UserID: 1, Issuer: "https://id.example.com", Subject: "owner-one", CreatedAt: time.Now().UTC()}
	if err := repository.Create(ctx, &ownerOneIdentity); err != nil {
		t.Fatalf("create owner one's identity: %v", err)
	}

	// Owner two (userID 2) presents owner one's identity id. The combined
	// (id, user_id) predicate matches zero rows, so this must be a no-op: no
	// error, and owner one's last_used_at stays untouched.
	if err := repository.TouchLastUsed(ctx, ownerOneIdentity.ID, 2, time.Now().UTC()); err != nil {
		t.Fatalf("cross-owner TouchLastUsed should be a no-op, got %v", err)
	}
	reloaded, found, err := repository.FindByIssuerSubject(ctx, ownerOneIdentity.Issuer, ownerOneIdentity.Subject)
	if err != nil || !found {
		t.Fatalf("reload owner one's identity: found=%v err=%v", found, err)
	}
	if reloaded.LastUsedAt != nil {
		t.Fatalf("cross-owner TouchLastUsed touched owner one's row: last_used_at=%v, want nil", reloaded.LastUsedAt)
	}

	// The legitimate owner touching their own identity still works.
	touchedAt := time.Now().UTC()
	if err := repository.TouchLastUsed(ctx, ownerOneIdentity.ID, 1, touchedAt); err != nil {
		t.Fatalf("owner-scoped TouchLastUsed: %v", err)
	}
	reloaded, found, err = repository.FindByIssuerSubject(ctx, ownerOneIdentity.Issuer, ownerOneIdentity.Subject)
	if err != nil || !found || reloaded.LastUsedAt == nil {
		t.Fatalf("expected owner-scoped touch to persist, found=%v err=%v last_used_at=%v", found, err, reloaded.LastUsedAt)
	}
}
