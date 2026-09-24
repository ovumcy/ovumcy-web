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
	"gorm.io/gorm"
)

// A link revokes the account's sessions only from the version its caller
// verified factors against. Another revoking write committed in between must
// roll the link back, or the caller mints a session at the version that write
// produced and outlives it. Both engines run the same cases: the guarantee
// rests on the UPDATE's predicate being re-evaluated after a lock wait, which
// the two engines provide differently.
func TestOIDCIdentityRepositoryLinkRevokesOnlyFromTheVerifiedSessionVersion(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "oidc-link-cas.db")})
		if err != nil {
			t.Fatalf("open sqlite: %v", err)
		}
		t.Cleanup(func() {
			if sqlDB, err := database.DB(); err == nil {
				_ = sqlDB.Close()
			}
		})
		assertLinkRevokesOnlyFromTheVerifiedSessionVersion(t, database)
	})

	t.Run("postgres", func(t *testing.T) {
		assertLinkRevokesOnlyFromTheVerifiedSessionVersion(t, openPostgresForMigrationBootstrapTest(t, startPostgresTestConfig(t)))
	})
}

func assertLinkRevokesOnlyFromTheVerifiedSessionVersion(t *testing.T, database *gorm.DB) {
	t.Helper()
	repository := NewOIDCIdentityRepository(database)
	ctx := context.Background()

	seed := func(email string, version int) uint {
		t.Helper()
		if err := database.Exec(
			`INSERT INTO users (email, password_hash, role, created_at, local_auth_enabled, auth_session_version) VALUES (?, ?, ?, ?, ?, ?)`,
			email, "hash", "owner", time.Now().UTC(), true, version,
		).Error; err != nil {
			t.Fatalf("insert %s: %v", email, err)
		}
		var userID uint
		if err := database.Raw(`SELECT id FROM users WHERE email = ?`, email).Scan(&userID).Error; err != nil || userID == 0 {
			t.Fatalf("resolve %s: %v (id %d)", email, err, userID)
		}
		return userID
	}
	version := func(userID uint) int {
		t.Helper()
		var stored int
		if err := database.Raw(`SELECT auth_session_version FROM users WHERE id = ?`, userID).Scan(&stored).Error; err != nil {
			t.Fatalf("load version of %d: %v", userID, err)
		}
		return stored
	}
	link := func(userID uint, subject string, expected int) error {
		identity := models.OIDCIdentity{UserID: userID, Issuer: "https://id.example.com", Subject: subject, CreatedAt: time.Now().UTC()}
		return repository.CreateAndRevokeSessions(ctx, &identity, expected)
	}
	linked := func(subject string) bool {
		t.Helper()
		_, found, err := repository.FindByIssuerSubject(ctx, "https://id.example.com", subject)
		if err != nil {
			t.Fatalf("FindByIssuerSubject(%s): %v", subject, err)
		}
		return found
	}

	cases := []struct {
		name        string
		stored      int
		expected    int
		wantErr     error
		wantVersion int
	}{
		{name: "verified version", stored: 3, expected: 3, wantVersion: 4},
		{name: "revoked since it was verified", stored: 4, expected: 3, wantErr: models.ErrAuthSessionVersionChanged, wantVersion: 4},
		{name: "legacy zero row read as version 1", stored: 0, expected: 1, wantVersion: 2},
		{name: "legacy zero expected on a legacy row", stored: 0, expected: 0, wantVersion: 2},
		{name: "legacy zero row revoked since it was read", stored: 0, expected: 2, wantErr: models.ErrAuthSessionVersionChanged, wantVersion: 0},
	}
	for index, tc := range cases {
		userID := seed(fmt.Sprintf("cas-%d@example.com", index), tc.stored)
		subject := fmt.Sprintf("cas-%d", index)
		err := link(userID, subject, tc.expected)
		if !errors.Is(err, tc.wantErr) {
			t.Fatalf("%s: expected error %v, got %v", tc.name, tc.wantErr, err)
		}
		if got := version(userID); got != tc.wantVersion {
			t.Fatalf("%s: expected stored version %d, got %d", tc.name, tc.wantVersion, got)
		}
		if linked(subject) != (tc.wantErr == nil) {
			t.Fatalf("%s: expected linked=%v", tc.name, tc.wantErr == nil)
		}
	}

	if err := link(99999, "cas-missing", 1); !errors.Is(err, errOIDCIdentityOwnerRequired) {
		t.Fatalf("expected errOIDCIdentityOwnerRequired for a missing account, got %v", err)
	}
	if linked("cas-missing") {
		t.Fatal("expected no identity row for a missing account")
	}

	// Concurrent links verified against the same version: the first commit
	// moves the version, so every other one must find it moved and roll back.
	racer := seed("cas-race@example.com", 1)
	const contenders = 3
	for round := range 10 {
		from := version(racer)
		var wg sync.WaitGroup
		start := make(chan struct{})
		results := make([]error, contenders)
		for index := range contenders {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				<-start
				results[index] = link(racer, fmt.Sprintf("race-%d-%d", round, index), from)
			}(index)
		}
		close(start)
		wg.Wait()

		wins := 0
		for index, err := range results {
			switch {
			case err == nil:
				wins++
			case errors.Is(err, models.ErrAuthSessionVersionChanged):
			default:
				t.Fatalf("round %d contender %d: unexpected error %v", round, index, err)
			}
		}
		if wins != 1 {
			t.Fatalf("round %d: expected exactly one link to win, got %d (%v)", round, wins, results)
		}
		if got := version(racer); got != from+1 {
			t.Fatalf("round %d: expected version %d, got %d", round, from+1, got)
		}
		identities, err := repository.ListByUser(ctx, racer)
		if err != nil || len(identities) != round+1 {
			t.Fatalf("round %d: expected %d identities, got %d (err %v)", round, round+1, len(identities), err)
		}
	}
}
