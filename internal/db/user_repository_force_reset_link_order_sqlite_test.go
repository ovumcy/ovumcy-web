package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestForceResetFullAndAnIdentityLinkInEitherOrderOnSQLite is the SQLite half of
// TestForceResetFullDeletesAnIdentityLinkedConcurrentlyOnPostgres. The race that
// test stages cannot happen here: every write transaction opens with BEGIN
// IMMEDIATE (sqlite.go, `_txlock=immediate`) and holds the one database write
// lock until it commits, so the reset never reads the identities while a link
// is in flight. What is left are the two serial orders, and both must end with
// no identity and no second factor standing.
func TestForceResetFullAndAnIdentityLinkInEitherOrderOnSQLite(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "ovumcy.db"))
	repos := NewRepositories(database)
	ctx := context.Background()

	for _, order := range []struct {
		email     string
		linkFirst bool
	}{
		{email: "link-first@example.com", linkFirst: true},
		{email: "reset-first@example.com", linkFirst: false},
	} {
		user := &models.User{
			Email:            order.email,
			PasswordHash:     "hash",
			RecoveryCodeHash: "recovery",
			TOTPEnabled:      true,
			TOTPSecret:       "secret",
			Role:             models.RoleOwner,
			CycleLength:      models.DefaultCycleLength,
			PeriodLength:     models.DefaultPeriodLength,
			CreatedAt:        time.Now().UTC(),
		}
		if err := repos.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
		stored, err := repos.Users.FindByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("reload user: %v", err)
		}

		link := func() error {
			return repos.OIDCIdentities.CreateAndRevokeSessions(ctx, &models.OIDCIdentity{
				UserID: user.ID, Issuer: "https://idp.example.com", Subject: user.Email, CreatedAt: time.Now().UTC(),
			}, stored.AuthSessionVersion)
		}
		reset := func() error {
			return repos.Users.ForceResetPasswordAndRevokeSessions(ctx, user.ID, "operator-hash", true)
		}

		if order.linkFirst {
			if err := link(); err != nil {
				t.Fatalf("link: %v", err)
			}
			if err := reset(); err != nil {
				t.Fatalf("reset after link: %v", err)
			}
		} else {
			if err := reset(); err != nil {
				t.Fatalf("reset: %v", err)
			}
			if err := link(); !errors.Is(err, models.ErrAuthSessionVersionChanged) {
				t.Fatalf("a link verified before the reset must fail its session-version compare-and-set, got %v", err)
			}
		}

		var identities int64
		if err := database.Model(&models.OIDCIdentity{}).Where("user_id = ?", user.ID).Count(&identities).Error; err != nil {
			t.Fatalf("count identities: %v", err)
		}
		if identities != 0 {
			t.Fatalf("linkFirst=%v: %d identity(ies) outlived reset-password --full", order.linkFirst, identities)
		}
		after, err := repos.Users.FindByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("reload reset user: %v", err)
		}
		if after.TOTPEnabled || after.TOTPSecret != "" || after.RecoveryCodeHash != "" {
			t.Fatalf("linkFirst=%v: reset-password --full left a second factor standing (totp_enabled=%v)", order.linkFirst, after.TOTPEnabled)
		}
	}
}
