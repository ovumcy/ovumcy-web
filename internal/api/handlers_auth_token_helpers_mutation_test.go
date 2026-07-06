package api

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// This file hardens the auth-token / session helpers in
// handlers_auth_token_helpers.go against the surviving mutants reported by the
// weekly gremlins run (baseline run 28758365493, internal_api shard map).
//
// The behavior under test is the OIDC logout-state rotation performed when a
// credential change re-issues the auth cookie (refreshCurrentSession ->
// setAuthCookie -> rotateOIDCLogoutState): the provider logout state keyed on
// the retiring session id must move to the freshly minted session id so the
// originating device can still complete provider logout, while every stale
// session is invalidated by the auth_session_version bump. Each guard in
// rotateOIDCLogoutState is a decision on that move, and the mutants flip those
// decisions; the round-trip assertions below observe the move at the store.

// validRotationLogoutState returns a fully-formed provider logout state that
// passes validOIDCLogoutState, so rotateOIDCLogoutState follows the Save/Delete
// arm rather than the invalid-state Delete-only arm.
func validRotationLogoutState() services.OIDCLogoutState {
	return services.OIDCLogoutState{
		EndSessionEndpoint:    "https://idp.example.com/logout",
		IDTokenHint:           "eyJhbGciOiJSUzI1NiJ9.header.signature",
		PostLogoutRedirectURL: "https://app.example.com/",
	}
}

// TestRotateOIDCLogoutStateMovesStateToNewSessionOnPasswordChange drives a real
// password change on a session that owns a stored provider logout state and
// asserts the state is re-keyed from the retiring session id to the new one.
//
// Kills the rotation-decision mutants in rotateOIDCLogoutState:
//   - L63 `handler == nil` / `oidcLogoutStateSvc == nil` negation (both would
//     early-return nil and skip the move),
//   - L68 `newSessionID == ""` negation,
//   - L73 `!ok || currentSession == nil` negation,
//   - L78 `oldSessionID == "" || oldSessionID == newSessionID` negation (both
//     operands),
//   - L83 `err != nil || !found` negation on the Load result.
//
// Under any of those negations the Save at the new session id never happens, so
// Load(newSessionID) comes back not-found and the assertion fails.
func TestRotateOIDCLogoutStateMovesStateToNewSessionOnPasswordChange(t *testing.T) {
	ctx := newSettingsSecurityTestContext(t, "token-rotate-move@example.com")

	oldSessionID := mustExtractAuthSessionIDFromCookieHeader(t, ctx.authCookie)
	stateService := services.NewOIDCLogoutStateService(db.NewRepositories(ctx.database).OIDCLogout)
	if err := stateService.Save(context.Background(), oldSessionID, validRotationLogoutState(), time.Now().UTC()); err != nil {
		t.Fatalf("seed logout state for old session: %v", err)
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}, map[string]string{"Accept": "application/json"})
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusSeeOther {
		t.Fatalf("change-password status = %d, want 200 or 303", response.StatusCode)
	}

	refreshed := responseCookie(response.Cookies(), authCookieName)
	if refreshed == nil || strings.TrimSpace(refreshed.Value) == "" {
		t.Fatal("change-password must reissue ovumcy_auth so the originating session survives")
	}
	newSessionID := mustExtractAuthSessionIDFromCookieHeader(t, refreshed.Name+"="+refreshed.Value)
	if newSessionID == oldSessionID {
		t.Fatalf("expected a fresh session id after refresh, got the retiring id %q", newSessionID)
	}

	movedState, found, err := stateService.Load(context.Background(), newSessionID, time.Now().UTC())
	if err != nil {
		t.Fatalf("load logout state for new session: %v", err)
	}
	if !found {
		t.Fatal("logout state was not rotated onto the new session id; rotateOIDCLogoutState did not move it")
	}
	if movedState.EndSessionEndpoint != validRotationLogoutState().EndSessionEndpoint {
		t.Fatalf("rotated logout state end_session_endpoint = %q, want %q", movedState.EndSessionEndpoint, validRotationLogoutState().EndSessionEndpoint)
	}

	staleState, staleFound, err := stateService.Load(context.Background(), oldSessionID, time.Now().UTC())
	if err != nil {
		t.Fatalf("load logout state for old session: %v", err)
	}
	if staleFound {
		t.Fatalf("retiring session id must not keep a logout state after rotation, got %#v", staleState)
	}
}

// TestRefreshCurrentSessionDoesNotLogRotationFailureOnSuccess pins the L110
// contract: a successful refresh whose rotation succeeds must NOT emit the
// `provider_logout_state_rotation_failed` security event. The mutant negates
// `if err := rotateOIDCLogoutState(...); err != nil` to `== nil`, which would
// log the failure on every successful rotation. Asserting the log's absence on
// the happy path fails under that mutation.
func TestRefreshCurrentSessionDoesNotLogRotationFailureOnSuccess(t *testing.T) {
	originalWriter := log.Writer()
	defer log.SetOutput(originalWriter)
	var output bytes.Buffer
	log.SetOutput(&output)

	ctx := newSettingsSecurityTestContextWithOptions(t, "token-rotate-nolog@example.com", onboardingTestAppOptions{enableCSRF: true, auditLogEnabled: true})

	oldSessionID := mustExtractAuthSessionIDFromCookieHeader(t, ctx.authCookie)
	stateService := services.NewOIDCLogoutStateService(db.NewRepositories(ctx.database).OIDCLogout)
	if err := stateService.Save(context.Background(), oldSessionID, validRotationLogoutState(), time.Now().UTC()); err != nil {
		t.Fatalf("seed logout state for old session: %v", err)
	}

	response := settingsFormRequestWithCSRF(t, ctx, http.MethodPut, "/api/v1/users/current/password", url.Values{
		"current_password": {"StrongPass1"},
		"new_password":     {"EvenStronger2"},
		"confirm_password": {"EvenStronger2"},
	}, map[string]string{"Accept": "application/json"})
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusSeeOther {
		t.Fatalf("change-password status = %d, want 200 or 303", response.StatusCode)
	}

	// Rotation succeeded (asserted structurally by the sibling test); on this
	// success path the failure event must be absent.
	if strings.Contains(output.String(), "provider_logout_state_rotation_failed") {
		t.Fatalf("successful refresh must not log a rotation failure, got %q", output.String())
	}
	// Guard: the audit stream is actually flowing, so the assertion above is a
	// real observation and not a silently-empty buffer.
	if !strings.Contains(output.String(), `action="auth.password_change"`) {
		t.Fatalf("expected the password-change audit event to be emitted, got %q", output.String())
	}
}

// TestBuildTokenWithSessionIDKeepsCallerTTL pins the positive-TTL contract of
// buildTokenWithSessionID's `if ttl <= 0` clamp (L56). The CONDITIONALS_NEGATION
// mutant turns it into `if ttl > 0`, which overrides a caller's explicit
// positive TTL with defaultAuthTokenTTL (7d). Building a token with a 1h TTL and
// asserting the parsed expiry stays ~1h (well under a day) fails under that
// mutation.
//
// The paired BOUNDARY mutant (`<=` -> `<`) is documented as equivalent in the
// PR body: it only changes behavior at ttl == 0, and there the inner clamp in
// services.BuildAuthSessionTokenWithVersionAndSessionID (also 7d) yields the
// identical expiry whether or not the outer clamp fires.
func TestBuildTokenWithSessionIDKeepsCallerTTL(t *testing.T) {
	t.Parallel()

	handler, database := newDataAccessTestHandler(t)
	user := createDataAccessTestUser(t, database, "token-ttl-keep@example.com")

	issuedAt := time.Now()
	token, sessionID, err := handler.buildTokenWithSessionID(&user, time.Hour)
	if err != nil {
		t.Fatalf("buildTokenWithSessionID returned error: %v", err)
	}
	if strings.TrimSpace(sessionID) == "" {
		t.Fatal("expected a non-empty session id")
	}

	claims, err := services.ParseAuthSessionToken(handler.secretKey, token, issuedAt)
	if err != nil {
		t.Fatalf("parse issued auth token: %v", err)
	}
	if claims.ExpiresAt == nil {
		t.Fatal("expected an expiry claim on the issued token")
	}

	lifetime := claims.ExpiresAt.Sub(issuedAt)
	// A caller TTL of 1h must be honored; anything at or beyond a day means the
	// clamp overrode a positive TTL with the multi-day default.
	if lifetime <= 0 || lifetime >= 24*time.Hour {
		t.Fatalf("issued token lifetime = %s, want ~1h (caller TTL honored, not clamped to the default)", lifetime)
	}
}
