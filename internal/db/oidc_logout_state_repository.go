package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OIDCLogoutStateRepository struct {
	database *gorm.DB
}

func NewOIDCLogoutStateRepository(database *gorm.DB) *OIDCLogoutStateRepository {
	return &OIDCLogoutStateRepository{database: database}
}

// Save persists one owner's provider-logout state. A row with no owner is
// refused: account erasure reaches these rows by user_id, so an unattributed
// row would outlive the account it belongs to, and the owner-scoped lookups
// below could never read it back either.
func (repo *OIDCLogoutStateRepository) Save(ctx context.Context, state *models.OIDCLogoutState) error {
	if state == nil {
		return nil
	}
	if state.UserID == 0 {
		return ErrOIDCLogoutStateUnattributed
	}

	now := time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now

	return repo.database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"end_session_endpoint", "id_token_hint", "post_logout_redirect_url", "expires_at", "updated_at"}),
	}).Create(state).Error
}

// FindBySessionID resolves a provider-logout state by session id AND owner.
// The session id never identifies the row on its own: it is combined with the
// owner the caller acts for, so one owner's request can never surface another
// owner's end-session material (`docs/SECURITY_INVARIANTS.md`). A zero userID
// is invalid input, not a request to search every owner — refusing it keeps a
// missing operand from disabling the comparison.
func (repo *OIDCLogoutStateRepository) FindBySessionID(ctx context.Context, sessionID string, userID uint) (models.OIDCLogoutState, bool, error) {
	if userID == 0 {
		return models.OIDCLogoutState{}, false, ErrOIDCLogoutStateUnattributed
	}

	var state models.OIDCLogoutState
	if err := repo.database.WithContext(ctx).
		Where("session_id = ? AND user_id = ?", strings.TrimSpace(sessionID), userID).
		First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.OIDCLogoutState{}, false, nil
		}
		return models.OIDCLogoutState{}, false, err
	}
	return state, true, nil
}

// FindBySessionIDUnattributed resolves a logout state by session id alone.
//
// TRANSITIONAL — it exists for exactly one caller, the provider-logout bridge
// redirect, which runs with no session and resolves purely from the sealed
// bridge cookie, whose payload does not yet carry the owner. Nothing else may
// use it. Once that payload carries the owner, the bridge reads through the
// owner-scoped FindBySessionID above and this method is deleted with it.
func (repo *OIDCLogoutStateRepository) FindBySessionIDUnattributed(ctx context.Context, sessionID string) (models.OIDCLogoutState, bool, error) {
	var state models.OIDCLogoutState
	if err := repo.database.WithContext(ctx).
		Where("session_id = ?", strings.TrimSpace(sessionID)).
		First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.OIDCLogoutState{}, false, nil
		}
		return models.OIDCLogoutState{}, false, err
	}
	return state, true, nil
}

// DeleteBySessionID removes one owner's logout state. Like the lookup it pairs
// the session id with the owner, so a delete can never reach across owners,
// and a zero userID is refused rather than widened to every owner.
func (repo *OIDCLogoutStateRepository) DeleteBySessionID(ctx context.Context, sessionID string, userID uint) error {
	if userID == 0 {
		return ErrOIDCLogoutStateUnattributed
	}
	return repo.database.WithContext(ctx).
		Where("session_id = ? AND user_id = ?", strings.TrimSpace(sessionID), userID).
		Delete(&models.OIDCLogoutState{}).Error
}

// DeleteExpired is the TTL sweep and is deliberately owner-agnostic: it is
// keyed on expiry alone and reads no caller-supplied identifier.
func (repo *OIDCLogoutStateRepository) DeleteExpired(ctx context.Context, cutoff time.Time) error {
	if cutoff.IsZero() {
		cutoff = time.Now().UTC()
	}
	return repo.database.WithContext(ctx).Where("expires_at <= ?", cutoff.UTC()).Delete(&models.OIDCLogoutState{}).Error
}
