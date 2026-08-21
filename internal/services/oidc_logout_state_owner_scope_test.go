package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"gorm.io/gorm"
)

// TestOIDCSignInWritesALogoutStateAccountErasureCanReach is the write half of
// the logout-state owner boundary. Reading is scoped to the session's owner,
// which only helps if the row carries one: a state built without its owner is
// persisted with user_id = 0, and account erasure — which deletes these rows
// with WHERE user_id = ? — can never match it, leaving a raw id_token_hint
// behind for up to the seven-day TTL after the account is gone. Migration 033
// purged the rows that predate the column so the table could hold that
// guarantee; a writer that mints an unattributed row breaks it again.
//
// The provider stays a double — only the code exchange is replaced. What is
// real here is the resolution of the account, the row that gets written, and
// the erasure that has to reach it.
func TestOIDCSignInWritesALogoutStateAccountErasureCanReach(t *testing.T) {
	database := newTwoOwnerIntegrationDatabase(t, "ovumcy-oidc-logout-write")
	repositories := db.NewRepositories(database)
	ctx := context.Background()
	now := time.Date(2026, time.May, 13, 10, 0, 0, 0, time.UTC)

	const (
		issuer    = "https://id.example.com"
		subject   = "logout-write-sub"
		sessionID = "sess-oidc-sign-in"
	)

	owner := createTwoOwnerUser(t, database, "oidc-logout-owner@example.com", nil)
	createTwoOwnerOIDCIdentity(t, database, owner.ID, issuer, subject)

	loginService := NewOIDCLoginService(&stubOIDCProviderClient{
		enabled: true,
		config:  oidcloginserviceCovLogoutConfig(),
		exchange: security.OIDCExchangeResult{
			Claims: security.OIDCClaims{
				Issuer:        issuer,
				Subject:       subject,
				Email:         owner.Email,
				EmailVerified: true,
				AuthTime:      now.Add(-2 * time.Minute),
			},
			Session: security.OIDCSession{
				EndSessionEndpoint: "https://id.example.com/logout",
				IDTokenHint:        "raw-id-token",
			},
		},
	}, repositories.OIDCIdentities, repositories.Users, nil)

	result, err := loginService.Authenticate(ctx, "code", "verifier", "nonce", now)
	if err != nil {
		t.Fatalf("Authenticate(): unexpected error: %v", err)
	}
	if result.Logout == nil {
		t.Fatal("expected the sign-in to build a provider logout state; the fixture enables provider logout and supplies both an end-session endpoint and an id_token_hint")
	}
	if result.Logout.UserID != owner.ID {
		t.Fatalf("the OIDC sign-in built a logout state owned by %d, want owner %d — an unattributed state is written with user_id = 0 and account erasure can never reach it", result.Logout.UserID, owner.ID)
	}

	stateService := NewOIDCLogoutStateService(repositories.OIDCLogout)
	if err := stateService.Save(ctx, sessionID, *result.Logout, now); err != nil {
		t.Fatalf("persist the sign-in's logout state: %v", err)
	}

	var stored models.OIDCLogoutState
	if err := database.Where("session_id = ?", sessionID).First(&stored).Error; err != nil {
		t.Fatalf("read the persisted logout state back: %v", err)
	}
	if stored.UserID != owner.ID {
		t.Fatalf("persisted row carries user_id = %d, want %d", stored.UserID, owner.ID)
	}

	// The point of the attribution: erasure's own predicate reaches the row.
	// Counting before proves the delete below had something to remove, so the
	// zero afterwards cannot be satisfied by a row that was never written.
	if count := countLogoutStates(t, database, sessionID); count != 1 {
		t.Fatalf("expected exactly one persisted logout state before erasure, got %d", count)
	}
	if err := repositories.Users.DeleteAccountAndRelatedData(ctx, owner.ID); err != nil {
		t.Fatalf("erase the account: %v", err)
	}
	if count := countLogoutStates(t, database, sessionID); count != 0 {
		t.Fatalf("account erasure left %d provider-logout row(s) — with them the raw id_token_hint — behind: the row is not attributable to the erased owner", count)
	}
}

func countLogoutStates(t *testing.T, database *gorm.DB, sessionID string) int64 {
	t.Helper()

	var count int64
	if err := database.Model(&models.OIDCLogoutState{}).Where("session_id = ?", sessionID).Count(&count).Error; err != nil {
		t.Fatalf("count logout states for %q: %v", sessionID, err)
	}
	return count
}

// TestOIDCLogoutStateServiceRefusesARecordThatIsNotTheAskingOwners pins the
// service half of the boundary. The repository predicate is one layer; this is
// the other, and it is not redundant: the store is an interface, so the
// guarantee has to hold for whatever implementation is wired in rather than
// resting on the SQL of the one that ships. The stub here returns its record
// for any owner, which is exactly the store that would defeat a service that
// trusted the lookup.
func TestOIDCLogoutStateServiceRefusesARecordThatIsNotTheAskingOwners(t *testing.T) {
	t.Parallel()

	const (
		ownerA uint = 11
		ownerB uint = 12
	)

	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:             "sess-owned-by-b",
			UserID:                ownerB,
			EndSessionEndpoint:    "https://id.example.com/logout",
			IDTokenHint:           "owner-b-id-token",
			PostLogoutRedirectURL: "https://app.example.com/login",
			ExpiresAt:             time.Now().Add(time.Hour).UTC(),
		},
	}
	service := NewOIDCLogoutStateService(store)

	got, found, err := service.Load(context.Background(), "sess-owned-by-b", ownerA, time.Now())
	if err != nil {
		t.Fatalf("Load() as the other owner: unexpected error: %v", err)
	}
	if found {
		t.Fatalf("owner %d was handed owner %d's provider-logout state: the service does not check the record's owner (got %#v)", ownerA, ownerB, got)
	}
	if store.findUserID != ownerA {
		t.Fatalf("the lookup must carry the owner it acts for to the store; store saw %d, want %d", store.findUserID, ownerA)
	}

	// Positive anchor: the same store, the same record, the owner it belongs
	// to — otherwise a service that found nothing for anybody would pass.
	got, found, err = service.Load(context.Background(), "sess-owned-by-b", ownerB, time.Now())
	if err != nil || !found {
		t.Fatalf("owner %d must read their own state back (found=%t, err=%v)", ownerB, found, err)
	}
	// The owner travels with the state: session rotation re-saves what Load
	// returned, and a state that lost its owner on the way out would be
	// written back unattributed.
	if got.UserID != ownerB {
		t.Fatalf("Load() dropped the owner from the state it returned (got %d, want %d) — a rotation re-saving this would write an unattributable row", got.UserID, ownerB)
	}
}

// TestOIDCLogoutStateServiceRefusesAnAbsentOwner covers the other half of the
// same rule: a missing owner is invalid input on every owner-scoped entry
// point, never a lookup that matches every owner. The store must not be
// reached at all.
func TestOIDCLogoutStateServiceRefusesAnAbsentOwner(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{findFound: true}
	service := NewOIDCLogoutStateService(store)
	ctx := context.Background()

	if _, found, err := service.Load(ctx, "sess", 0, time.Now()); !errors.Is(err, ErrOIDCLogoutStateUnattributed) || found {
		t.Fatalf("Load() with no owner must be refused, got found=%t err=%v", found, err)
	}
	if _, found, err := service.Consume(ctx, "sess", 0, time.Now()); !errors.Is(err, ErrOIDCLogoutStateUnattributed) || found {
		t.Fatalf("Consume() with no owner must be refused, got found=%t err=%v", found, err)
	}
	if err := service.Delete(ctx, "sess", 0); !errors.Is(err, ErrOIDCLogoutStateUnattributed) {
		t.Fatalf("Delete() with no owner must be refused, got %v", err)
	}
	if err := service.Save(ctx, "sess", OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/logout",
		IDTokenHint:           "hint",
		PostLogoutRedirectURL: "https://app.example.com/login",
	}, time.Now()); !errors.Is(err, ErrOIDCLogoutStateUnattributed) {
		t.Fatalf("Save() of a state nobody owns must be refused, got %v", err)
	}
	if store.deleteByIDCalls != 0 || store.saved != nil {
		t.Fatalf("a refused call must not reach the store (deletes=%d, saved=%v)", store.deleteByIDCalls, store.saved)
	}
}

// TestOIDCLogoutStateConsumeUnattributedStillDeletesThroughTheRowsOwner covers
// the one deliberately ownerless entry point — the provider-logout bridge
// redirect, which carries no session and reads only the sealed bridge cookie.
// It may resolve without an owner because the cookie payload has none to give;
// it may not then perform an ownerless WRITE. The delete is scoped to the
// owner named by the row it just read, and a row with no owner is refused
// outright rather than deleted by session id alone.
func TestOIDCLogoutStateConsumeUnattributedStillDeletesThroughTheRowsOwner(t *testing.T) {
	t.Parallel()

	const ownerB uint = 12
	record := models.OIDCLogoutState{
		SessionID:             "sess-bridge",
		UserID:                ownerB,
		EndSessionEndpoint:    "https://id.example.com/logout",
		IDTokenHint:           "owner-b-id-token",
		PostLogoutRedirectURL: "https://app.example.com/login",
		ExpiresAt:             time.Now().Add(time.Hour).UTC(),
	}

	store := &oidclogoutstateserviceCovStore{findFound: true, findRecord: record}
	service := NewOIDCLogoutStateService(store)

	state, found, err := service.ConsumeUnattributed(context.Background(), "sess-bridge", time.Now())
	if err != nil || !found {
		t.Fatalf("ConsumeUnattributed(): found=%t err=%v", found, err)
	}
	if state.EndSessionEndpoint != record.EndSessionEndpoint {
		t.Fatalf("unexpected end-session endpoint %q", state.EndSessionEndpoint)
	}
	if store.unattributedCalls != 1 || store.unattributedSession != "sess-bridge" {
		t.Fatalf("expected exactly one unattributed lookup for the bridge session, got calls=%d id=%q", store.unattributedCalls, store.unattributedSession)
	}
	if store.deleteByIDCalls != 1 || store.deleteByUserID != ownerB {
		t.Fatalf("the one-time delete must be scoped to the row's own owner: calls=%d owner=%d, want 1 call for owner %d", store.deleteByIDCalls, store.deleteByUserID, ownerB)
	}

	// A row with no owner cannot be deleted under any owner, so it is not
	// usable at all: it is refused and left for the TTL sweep. Migration 033
	// purged these and both writers now refuse to create another.
	orphan := record
	orphan.UserID = 0
	orphanStore := &oidclogoutstateserviceCovStore{findFound: true, findRecord: orphan}
	orphanService := NewOIDCLogoutStateService(orphanStore)
	if _, found, err := orphanService.ConsumeUnattributed(context.Background(), "sess-bridge", time.Now()); found || err != nil {
		t.Fatalf("an unattributed row must not be usable, got found=%t err=%v", found, err)
	}
	if orphanStore.deleteByIDCalls != 0 {
		t.Fatalf("an unattributed row must not be deleted by session id alone, got %d delete(s)", orphanStore.deleteByIDCalls)
	}
}
