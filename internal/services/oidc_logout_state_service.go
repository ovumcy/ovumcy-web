package services

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

const defaultOIDCLogoutStateTTL = 7 * 24 * time.Hour

// ErrOIDCLogoutStateUnattributed is returned when a logout-state read, write
// or delete arrives without the owner it acts for. The store refuses the same
// input; this service refuses it too, because the store is an interface and
// the guarantee must not depend on which implementation is wired in
// (`docs/SECURITY_INVARIANTS.md`). Neither pre-check is redundant with the
// other: dropping the one here would leave the refusal to whatever store is
// wired in, and a store written later need not have one.
// Regression: TestOIDCLogoutStateServiceRefusesAnAbsentOwner.
//
// It is the shared value from internal/models, not a second one with the same
// text: this package cannot import internal/db, and a redeclaration would
// leave errors.Is false across the layer boundary for the very error being
// tested for.
var ErrOIDCLogoutStateUnattributed = models.ErrOIDCLogoutStateUnattributed

type OIDCLogoutStateStore interface {
	Save(ctx context.Context, state *models.OIDCLogoutState) error
	FindBySessionID(ctx context.Context, sessionID string, userID uint) (models.OIDCLogoutState, bool, error)
	DeleteBySessionID(ctx context.Context, sessionID string, userID uint) error
	DeleteExpired(ctx context.Context, cutoff time.Time) error
}

// The id_token_hint is an ID token the provider signed for this owner, kept for
// up to defaultOIDCLogoutStateTTL. It is stored sealed under the instance
// secret and bound to its owner, so a database dump or backup alone does not
// carry a usable token and a row moved to another user_id does not open
// (WEB-489). Regression: TestOIDCLogoutStateServiceStoresTheHintSealed.
type OIDCLogoutStateService struct {
	store     OIDCLogoutStateStore
	secretKey []byte
}

func NewOIDCLogoutStateService(store OIDCLogoutStateStore, secretKey []byte) *OIDCLogoutStateService {
	return &OIDCLogoutStateService{store: store, secretKey: secretKey}
}

func oidcLogoutIDTokenHintAAD(userID uint) []byte {
	return []byte(fmt.Sprintf("ovumcy.field.oidc_logout_id_token_hint:%d", userID))
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

	sealedHint := ""
	if hint := strings.TrimSpace(state.IDTokenHint); hint != "" {
		sealed, err := security.EncryptField(hint, service.secretKey, oidcLogoutIDTokenHintAAD(state.UserID))
		if err != nil {
			return err
		}
		sealedHint = sealed
	}

	if err := service.store.DeleteExpired(ctx, now); err != nil {
		return err
	}
	return service.store.Save(ctx, &models.OIDCLogoutState{
		SessionID:             sessionID,
		UserID:                state.UserID,
		EndSessionEndpoint:    strings.TrimSpace(state.EndSessionEndpoint),
		IDTokenHint:           sealedHint,
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

// resolve applies the expiry check, the hint unseal and the optional one-time
// delete to a record the caller has already established belongs to the owner
// it acts for. A row whose hint does not open is dropped like an expired one.
func (service *OIDCLogoutStateService) resolve(ctx context.Context, sessionID string, record models.OIDCLogoutState, now time.Time, consume bool) (OIDCLogoutState, bool, error) {
	hint, opened := service.openIDTokenHint(record)
	if !opened || (!record.ExpiresAt.IsZero() && !record.ExpiresAt.After(now)) {
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
		IDTokenHint:           hint,
		PostLogoutRedirectURL: strings.TrimSpace(record.PostLogoutRedirectURL),
	}, true, nil
}

// openIDTokenHint fails closed: a cleartext row written before WEB-489, a row
// sealed for another owner, and a payload sealed without the owner binding all
// report false, so the caller drops the row and the sign-out stays local.
func (service *OIDCLogoutStateService) openIDTokenHint(record models.OIDCLogoutState) (string, bool) {
	stored := strings.TrimSpace(record.IDTokenHint)
	if stored == "" {
		return "", true
	}
	hint, isLegacy, err := security.DecryptField(stored, service.secretKey, oidcLogoutIDTokenHintAAD(record.UserID))
	if err != nil || isLegacy {
		// Without this line a SECRET_KEY that no longer matches the stored rows
		// reads exactly like an instance with no provider-logout state at all.
		// Neither the session id nor the stored value is logged.
		log.Printf("oidc logout state: id_token_hint did not open, row dropped: owner id=%d unbound=%t", record.UserID, isLegacy)
		return "", false
	}
	return strings.TrimSpace(hint), true
}

func effectiveLogoutStateTime(now time.Time) time.Time {
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now.UTC()
}
