package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// oidclogoutstateserviceCovStore is a test-local stub for OIDCLogoutStateStore.
type oidclogoutstateserviceCovStore struct {
	saved               *models.OIDCLogoutState
	saveErr             error
	findRecord          models.OIDCLogoutState
	findFound           bool
	findErr             error
	deleteBySessionID   string
	deleteBySessionErr  error
	deleteExpiredCutoff time.Time
	deleteExpiredErr    error
	deleteExpiredCalls  int
	deleteByIDCalls     int
}

func (s *oidclogoutstateserviceCovStore) Save(ctx context.Context, state *models.OIDCLogoutState) error {
	s.saved = state
	return s.saveErr
}

func (s *oidclogoutstateserviceCovStore) FindBySessionID(ctx context.Context, sessionID string) (models.OIDCLogoutState, bool, error) {
	if s.findErr != nil {
		return models.OIDCLogoutState{}, false, s.findErr
	}
	return s.findRecord, s.findFound, nil
}

func (s *oidclogoutstateserviceCovStore) DeleteBySessionID(ctx context.Context, sessionID string) error {
	s.deleteBySessionID = sessionID
	s.deleteByIDCalls++
	return s.deleteBySessionErr
}

func (s *oidclogoutstateserviceCovStore) DeleteExpired(ctx context.Context, cutoff time.Time) error {
	s.deleteExpiredCutoff = cutoff
	s.deleteExpiredCalls++
	return s.deleteExpiredErr
}

// ---------------------------------------------------------------------------
// TTL constant (line 10): Save should compute ExpiresAt = now + 7*24h
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovSaveTTLIs7Days(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{}
	svc := NewOIDCLogoutStateService(store)

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	state := OIDCLogoutState{
		EndSessionEndpoint:    "https://id.example.com/logout",
		IDTokenHint:           "tok123",
		PostLogoutRedirectURL: "https://app.example.com/post-logout",
	}

	if err := svc.Save(context.Background(), "sess-ttl", state, now); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}
	if store.saved == nil {
		t.Fatal("expected Save() to persist a record")
	}
	want := now.Add(7 * 24 * time.Hour)
	if !store.saved.ExpiresAt.Equal(want) {
		t.Fatalf("ExpiresAt: want %s, got %s", want, store.saved.ExpiresAt)
	}
}

// ---------------------------------------------------------------------------
// Line 28: nil service / nil store guard in Save
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovSaveNilServiceReturnsNil(t *testing.T) {
	t.Parallel()

	var svc *OIDCLogoutStateService
	if err := svc.Save(context.Background(), "sess1", OIDCLogoutState{}, time.Now()); err != nil {
		t.Fatalf("nil receiver Save() should return nil, got %v", err)
	}
}

func TestOIDCLogoutStateServiceCovSaveNilStoreReturnsNil(t *testing.T) {
	t.Parallel()

	svc := &OIDCLogoutStateService{store: nil}
	if err := svc.Save(context.Background(), "sess1", OIDCLogoutState{}, time.Now()); err != nil {
		t.Fatalf("nil store Save() should return nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Line 32: empty sessionID guard in Save (after TrimSpace)
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovSaveEmptySessionIDReturnsNil(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{}
	svc := NewOIDCLogoutStateService(store)

	if err := svc.Save(context.Background(), "   ", OIDCLogoutState{}, time.Now()); err != nil {
		t.Fatalf("Save() with whitespace-only sessionID should return nil, got %v", err)
	}
	if store.saved != nil {
		t.Fatal("expected no record persisted for empty sessionID")
	}
}

// ---------------------------------------------------------------------------
// Line 41: DeleteExpired error propagation in Save
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovSaveDeleteExpiredErrorPropagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("db error deleting expired")
	store := &oidclogoutstateserviceCovStore{deleteExpiredErr: wantErr}
	svc := NewOIDCLogoutStateService(store)

	err := svc.Save(context.Background(), "sess-del-err", OIDCLogoutState{}, time.Now())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected DeleteExpired error to propagate, got %v", err)
	}
	if store.saved != nil {
		t.Fatal("expected no Save() call when DeleteExpired fails")
	}
}

// ---------------------------------------------------------------------------
// Line 64: nil service / nil store guard in Delete
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovDeleteNilServiceReturnsNil(t *testing.T) {
	t.Parallel()

	var svc *OIDCLogoutStateService
	if err := svc.Delete(context.Background(), "sess1"); err != nil {
		t.Fatalf("nil receiver Delete() should return nil, got %v", err)
	}
}

func TestOIDCLogoutStateServiceCovDeleteNilStoreReturnsNil(t *testing.T) {
	t.Parallel()

	svc := &OIDCLogoutStateService{store: nil}
	if err := svc.Delete(context.Background(), "sess1"); err != nil {
		t.Fatalf("nil store Delete() should return nil, got %v", err)
	}
}

// Delete happy path — also exercises the TrimSpace inside Delete
func TestOIDCLogoutStateServiceCovDeleteTrimSpaceAndDelegates(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{}
	svc := NewOIDCLogoutStateService(store)

	if err := svc.Delete(context.Background(), "  sess-del  "); err != nil {
		t.Fatalf("Delete() unexpected error: %v", err)
	}
	if store.deleteBySessionID != "sess-del" {
		t.Fatalf("expected trimmed session ID, got %q", store.deleteBySessionID)
	}
}

// ---------------------------------------------------------------------------
// Line 71: nil service / nil store guard in load (via Load and Consume)
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovLoadNilServiceReturnsEmpty(t *testing.T) {
	t.Parallel()

	var svc *OIDCLogoutStateService
	got, found, err := svc.Load(context.Background(), "sess1", time.Now())
	if err != nil || found || got != (OIDCLogoutState{}) {
		t.Fatalf("nil receiver Load() should return zero, false, nil; got %+v, %v, %v", got, found, err)
	}
}

func TestOIDCLogoutStateServiceCovLoadNilStoreReturnsEmpty(t *testing.T) {
	t.Parallel()

	svc := &OIDCLogoutStateService{store: nil}
	got, found, err := svc.Load(context.Background(), "sess1", time.Now())
	if err != nil || found || got != (OIDCLogoutState{}) {
		t.Fatalf("nil store Load() should return zero, false, nil; got %+v, %v, %v", got, found, err)
	}
}

// ---------------------------------------------------------------------------
// Line 75: empty sessionID guard in load
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovLoadEmptySessionIDReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{}
	svc := NewOIDCLogoutStateService(store)

	got, found, err := svc.Load(context.Background(), "  ", time.Now())
	if err != nil || found || got != (OIDCLogoutState{}) {
		t.Fatalf("Load() with whitespace sessionID should return zero, false, nil; got %+v, %v, %v", got, found, err)
	}
	if store.deleteExpiredCalls > 0 {
		t.Fatal("expected no DeleteExpired for empty sessionID")
	}
}

// ---------------------------------------------------------------------------
// Line 83: DeleteExpired error propagation in load
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovLoadDeleteExpiredErrorPropagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("db error in load")
	store := &oidclogoutstateserviceCovStore{deleteExpiredErr: wantErr}
	svc := NewOIDCLogoutStateService(store)

	_, found, err := svc.Load(context.Background(), "sess-load-err", time.Now())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected DeleteExpired error to propagate through Load, got %v", err)
	}
	if found {
		t.Fatal("expected found=false when DeleteExpired fails")
	}
}

// ---------------------------------------------------------------------------
// Line 88: not-found path (FindBySessionID returns found=false)
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovLoadNotFoundReturnsFalse(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{findFound: false}
	svc := NewOIDCLogoutStateService(store)

	got, found, err := svc.Load(context.Background(), "no-such-session", time.Now())
	if err != nil || found || got != (OIDCLogoutState{}) {
		t.Fatalf("Load() for missing session should return zero, false, nil; got %+v, %v, %v", got, found, err)
	}
}

// Line 88: FindBySessionID error propagation
func TestOIDCLogoutStateServiceCovLoadFindErrorPropagates(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("find error")
	store := &oidclogoutstateserviceCovStore{findErr: wantErr}
	svc := NewOIDCLogoutStateService(store)

	_, found, err := svc.Load(context.Background(), "sess-find-err", time.Now())
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected Find error to propagate through Load, got %v", err)
	}
	if found {
		t.Fatal("expected found=false on find error")
	}
}

// ---------------------------------------------------------------------------
// Line 92: expired-record path (ExpiresAt <= now → delete and return not found)
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovLoadExpiredRecordDeletedAndNotFound(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	// ExpiresAt is in the past, so the record is expired
	expiredAt := now.Add(-time.Minute)
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:          "sess-expired",
			EndSessionEndpoint: "https://id.example.com/logout",
			ExpiresAt:          expiredAt,
		},
	}
	svc := NewOIDCLogoutStateService(store)

	got, found, err := svc.Load(context.Background(), "sess-expired", now)
	if err != nil || found || got != (OIDCLogoutState{}) {
		t.Fatalf("Load() on expired record should return zero, false, nil; got %+v, %v, %v", got, found, err)
	}
	// The expired record must have been deleted
	if store.deleteByIDCalls != 1 || store.deleteBySessionID != "sess-expired" {
		t.Fatalf("expected DeleteBySessionID(sess-expired), calls=%d id=%q", store.deleteByIDCalls, store.deleteBySessionID)
	}
}

// Line 92: record exactly at expiry boundary (ExpiresAt == now → also expired)
func TestOIDCLogoutStateServiceCovLoadExactExpiryBoundaryIsExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID: "sess-boundary",
			ExpiresAt: now, // exactly at boundary → !After(now) → expired
		},
	}
	svc := NewOIDCLogoutStateService(store)

	_, found, err := svc.Load(context.Background(), "sess-boundary", now)
	if err != nil || found {
		t.Fatalf("record with ExpiresAt==now should be treated as expired; found=%v err=%v", found, err)
	}
}

// Line 92: DeleteBySessionID error propagates when record is expired
func TestOIDCLogoutStateServiceCovLoadExpiredDeleteErrorPropagates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	wantErr := errors.New("delete expired failed")
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID: "sess-exp-del-err",
			ExpiresAt: now.Add(-time.Minute),
		},
		deleteBySessionErr: wantErr,
	}
	svc := NewOIDCLogoutStateService(store)

	_, _, err := svc.Load(context.Background(), "sess-exp-del-err", now)
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected delete error to propagate; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Line 92: non-expired record → Load returns data (IsZero ExpiresAt guard too)
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovLoadValidRecordReturnsData(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:             "sess-ok",
			EndSessionEndpoint:    " https://id.example.com/logout ",
			IDTokenHint:           " tok ",
			PostLogoutRedirectURL: " https://app.example.com/done ",
			ExpiresAt:             now.Add(time.Hour),
		},
	}
	svc := NewOIDCLogoutStateService(store)

	got, found, err := svc.Load(context.Background(), "sess-ok", now)
	if err != nil {
		t.Fatalf("Load() unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for valid non-expired record")
	}
	// Fields should be trimmed
	if got.EndSessionEndpoint != "https://id.example.com/logout" {
		t.Fatalf("EndSessionEndpoint not trimmed: %q", got.EndSessionEndpoint)
	}
	if got.IDTokenHint != "tok" {
		t.Fatalf("IDTokenHint not trimmed: %q", got.IDTokenHint)
	}
	if got.PostLogoutRedirectURL != "https://app.example.com/done" {
		t.Fatalf("PostLogoutRedirectURL not trimmed: %q", got.PostLogoutRedirectURL)
	}
}

// ExpiresAt.IsZero() → treated as non-expired (defensive: zero means no expiry set)
func TestOIDCLogoutStateServiceCovLoadZeroExpiresAtNotExpired(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:          "sess-zero-exp",
			EndSessionEndpoint: "https://id.example.com/logout",
			ExpiresAt:          time.Time{}, // zero
		},
	}
	svc := NewOIDCLogoutStateService(store)

	_, found, err := svc.Load(context.Background(), "sess-zero-exp", now)
	if err != nil || !found {
		t.Fatalf("zero ExpiresAt should not be treated as expired; found=%v err=%v", found, err)
	}
}

// ---------------------------------------------------------------------------
// Line 98: consume=true (Consume) vs consume=false (Load)
// ---------------------------------------------------------------------------

// Consume should delete the record after returning it
func TestOIDCLogoutStateServiceCovConsumeDeletesRecord(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:          "sess-consume",
			EndSessionEndpoint: "https://id.example.com/logout",
			IDTokenHint:        "tok-consume",
			ExpiresAt:          now.Add(time.Hour),
		},
	}
	svc := NewOIDCLogoutStateService(store)

	got, found, err := svc.Consume(context.Background(), "sess-consume", now)
	if err != nil {
		t.Fatalf("Consume() unexpected error: %v", err)
	}
	if !found {
		t.Fatal("expected found=true for Consume on valid record")
	}
	if got.IDTokenHint != "tok-consume" {
		t.Fatalf("expected IDTokenHint tok-consume, got %q", got.IDTokenHint)
	}
	// Record must have been deleted
	if store.deleteByIDCalls != 1 || store.deleteBySessionID != "sess-consume" {
		t.Fatalf("Consume() should delete record; calls=%d id=%q", store.deleteByIDCalls, store.deleteBySessionID)
	}
}

// Load should NOT delete the record
func TestOIDCLogoutStateServiceCovLoadDoesNotDeleteRecord(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID:          "sess-load-nodelete",
			EndSessionEndpoint: "https://id.example.com/logout",
			ExpiresAt:          now.Add(time.Hour),
		},
	}
	svc := NewOIDCLogoutStateService(store)

	_, found, err := svc.Load(context.Background(), "sess-load-nodelete", now)
	if err != nil || !found {
		t.Fatalf("Load() unexpected result; found=%v err=%v", found, err)
	}
	if store.deleteByIDCalls != 0 {
		t.Fatalf("Load() must not delete the record; deleteByIDCalls=%d", store.deleteByIDCalls)
	}
}

// Consume: DeleteBySessionID error propagates
func TestOIDCLogoutStateServiceCovConsumeDeleteErrorPropagates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	wantErr := errors.New("consume delete failed")
	store := &oidclogoutstateserviceCovStore{
		findFound: true,
		findRecord: models.OIDCLogoutState{
			SessionID: "sess-consume-err",
			ExpiresAt: now.Add(time.Hour),
		},
		deleteBySessionErr: wantErr,
	}
	svc := NewOIDCLogoutStateService(store)

	_, _, err := svc.Consume(context.Background(), "sess-consume-err", now)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Consume() delete error should propagate; got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Save: fields are trimmed and UTC-normalised
// ---------------------------------------------------------------------------

func TestOIDCLogoutStateServiceCovSaveFieldsTrimmedAndUTC(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{}
	svc := NewOIDCLogoutStateService(store)

	loc, _ := time.LoadLocation("America/New_York")
	now := time.Date(2026, 6, 1, 8, 0, 0, 0, loc) // non-UTC input
	state := OIDCLogoutState{
		EndSessionEndpoint:    "  https://id.example.com/logout  ",
		IDTokenHint:           "  tok123  ",
		PostLogoutRedirectURL: "  https://app.example.com/done  ",
	}

	if err := svc.Save(context.Background(), " sess-trim ", state, now); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}
	if store.saved == nil {
		t.Fatal("expected record persisted")
	}
	if store.saved.SessionID != "sess-trim" {
		t.Fatalf("expected trimmed session ID, got %q", store.saved.SessionID)
	}
	if store.saved.EndSessionEndpoint != "https://id.example.com/logout" {
		t.Fatalf("EndSessionEndpoint not trimmed: %q", store.saved.EndSessionEndpoint)
	}
	if store.saved.IDTokenHint != "tok123" {
		t.Fatalf("IDTokenHint not trimmed: %q", store.saved.IDTokenHint)
	}
	if store.saved.PostLogoutRedirectURL != "https://app.example.com/done" {
		t.Fatalf("PostLogoutRedirectURL not trimmed: %q", store.saved.PostLogoutRedirectURL)
	}
	if store.saved.CreatedAt.Location() != time.UTC {
		t.Fatalf("CreatedAt should be UTC, got %v", store.saved.CreatedAt.Location())
	}
}

// Save: DeleteExpired is called with the current time
func TestOIDCLogoutStateServiceCovSaveDeleteExpiredCalledWithNow(t *testing.T) {
	t.Parallel()

	store := &oidclogoutstateserviceCovStore{}
	svc := NewOIDCLogoutStateService(store)

	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	if err := svc.Save(context.Background(), "sess-de", OIDCLogoutState{}, now); err != nil {
		t.Fatalf("Save() unexpected error: %v", err)
	}
	if store.deleteExpiredCalls != 1 {
		t.Fatalf("expected 1 DeleteExpired call, got %d", store.deleteExpiredCalls)
	}
	if !store.deleteExpiredCutoff.Equal(now) {
		t.Fatalf("DeleteExpired cutoff: want %s, got %s", now, store.deleteExpiredCutoff)
	}
}
