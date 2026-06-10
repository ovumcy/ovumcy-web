package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

type OIDCIdentityRepository struct {
	database *gorm.DB
}

func NewOIDCIdentityRepository(database *gorm.DB) *OIDCIdentityRepository {
	return &OIDCIdentityRepository{database: database}
}

func (repo *OIDCIdentityRepository) FindByIssuerSubject(ctx context.Context, issuer string, subject string) (models.OIDCIdentity, bool, error) {
	var identity models.OIDCIdentity
	if err := repo.database.WithContext(ctx).
		Where("issuer = ? AND subject = ?", strings.TrimSpace(issuer), strings.TrimSpace(subject)).
		First(&identity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.OIDCIdentity{}, false, nil
		}
		return models.OIDCIdentity{}, false, err
	}
	return identity, true, nil
}

func (repo *OIDCIdentityRepository) Create(ctx context.Context, identity *models.OIDCIdentity) error {
	return classifyOIDCIdentityCreateError(repo.database.WithContext(ctx).Create(identity).Error)
}

func (repo *OIDCIdentityRepository) TouchLastUsed(ctx context.Context, identityID uint, usedAt time.Time) error {
	if identityID == 0 {
		return nil
	}
	if usedAt.IsZero() {
		usedAt = time.Now().UTC()
	}
	return repo.database.WithContext(ctx).Model(&models.OIDCIdentity{}).
		Where("id = ?", identityID).
		Update("last_used_at", usedAt).Error
}
