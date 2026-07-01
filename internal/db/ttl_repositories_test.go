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
// assertPickupConsume consumes nonce at time `at` and asserts the returned
// (userID, ok) pair, folding the repeated Consume-and-check blocks out of the
// scenario so its cyclomatic complexity stays in range.
func assertPickupConsume(t *testing.T, repo *RegisterPickupTokenRepository, nonce string, at time.Time, wantUserID uint, wantOK bool) {
	t.Helper()
	userID, ok, err := repo.Consume(context.Background(), nonce, at)
	requireNoErr(t, err, "consume "+nonce)
	if ok != wantOK || userID != wantUserID {
		t.Fatalf("consume %q = (%d, %t), want (%d, %t)", nonce, userID, ok, wantUserID, wantOK)
	}
}

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

	// Issue + consume returns the original user id once; replay is an
	// indistinguishable no-op (single-use).
	requireNoErr(t, repo.Issue(context.Background(), "nonce-a", 42, base.Add(5*time.Minute)), "issue nonce-a")
	assertPickupConsume(t, repo, "nonce-a", base, 42, true)
	assertPickupConsume(t, repo, "nonce-a", base, 0, false)

	// Expired token cannot be consumed.
	requireNoErr(t, repo.Issue(context.Background(), "nonce-expired", 7, base.Add(-1*time.Minute)), "issue expired")
	assertPickupConsume(t, repo, "nonce-expired", base, 0, false)

	// Missing / empty nonce never consume.
	assertPickupConsume(t, repo, "does-not-exist", base, 0, false)
	assertPickupConsume(t, repo, "", base, 0, false)

	// DeleteExpired drops only expired rows.
	requireNoErr(t, repo.Issue(context.Background(), "nonce-future", 9, base.Add(5*time.Minute)), "issue future")
	requireNoErr(t, repo.Issue(context.Background(), "nonce-stale", 9, base.Add(-2*time.Minute)), "issue stale")
	requireNoErr(t, repo.DeleteExpired(context.Background(), base), "delete expired")
	assertPickupConsume(t, repo, "nonce-stale", base, 0, false)
	assertPickupConsume(t, repo, "nonce-future", base, 9, true)
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
// assertLogoutState finds sessionID and asserts presence, plus — when present
// and wantEndpoint is set — the mutable columns, collapsing the repeated
// find-and-check blocks out of the scenario.
func assertLogoutState(t *testing.T, repo *OIDCLogoutStateRepository, sessionID string, wantOK bool, wantEndpoint, wantHint string) {
	t.Helper()
	got, ok, err := repo.FindBySessionID(context.Background(), sessionID)
	requireNoErr(t, err, "find "+sessionID)
	if ok != wantOK {
		t.Fatalf("find %q ok=%t, want %t", sessionID, ok, wantOK)
	}
	if wantOK && wantEndpoint != "" && (got.EndSessionEndpoint != wantEndpoint || got.IDTokenHint != wantHint) {
		t.Fatalf("find %q returned %#v, want endpoint=%q hint=%q", sessionID, got, wantEndpoint, wantHint)
	}
}

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
	requireNoErr(t, repo.Save(context.Background(), nil), "save nil")

	requireNoErr(t, repo.Save(context.Background(), newState("sess-1", "https://id.example.com/logout", "hint-1", base.Add(10*time.Minute))), "save")
	assertLogoutState(t, repo, "sess-1", true, "https://id.example.com/logout", "hint-1")

	// Upsert on session_id conflict updates the mutable columns.
	requireNoErr(t, repo.Save(context.Background(), newState("sess-1", "https://id.example.com/logout-v2", "hint-2", base.Add(10*time.Minute))), "re-save")
	assertLogoutState(t, repo, "sess-1", true, "https://id.example.com/logout-v2", "hint-2")

	// Missing session.
	assertLogoutState(t, repo, "nope", false, "", "")

	// Targeted delete.
	requireNoErr(t, repo.DeleteBySessionID(context.Background(), "sess-1"), "delete")
	assertLogoutState(t, repo, "sess-1", false, "", "")

	// TTL sweep drops only expired rows.
	requireNoErr(t, repo.Save(context.Background(), newState("valid", "https://id.example.com/l", "h", base.Add(10*time.Minute))), "save valid")
	requireNoErr(t, repo.Save(context.Background(), newState("stale", "https://id.example.com/l", "h", base.Add(-1*time.Minute))), "save stale")
	requireNoErr(t, repo.DeleteExpired(context.Background(), base), "delete expired")
	assertLogoutState(t, repo, "stale", false, "", "")
	assertLogoutState(t, repo, "valid", true, "", "")
}
