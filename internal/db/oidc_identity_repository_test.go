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
	if err := repository.CreateAndRevokeSessions(ctx, &identity); err != nil {
		t.Fatalf("CreateAndRevokeSessions: %v", err)
	}
	if got := version(1); got != 2 {
		t.Fatalf("expected the link to bump owner 1 to version 2, got %d", got)
	}

	// Another owner's id: nothing deleted, nobody's version moves.
	deleted, err := repository.DeleteForUserAndRevokeSessions(ctx, 2, identity.ID, true)
	if err != nil || deleted {
		t.Fatalf("expected a foreign-owner unlink to delete nothing, got deleted=%v err=%v", deleted, err)
	}
	if version(1) != 2 || version(2) != 1 {
		t.Fatalf("expected no version change on a refused unlink, got %d/%d", version(1), version(2))
	}
	if _, found, _ := repository.FindByIssuerSubject(ctx, identity.Issuer, identity.Subject); !found {
		t.Fatal("expected the identity to survive another owner's unlink")
	}

	deleted, err = repository.DeleteForUserAndRevokeSessions(ctx, 1, identity.ID, true)
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
	deleted, err := repository.DeleteForUserAndRevokeSessions(ctx, 1, first.ID, false)
	if err != nil || !deleted {
		t.Fatalf("expected one of two identities to be removable, got deleted=%v err=%v", deleted, err)
	}
	if got := version(1); got != 2 {
		t.Fatalf("expected the unlink to bump owner 1 to version 2, got %d", got)
	}

	// The last one may not while local sign-in is closed, although the account
	// holds a password.
	deleted, err = repository.DeleteForUserAndRevokeSessions(ctx, 1, second.ID, false)
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
	deleted, err = repository.DeleteForUserAndRevokeSessions(ctx, 1, second.ID, true)
	if err != nil || !deleted {
		t.Fatalf("expected the last identity to be removable with a usable password, got deleted=%v err=%v", deleted, err)
	}
}

// Two concurrent unlinks of an account's two identities, with no local
// sign-in to fall back on: each pre-read sees the other identity, so only the
// in-transaction check can keep one. Exactly one delete may win, every round.
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

		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]error, len(pair))
		deleted := make([]bool, len(pair))
		for index := range pair {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				deleted[index], results[index] = repository.DeleteForUserAndRevokeSessions(ctx, userID, pair[index].ID, false)
			}(index)
		}
		close(start)
		wg.Wait()

		wins, refusals := 0, 0
		for index := range pair {
			switch {
			case results[index] == nil && deleted[index]:
				wins++
			case errors.Is(results[index], models.ErrOIDCUnlinkLastSignIn):
				refusals++
			default:
				t.Fatalf("round %d: unexpected outcome deleted=%v err=%v", round, deleted[index], results[index])
			}
		}
		if wins != 1 || refusals != 1 {
			t.Fatalf("round %d: expected one win and one refusal, got %d/%d", round, wins, refusals)
		}
		remaining, err := repository.ListByUser(ctx, userID)
		if err != nil || len(remaining) != 1 {
			t.Fatalf("round %d: expected one identity left, got %d (err %v)", round, len(remaining), err)
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
	if err := repository.CreateAndRevokeSessions(ctx, &identity); err == nil {
		t.Fatal("expected a link to a missing account to fail")
	}
	if _, found, _ := repository.FindByIssuerSubject(ctx, identity.Issuer, identity.Subject); found {
		t.Fatal("expected no identity row after the rolled-back link")
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
