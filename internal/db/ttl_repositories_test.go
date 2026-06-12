package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// TestRegisterPickupTokenRepositoryIssueConsumeTTL locks the single-use +
// TTL contract of the register pickup nonce: a successful Consume marks the
// row consumed in the same transaction (replay is indistinguishable from
// missing), expired rows never consume, and DeleteExpired drops only the
// expired ones.
func TestRegisterPickupTokenRepositoryIssueConsumeTTL(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "pickup.db"))
	repo := NewRegisterPickupTokenRepository(database)
	// Issue purges expired rows against the real clock, so the TTL anchors
	// here must be near-now for "expired" and "future" to mean the same thing
	// to the repository and to the assertions.
	base := time.Now().UTC().Truncate(time.Second)

	// Issue validation.
	if err := repo.Issue(context.Background(), "", 42, base.Add(5*time.Minute)); err == nil {
		t.Fatal("expected empty nonce to be rejected")
	}
	if err := repo.Issue(context.Background(), "nonce-a", 0, base.Add(5*time.Minute)); err == nil {
		t.Fatal("expected zero user id to be rejected")
	}
	if err := repo.Issue(context.Background(), "nonce-a", 42, time.Time{}); err == nil {
		t.Fatal("expected zero expiry to be rejected")
	}

	// Issue + consume returns the original user id once.
	if err := repo.Issue(context.Background(), "nonce-a", 42, base.Add(5*time.Minute)); err != nil {
		t.Fatalf("issue: %v", err)
	}
	if userID, ok, err := repo.Consume(context.Background(), "nonce-a", base); err != nil || !ok || userID != 42 {
		t.Fatalf("first consume = (%d, %t, %v), want (42, true, nil)", userID, ok, err)
	}

	// Single-use: replay returns the same indistinguishable (0,false,nil).
	if userID, ok, err := repo.Consume(context.Background(), "nonce-a", base); err != nil || ok || userID != 0 {
		t.Fatalf("replay consume = (%d, %t, %v), want (0, false, nil)", userID, ok, err)
	}

	// Expired token cannot be consumed.
	if err := repo.Issue(context.Background(), "nonce-expired", 7, base.Add(-1*time.Minute)); err != nil {
		t.Fatalf("issue expired: %v", err)
	}
	if userID, ok, err := repo.Consume(context.Background(), "nonce-expired", base); err != nil || ok || userID != 0 {
		t.Fatalf("expired consume = (%d, %t, %v), want (0, false, nil)", userID, ok, err)
	}

	// Missing / empty nonce never consume.
	if _, ok, _ := repo.Consume(context.Background(), "does-not-exist", base); ok {
		t.Fatal("expected missing nonce to not consume")
	}
	if _, ok, _ := repo.Consume(context.Background(), "", base); ok {
		t.Fatal("expected empty nonce to not consume")
	}

	// DeleteExpired drops only expired rows.
	if err := repo.Issue(context.Background(), "nonce-future", 9, base.Add(5*time.Minute)); err != nil {
		t.Fatalf("issue future: %v", err)
	}
	if err := repo.Issue(context.Background(), "nonce-stale", 9, base.Add(-2*time.Minute)); err != nil {
		t.Fatalf("issue stale: %v", err)
	}
	if err := repo.DeleteExpired(context.Background(), base); err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if _, ok, _ := repo.Consume(context.Background(), "nonce-stale", base); ok {
		t.Fatal("expected stale row to be deleted by DeleteExpired")
	}
	if _, ok, _ := repo.Consume(context.Background(), "nonce-future", base); !ok {
		t.Fatal("expected future row to survive DeleteExpired")
	}
}

// TestRegisterPickupTokenRepositoryIssuePurgesExpiredRows pins the retention
// contract: rows are only ever created by Issue, and Issue drops every row
// whose TTL has lapsed, so the table stays bounded without any background
// job or operator action.
func TestRegisterPickupTokenRepositoryIssuePurgesExpiredRows(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "pickup-purge.db"))
	repo := NewRegisterPickupTokenRepository(database)
	now := time.Now().UTC()

	// Seed one already-expired and one consumed-but-expired row.
	if err := repo.Issue(context.Background(), "stale-unconsumed", 7, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("issue stale: %v", err)
	}
	if err := repo.Issue(context.Background(), "stale-consumed", 8, now.Add(-5*time.Minute)); err != nil {
		t.Fatalf("issue stale consumed: %v", err)
	}

	countRows := func() int64 {
		var count int64
		if err := database.Model(&models.RegisterPickupToken{}).Count(&count).Error; err != nil {
			t.Fatalf("count rows: %v", err)
		}
		return count
	}
	// The second Issue already purged the first stale row, so at most the
	// freshly inserted row remains plus nothing older.
	if got := countRows(); got != 1 {
		t.Fatalf("after second issue: %d rows, want 1 (purge-on-issue)", got)
	}

	// A live issue purges the remaining expired rows and leaves only itself.
	if err := repo.Issue(context.Background(), "live", 9, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("issue live: %v", err)
	}
	if got := countRows(); got != 1 {
		t.Fatalf("after live issue: %d rows, want only the live row", got)
	}
	if userID, ok, err := repo.Consume(context.Background(), "live", now); err != nil || !ok || userID != 9 {
		t.Fatalf("live consume = (%d, %t, %v), want (9, true, nil)", userID, ok, err)
	}

	// Purge errors propagate and roll the insert back: with the table gone
	// the in-transaction delete fails before the create runs.
	if err := database.Exec("DROP TABLE register_pickup_tokens").Error; err != nil {
		t.Fatalf("drop register_pickup_tokens: %v", err)
	}
	if err := repo.Issue(context.Background(), "after-drop", 11, now.Add(5*time.Minute)); err == nil {
		t.Fatal("expected Issue to surface the purge error")
	}

	// Repository errors propagate: a closed connection surfaces from the
	// purge+insert transaction instead of being swallowed.
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("database.DB(): %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
	if err := repo.Issue(context.Background(), "after-close", 10, now.Add(5*time.Minute)); err == nil {
		t.Fatal("expected Issue on a closed database to fail")
	}
}

// TestOIDCLogoutStateRepositorySaveFindTTL covers the OIDC logout-state
// persistence: save/find round-trip, session_id upsert, not-found, targeted
// delete, and TTL sweep.
func TestOIDCLogoutStateRepositorySaveFindTTL(t *testing.T) {
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "logout.db"))
	repo := NewOIDCLogoutStateRepository(database)
	base := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)

	newState := func(sessionID, endpoint, hint string, expiresAt time.Time) *models.OIDCLogoutState {
		return &models.OIDCLogoutState{
			SessionID:             sessionID,
			EndSessionEndpoint:    endpoint,
			IDTokenHint:           hint,
			PostLogoutRedirectURL: "https://ovumcy.example.com/login",
			ExpiresAt:             expiresAt,
		}
	}

	// nil is a no-op.
	if err := repo.Save(context.Background(), nil); err != nil {
		t.Fatalf("save nil: %v", err)
	}

	if err := repo.Save(context.Background(), newState("sess-1", "https://id.example.com/logout", "hint-1", base.Add(10*time.Minute))); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, ok, err := repo.FindBySessionID(context.Background(), "sess-1")
	if err != nil || !ok {
		t.Fatalf("find = (ok=%t, err=%v), want found", ok, err)
	}
	if got.EndSessionEndpoint != "https://id.example.com/logout" || got.IDTokenHint != "hint-1" {
		t.Fatalf("find returned unexpected state: %#v", got)
	}

	// Upsert on session_id conflict updates the mutable columns.
	if err := repo.Save(context.Background(), newState("sess-1", "https://id.example.com/logout-v2", "hint-2", base.Add(10*time.Minute))); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	got, _, _ = repo.FindBySessionID(context.Background(), "sess-1")
	if got.EndSessionEndpoint != "https://id.example.com/logout-v2" || got.IDTokenHint != "hint-2" {
		t.Fatalf("expected upsert to update columns, got %#v", got)
	}

	// Missing session.
	if _, ok, _ := repo.FindBySessionID(context.Background(), "nope"); ok {
		t.Fatal("expected missing session to return not-found")
	}

	// Targeted delete.
	if err := repo.DeleteBySessionID(context.Background(), "sess-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, ok, _ := repo.FindBySessionID(context.Background(), "sess-1"); ok {
		t.Fatal("expected deleted session to be gone")
	}

	// TTL sweep drops only expired rows.
	if err := repo.Save(context.Background(), newState("valid", "https://id.example.com/l", "h", base.Add(10*time.Minute))); err != nil {
		t.Fatalf("save valid: %v", err)
	}
	if err := repo.Save(context.Background(), newState("stale", "https://id.example.com/l", "h", base.Add(-1*time.Minute))); err != nil {
		t.Fatalf("save stale: %v", err)
	}
	if err := repo.DeleteExpired(context.Background(), base); err != nil {
		t.Fatalf("delete expired: %v", err)
	}
	if _, ok, _ := repo.FindBySessionID(context.Background(), "stale"); ok {
		t.Fatal("expected stale logout state to be deleted")
	}
	if _, ok, _ := repo.FindBySessionID(context.Background(), "valid"); !ok {
		t.Fatal("expected valid logout state to survive")
	}
}
