package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

// moveLogoutStateStub is a test-local stub for services.OIDCLogoutStateStore,
// scoped to driving moveOIDCLogoutState's own decisions directly rather than
// through an HTTP round trip: the not-found arm, the invalid-state arm, and
// the Save-error arm are all storage-shaped outcomes a stub can produce on
// demand, without needing a live database or a full OIDC+TOTP request chain.
type moveLogoutStateStub struct {
	findRecord models.OIDCLogoutState
	findFound  bool
	findErr    error
	findCalls  int

	savedSessionID string
	saveErr        error
	saveCalls      int

	deleteBySessionID string
	deleteByUserID    uint
	deleteCalls       int
}

func (s *moveLogoutStateStub) Save(ctx context.Context, state *models.OIDCLogoutState) error {
	s.saveCalls++
	if state != nil {
		s.savedSessionID = state.SessionID
	}
	return s.saveErr
}

func (s *moveLogoutStateStub) FindBySessionID(ctx context.Context, sessionID string, userID uint) (models.OIDCLogoutState, bool, error) {
	s.findCalls++
	if s.findErr != nil {
		return models.OIDCLogoutState{}, false, s.findErr
	}
	return s.findRecord, s.findFound, nil
}

func (s *moveLogoutStateStub) DeleteBySessionID(ctx context.Context, sessionID string, userID uint) error {
	s.deleteCalls++
	s.deleteBySessionID = sessionID
	s.deleteByUserID = userID
	return nil
}

func (s *moveLogoutStateStub) DeleteExpired(ctx context.Context, cutoff time.Time) error {
	return nil
}

// validMoveLogoutStateRecord returns a record that passes validOIDCLogoutState,
// so a test using it exercises the Save/Delete arm of moveOIDCLogoutState
// rather than the invalid-state Delete-only arm.
func validMoveLogoutStateRecord(ownerID uint) models.OIDCLogoutState {
	return models.OIDCLogoutState{
		UserID:                ownerID,
		EndSessionEndpoint:    "https://idp.example.com/logout",
		IDTokenHint:           "eyJhbGciOiJSUzI1NiJ9.header.signature",
		PostLogoutRedirectURL: "https://app.example.com/",
	}
}

// TestMoveOIDCLogoutStateNilHandlerIsANoOp pins the `handler == nil` arm of
// the guard at the top of moveOIDCLogoutState — unreachable through any HTTP
// handler (a request always carries a real *Handler), but a direct call is
// cheap and the guard is otherwise silently unverified.
func TestMoveOIDCLogoutStateNilHandlerIsANoOp(t *testing.T) {
	var handler *Handler
	if err := handler.moveOIDCLogoutState(context.Background(), "old-session", "new-session", 7, time.Now()); err != nil {
		t.Fatalf("expected a nil handler to no-op, got %v", err)
	}
}

// TestMoveOIDCLogoutStateNilServiceIsANoOp pins the `oidcLogoutStateSvc == nil`
// arm of the same guard.
func TestMoveOIDCLogoutStateNilServiceIsANoOp(t *testing.T) {
	handler := &Handler{}
	if err := handler.moveOIDCLogoutState(context.Background(), "old-session", "new-session", 7, time.Now()); err != nil {
		t.Fatalf("expected a handler with no logout-state service to no-op, got %v", err)
	}
}

// TestMoveOIDCLogoutStateEmptyOrIdenticalSessionIDsAreANoOp pins every operand
// of the second guard: an empty old id, an empty new id, and the two ids
// already being equal all skip the relocation without touching the store.
func TestMoveOIDCLogoutStateEmptyOrIdenticalSessionIDsAreANoOp(t *testing.T) {
	cases := []struct {
		name string
		old  string
		new  string
	}{
		{name: "empty old session id", old: "", new: "new-session"},
		{name: "empty new session id", old: "old-session", new: ""},
		{name: "identical session ids", old: "same-session", new: "same-session"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			store := &moveLogoutStateStub{findFound: true, findRecord: validMoveLogoutStateRecord(7)}
			handler := &Handler{oidcLogoutStateSvc: services.NewOIDCLogoutStateService(store)}
			if err := handler.moveOIDCLogoutState(context.Background(), testCase.old, testCase.new, 7, time.Now()); err != nil {
				t.Fatalf("expected a no-op, got %v", err)
			}
			if store.findCalls != 0 {
				t.Fatalf("expected no store lookup before the session-id guard, got %d calls", store.findCalls)
			}
		})
	}
}

// TestMoveOIDCLogoutStateNotFoundIsANoOp pins the not-found arm: nothing was
// staged under oldSessionID (or it already expired out from under the
// service), so the relocation is a quiet no-op rather than an error — the
// session the TOTP challenge just minted simply carries no logout state,
// same as an OIDC login whose account had none to begin with.
func TestMoveOIDCLogoutStateNotFoundIsANoOp(t *testing.T) {
	store := &moveLogoutStateStub{findFound: false}
	handler := &Handler{oidcLogoutStateSvc: services.NewOIDCLogoutStateService(store)}
	if err := handler.moveOIDCLogoutState(context.Background(), "old-session", "new-session", 7, time.Now()); err != nil {
		t.Fatalf("expected a not-found row to no-op rather than error, got %v", err)
	}
	if store.saveCalls != 0 {
		t.Fatal("did not expect a Save when nothing was found to relocate")
	}
}

// TestMoveOIDCLogoutStateLoadErrorPropagates pins the other half of the same
// guard: a genuine storage error reading oldSessionID's row (not merely a
// miss) propagates to the caller instead of being swallowed as a quiet no-op.
func TestMoveOIDCLogoutStateLoadErrorPropagates(t *testing.T) {
	store := &moveLogoutStateStub{findErr: errors.New("storage unavailable")}
	handler := &Handler{oidcLogoutStateSvc: services.NewOIDCLogoutStateService(store)}
	if err := handler.moveOIDCLogoutState(context.Background(), "old-session", "new-session", 7, time.Now()); err == nil {
		t.Fatal("expected the Load error to propagate")
	}
}

// TestMoveOIDCLogoutStateInvalidRecordDeletesWithoutSaving pins the
// !validOIDCLogoutState arm: a row that Load found but that fails the same
// validity check buildLogoutState's own output always satisfies is dropped —
// deleted under the old id, never carried forward to the new one.
func TestMoveOIDCLogoutStateInvalidRecordDeletesWithoutSaving(t *testing.T) {
	store := &moveLogoutStateStub{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			UserID: 7,
			// EndSessionEndpoint deliberately blank: fails validOIDCLogoutState's
			// first check before any URL parsing.
		},
	}
	handler := &Handler{oidcLogoutStateSvc: services.NewOIDCLogoutStateService(store)}
	if err := handler.moveOIDCLogoutState(context.Background(), "old-session", "new-session", 7, time.Now()); err != nil {
		t.Fatalf("expected an invalid record to be dropped without error, got %v", err)
	}
	if store.saveCalls != 0 {
		t.Fatal("did not expect a Save for a record that fails validOIDCLogoutState")
	}
	if store.deleteCalls != 1 || store.deleteBySessionID != "old-session" || store.deleteByUserID != 7 {
		t.Fatalf("expected the invalid row deleted from the old session id for its owner, got calls=%d sessionID=%q userID=%d", store.deleteCalls, store.deleteBySessionID, store.deleteByUserID)
	}
}

// TestMoveOIDCLogoutStateSaveErrorPropagates pins the Save-error arm: a valid
// record that the store fails to persist under the new session id surfaces
// the error to the caller (VerifyTOTPLogin tears the session down on it)
// rather than silently dropping the logout state.
func TestMoveOIDCLogoutStateSaveErrorPropagates(t *testing.T) {
	store := &moveLogoutStateStub{
		findFound:  true,
		findRecord: validMoveLogoutStateRecord(7),
		saveErr:    errors.New("storage unavailable"),
	}
	handler := &Handler{oidcLogoutStateSvc: services.NewOIDCLogoutStateService(store)}
	if err := handler.moveOIDCLogoutState(context.Background(), "old-session", "new-session", 7, time.Now()); err == nil {
		t.Fatal("expected the Save error to propagate")
	}
	if store.deleteCalls != 0 {
		t.Fatal("did not expect the old row deleted when the Save onto the new session id failed")
	}
}

// TestMoveOIDCLogoutStateValidRecordMovesAndDeletesOld pins the happy path
// directly (the HTTP-level round trip in auth_oidc_regressions_test.go pins
// the same shape end to end): a valid record found under the old session id
// is saved under the new one, then deleted from the old one.
func TestMoveOIDCLogoutStateValidRecordMovesAndDeletesOld(t *testing.T) {
	store := &moveLogoutStateStub{findFound: true, findRecord: validMoveLogoutStateRecord(7)}
	handler := &Handler{oidcLogoutStateSvc: services.NewOIDCLogoutStateService(store)}
	if err := handler.moveOIDCLogoutState(context.Background(), "old-session", "new-session", 7, time.Now()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.saveCalls != 1 || store.savedSessionID != "new-session" {
		t.Fatalf("expected the record saved under the new session id, got calls=%d sessionID=%q", store.saveCalls, store.savedSessionID)
	}
	if store.deleteCalls != 1 || store.deleteBySessionID != "old-session" {
		t.Fatalf("expected the old session id's row deleted, got calls=%d sessionID=%q", store.deleteCalls, store.deleteBySessionID)
	}
}
