package services

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

const defaultOIDCLogoutStateTTL = 7 * 24 * time.Hour

// ErrOIDCLogoutStateUnattributed is returned when a logout-state read, write
// or delete arrives without the owner it acts for. The store refuses the same
// input; this service refuses it too, because the store is an interface and
// the guarantee must not depend on which implementation is wired in
// (`docs/SECURITY_INVARIANTS.md`).
//
// The shipped store declares its own value of the same name and the same text,
// and `errors.Is` between the two is false. Linking them would mean importing
// `internal/db` here — no production file in this package does, so the whole
// package would gain gorm and the SQLite driver in its build graph for one
// error value. This value is the one callers test for, and it stays the only
// one they can see because every entry point below refuses a zero owner BEFORE
// touching the store: a store's own refusal is never propagated. Dropping a
// pre-check would leak the store's value unwrapped, and a caller's
// `errors.Is(err, ErrOIDCLogoutStateUnattributed)` would silently go false.
// Regression: TestOIDCLogoutStateServiceRefusesAnAbsentOwner.
var ErrOIDCLogoutStateUnattributed = errors.New("oidc logout state requires an owner id")

type OIDCLogoutStateStore interface {
	Save(ctx context.Context, state *models.OIDCLogoutState) error
	FindBySessionID(ctx context.Context, sessionID string, userID uint) (models.OIDCLogoutState, bool, error)
	DeleteBySessionID(ctx context.Context, sessionID string, userID uint) error
	DeleteExpired(ctx context.Context, cutoff time.Time) error
}

type OIDCLogoutStateService struct {
	store OIDCLogoutStateStore
}

func NewOIDCLogoutStateService(store OIDCLogoutStateStore) *OIDCLogoutStateService {
	return &OIDCLogoutStateService{store: store}
}

// Save persists the provider-logout material for one session. The owner rides
// in state.UserID and is required: a row nobody owns is invisible to the
// owner-scoped reads below and to account erasure, which deletes these rows by
// user_id.
func (service *OIDCLogoutStateService) Save(ctx context.Context, sessionID string, state OIDCLogoutState, now time.Time) error {
	if service == nil || service.store == nil {
		return nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if state.UserID == 0 {
		return ErrOIDCLogoutStateUnattributed
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	expiresAt := now.Add(defaultOIDCLogoutStateTTL)

	if err := service.store.DeleteExpired(ctx, now); err != nil {
		return err
	}
	return service.store.Save(ctx, &models.OIDCLogoutState{
		SessionID:             sessionID,
		UserID:                state.UserID,
		EndSessionEndpoint:    strings.TrimSpace(state.EndSessionEndpoint),
		IDTokenHint:           strings.TrimSpace(state.IDTokenHint),
		PostLogoutRedirectURL: strings.TrimSpace(state.PostLogoutRedirectURL),
		ExpiresAt:             expiresAt,
		CreatedAt:             now,
		UpdatedAt:             now,
	})
}

// Load reads the logout state a session id carries for the given owner. Both
// operands are required; userID is never inferred from the record.
func (service *OIDCLogoutStateService) Load(ctx context.Context, sessionID string, userID uint, now time.Time) (OIDCLogoutState, bool, error) {
	return service.load(ctx, sessionID, userID, now, false)
}

// Consume is Load plus the one-time delete of the row it returned.
func (service *OIDCLogoutStateService) Consume(ctx context.Context, sessionID string, userID uint, now time.Time) (OIDCLogoutState, bool, error) {
	return service.load(ctx, sessionID, userID, now, true)
}

// Delete removes one owner's logout state for a session id.
func (service *OIDCLogoutStateService) Delete(ctx context.Context, sessionID string, userID uint) error {
	if service == nil || service.store == nil {
		return nil
	}
	if userID == 0 {
		return ErrOIDCLogoutStateUnattributed
	}
	return service.store.DeleteBySessionID(ctx, strings.TrimSpace(sessionID), userID)
}

func (service *OIDCLogoutStateService) load(ctx context.Context, sessionID string, userID uint, now time.Time, consume bool) (OIDCLogoutState, bool, error) {
	if service == nil || service.store == nil {
		return OIDCLogoutState{}, false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return OIDCLogoutState{}, false, nil
	}
	if userID == 0 {
		return OIDCLogoutState{}, false, ErrOIDCLogoutStateUnattributed
	}
	now = effectiveLogoutStateTime(now)

	if err := service.store.DeleteExpired(ctx, now); err != nil {
		return OIDCLogoutState{}, false, err
	}

	record, found, err := service.store.FindBySessionID(ctx, sessionID, userID)
	if err != nil || !found {
		return OIDCLogoutState{}, false, err
	}
	if record.UserID != userID {
		// The store's own predicate already excludes this row; re-checking here
		// keeps the guarantee with the service rather than with whichever store
		// happens to be wired in.
		return OIDCLogoutState{}, false, nil
	}

	return service.resolve(ctx, sessionID, record, now, consume)
}

// resolve applies the expiry check and the optional one-time delete to a
// record the caller has already established belongs to the owner it acts for.
func (service *OIDCLogoutStateService) resolve(ctx context.Context, sessionID string, record models.OIDCLogoutState, now time.Time, consume bool) (OIDCLogoutState, bool, error) {
	if !record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now) {
		if deleteErr := service.store.DeleteBySessionID(ctx, sessionID, record.UserID); deleteErr != nil {
			return OIDCLogoutState{}, false, deleteErr
		}
		return OIDCLogoutState{}, false, nil
	}
	if consume {
		if err := service.store.DeleteBySessionID(ctx, sessionID, record.UserID); err != nil {
			return OIDCLogoutState{}, false, err
		}
	}

	return OIDCLogoutState{
		// The owner travels with the state: a caller that re-saves it onto a
		// new session id (session rotation) carries the attribution forward
		// instead of re-supplying it from somewhere else.
		UserID:                record.UserID,
		EndSessionEndpoint:    strings.TrimSpace(record.EndSessionEndpoint),
		IDTokenHint:           strings.TrimSpace(record.IDTokenHint),
		PostLogoutRedirectURL: strings.TrimSpace(record.PostLogoutRedirectURL),
	}, true, nil
}

func effectiveLogoutStateTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}
