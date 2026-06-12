package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestDeleteAccountAndRelatedDataRemovesAllUserRows proves account erasure is
// complete across every user-scoped table — including register_pickup_tokens
// (which has no foreign key) and oidc_identities — so no orphaned auth-linkage
// rows survive a delete. This guards the GDPR right-to-erasure contract
// independently of whether ON DELETE CASCADE is enforced.
func TestDeleteAccountAndRelatedDataRemovesAllUserRows(t *testing.T) {
	dir := t.TempDir()
	database, err := OpenSQLite(filepath.Join(dir, "erasure.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repos := NewRepositories(database)

	user := &models.User{
		Email:            "erase@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	seed := []any{
		&models.DailyLog{UserID: user.ID, Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true},
		&models.SymptomType{UserID: user.ID, Name: "custom", Color: "#AABBCC"},
		&models.RegisterPickupToken{Nonce: "nonce-1", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour).UTC(), CreatedAt: time.Now().UTC()},
		&models.OIDCIdentity{UserID: user.ID, Issuer: "https://idp.example.com", Subject: "subject-1", CreatedAt: time.Now().UTC()},
		// Logout states are not user-scoped; erasure runs a best-effort
		// post-commit purge of globally expired rows.
		&models.OIDCLogoutState{SessionID: "sess-expired", EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(-time.Minute).UTC()},
		&models.OIDCLogoutState{SessionID: "sess-live", EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(time.Hour).UTC()},
	}
	for _, row := range seed {
		if err := database.Create(row).Error; err != nil {
			t.Fatalf("seed %T: %v", row, err)
		}
	}

	if err := repos.Users.DeleteAccountAndRelatedData(context.Background(), user.ID); err != nil {
		t.Fatalf("delete account: %v", err)
	}

	var usersLeft int64
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Count(&usersLeft).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersLeft != 0 {
		t.Fatalf("users still has %d row(s) for the deleted account", usersLeft)
	}

	type tableCheck struct {
		label string
		model any
	}
	for _, tc := range []tableCheck{
		{"daily_logs", &models.DailyLog{}},
		{"symptom_types", &models.SymptomType{}},
		{"register_pickup_tokens", &models.RegisterPickupToken{}},
		{"oidc_identities", &models.OIDCIdentity{}},
	} {
		var remaining int64
		if err := database.Model(tc.model).Where("user_id = ?", user.ID).Count(&remaining).Error; err != nil {
			t.Fatalf("count %s: %v", tc.label, err)
		}
		if remaining != 0 {
			t.Fatalf("%s still has %d row(s) for the deleted user — account erasure incomplete", tc.label, remaining)
		}
	}

	// The post-commit housekeeping purge drops expired logout states and
	// keeps unexpired ones (they age out via their own TTL).
	var expiredLeft int64
	if err := database.Model(&models.OIDCLogoutState{}).Where("session_id = ?", "sess-expired").Count(&expiredLeft).Error; err != nil {
		t.Fatalf("count expired logout states: %v", err)
	}
	if expiredLeft != 0 {
		t.Fatalf("expected expired oidc_logout_states row to be purged after erasure, found %d", expiredLeft)
	}
	var liveLeft int64
	if err := database.Model(&models.OIDCLogoutState{}).Where("session_id = ?", "sess-live").Count(&liveLeft).Error; err != nil {
		t.Fatalf("count live logout states: %v", err)
	}
	if liveLeft != 1 {
		t.Fatalf("expected unexpired oidc_logout_states row to survive erasure, found %d", liveLeft)
	}
}

// TestDeleteAccountAndRelatedDataSucceedsWhenLogoutStatePurgeFails pins the
// transaction boundary of the best-effort logout-state purge: it runs after
// the erasure transaction has committed, so a purge failure can neither
// poison the transaction (Postgres aborts a transaction after any errored
// statement) nor turn a completed erasure into a reported failure.
func TestDeleteAccountAndRelatedDataSucceedsWhenLogoutStatePurgeFails(t *testing.T) {
	dir := t.TempDir()
	database, err := OpenSQLite(filepath.Join(dir, "erasure-purgefail.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	repos := NewRepositories(database)

	user := &models.User{
		Email:            "purgefail@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repos.Users.Create(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Make the housekeeping purge fail without touching the erasure tables.
	if err := database.Exec("DROP TABLE oidc_logout_states").Error; err != nil {
		t.Fatalf("drop oidc_logout_states: %v", err)
	}

	if err := repos.Users.DeleteAccountAndRelatedData(context.Background(), user.ID); err != nil {
		t.Fatalf("erasure must succeed despite a failing logout-state purge, got: %v", err)
	}

	var usersLeft int64
	if err := database.Model(&models.User{}).Where("id = ?", user.ID).Count(&usersLeft).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if usersLeft != 0 {
		t.Fatalf("user must be erased even when the purge fails; rows = %d", usersLeft)
	}
}

// TestDeleteAccountAndRelatedDataRollsBackOnChildDeleteError exercises the
// error-return branches added for the explicit oidc_identities and
// register_pickup_tokens deletes: when one child delete fails mid-transaction,
// the whole erasure must roll back so the account is not left half-deleted.
func TestDeleteAccountAndRelatedDataRollsBackOnChildDeleteError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dropTable string
	}{
		{name: "register_pickup_tokens delete fails", dropTable: "register_pickup_tokens"},
		{name: "oidc_identities delete fails", dropTable: "oidc_identities"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			database, err := OpenSQLite(filepath.Join(dir, "delerr.db"))
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})
			repos := NewRepositories(database)
			user := &models.User{
				Email:            "delerr@example.com",
				PasswordHash:     "hash",
				RecoveryCodeHash: "recovery",
				Role:             models.RoleOwner,
				CycleLength:      models.DefaultCycleLength,
				PeriodLength:     models.DefaultPeriodLength,
				AutoPeriodFill:   true,
				CreatedAt:        time.Now().UTC(),
			}
			if err := repos.Users.Create(context.Background(), user); err != nil {
				t.Fatalf("create user: %v", err)
			}

			// Drop a child table so its delete inside the transaction errors,
			// hitting the error-return branch and forcing a rollback.
			if err := database.Exec("DROP TABLE " + tc.dropTable).Error; err != nil {
				t.Fatalf("drop %s: %v", tc.dropTable, err)
			}

			if err := repos.Users.DeleteAccountAndRelatedData(context.Background(), user.ID); err == nil {
				t.Fatal("expected an error when a child-table delete fails, got nil")
			}

			var usersLeft int64
			if err := database.Model(&models.User{}).Where("id = ?", user.ID).Count(&usersLeft).Error; err != nil {
				t.Fatalf("count users: %v", err)
			}
			if usersLeft != 1 {
				t.Fatalf("transaction must roll back on delete error; user rows = %d, want 1", usersLeft)
			}
		})
	}
}
