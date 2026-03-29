package db

import (
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

func (repo *OIDCLogoutStateRepository) Save(state *models.OIDCLogoutState) error {
	if state == nil {
		return nil
	}

	now := time.Now().UTC()
	if state.CreatedAt.IsZero() {
		state.CreatedAt = now
	}
	state.UpdatedAt = now

	return repo.database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "session_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"end_session_endpoint", "id_token_hint", "post_logout_redirect_url", "expires_at", "updated_at"}),
	}).Create(state).Error
}

func (repo *OIDCLogoutStateRepository) FindBySessionID(sessionID string) (models.OIDCLogoutState, bool, error) {
	var state models.OIDCLogoutState
	if err := repo.database.
		Where("session_id = ?", strings.TrimSpace(sessionID)).
		First(&state).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.OIDCLogoutState{}, false, nil
		}
		return models.OIDCLogoutState{}, false, err
	}
	return state, true, nil
}

func (repo *OIDCLogoutStateRepository) DeleteBySessionID(sessionID string) error {
	return repo.database.Where("session_id = ?", strings.TrimSpace(sessionID)).Delete(&models.OIDCLogoutState{}).Error
}

func (repo *OIDCLogoutStateRepository) DeleteExpired(cutoff time.Time) error {
	if cutoff.IsZero() {
		cutoff = time.Now().UTC()
	}
	return repo.database.Where("expires_at <= ?", cutoff.UTC()).Delete(&models.OIDCLogoutState{}).Error
}
