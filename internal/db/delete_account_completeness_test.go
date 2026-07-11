package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// countWhere returns the row count for model matching query, failing the test
// on a query error — folding the repeated count-and-check-err blocks out of the
// erasure scenarios.
func countWhere(t *testing.T, database *gorm.DB, model any, query string, args ...any) int64 {
	t.Helper()
	var count int64
	requireNoErr(t, database.Model(model).Where(query, args...).Count(&count).Error, "count")
	return count
}

// TestDeleteAccountAndRelatedDataRemovesAllUserRows proves account erasure is
// complete across every user-scoped table — including register_pickup_tokens
// (which has no foreign key) and oidc_identities — so no orphaned auth-linkage
// rows survive a delete. This guards the GDPR right-to-erasure contract
// independently of whether ON DELETE CASCADE is enforced.
func TestDeleteAccountAndRelatedDataRemovesAllUserRows(t *testing.T) {
	dir := t.TempDir()
	database, err := OpenSQLite(filepath.Join(dir, "erasure.db"))
	requireNoErr(t, err, "open sqlite")
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
	requireNoErr(t, repos.Users.Create(context.Background(), user), "create user")

	seed := []any{
		&models.DailyLog{UserID: user.ID, Date: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), IsPeriod: true},
		&models.SymptomType{UserID: user.ID, Name: "custom", Color: "#AABBCC"},
		&models.RegisterPickupToken{Nonce: "nonce-1", UserID: user.ID, ExpiresAt: time.Now().Add(time.Hour).UTC(), CreatedAt: time.Now().UTC()},
		&models.OIDCIdentity{UserID: user.ID, Issuer: "https://idp.example.com", Subject: "subject-1", CreatedAt: time.Now().UTC()},
		// oidc_logout_states: a row minted since migration 031 carries user_id and
		// is erased explicitly (sess-user). Legacy rows with a NULL user_id are not
		// user-scoped — the expired one is dropped by the best-effort post-commit
		// purge (sess-expired) and the unexpired one ages out via its own TTL
		// (sess-live).
		&models.OIDCLogoutState{SessionID: "sess-user", UserID: user.ID, EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(time.Hour).UTC()},
		&models.OIDCLogoutState{SessionID: "sess-expired", EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(-time.Minute).UTC()},
		&models.OIDCLogoutState{SessionID: "sess-live", EndSessionEndpoint: "https://idp.example.com/logout", IDTokenHint: "hint", ExpiresAt: time.Now().Add(time.Hour).UTC()},
	}
	for _, row := range seed {
		requireNoErr(t, database.Create(row).Error, "seed row")
	}

	requireNoErr(t, repos.Users.DeleteAccountAndRelatedData(context.Background(), user.ID), "delete account")

	if usersLeft := countWhere(t, database, &models.User{}, "id = ?", user.ID); usersLeft != 0 {
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
		{"oidc_logout_states", &models.OIDCLogoutState{}},
	} {
		if remaining := countWhere(t, database, tc.model, "user_id = ?", user.ID); remaining != 0 {
			t.Fatalf("%s still has %d row(s) for the deleted user — account erasure incomplete", tc.label, remaining)
		}
	}

	// The post-commit housekeeping purge drops expired logout states and keeps
	// unexpired ones (they age out via their own TTL).
	if expiredLeft := countWhere(t, database, &models.OIDCLogoutState{}, "session_id = ?", "sess-expired"); expiredLeft != 0 {
		t.Fatalf("expected expired oidc_logout_states row to be purged after erasure, found %d", expiredLeft)
	}
	if liveLeft := countWhere(t, database, &models.OIDCLogoutState{}, "session_id = ?", "sess-live"); liveLeft != 1 {
		t.Fatalf("expected unexpired oidc_logout_states row to survive erasure, found %d", liveLeft)
	}
}

// TestDeleteAccountAndRelatedDataRollsBackOnChildDeleteError exercises the
// error-return branches for the explicit oidc_identities, register_pickup_tokens,
// and oidc_logout_states deletes: when one child delete fails mid-transaction,
// the whole erasure must roll back so the account is not left half-deleted.
func TestDeleteAccountAndRelatedDataRollsBackOnChildDeleteError(t *testing.T) {
	for _, tc := range []struct {
		name      string
		dropTable string
	}{
		{name: "register_pickup_tokens delete fails", dropTable: "register_pickup_tokens"},
		{name: "oidc_identities delete fails", dropTable: "oidc_identities"},
		{name: "oidc_logout_states delete fails", dropTable: "oidc_logout_states"},
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
