package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// RegisterPickupTokenRepository persists and consumes the nonces that back the
// sealed `ovumcy_register_pickup` cookie. The contract is single-use: every
// successful Consume() invocation marks the row consumed in the same UPDATE,
// so a captured cookie cannot be replayed to mint a second auth session
// inside the 5-minute TTL.
type RegisterPickupTokenRepository struct {
	database *gorm.DB
}

func NewRegisterPickupTokenRepository(database *gorm.DB) *RegisterPickupTokenRepository {
	return &RegisterPickupTokenRepository{database: database}
}

// Issue inserts a fresh pickup token. The nonce must be unique; callers
// should source it from a 16-byte CSPRNG draw. ExpiresAt is treated as the
// hard cap for the matching cookie's TTL.
func (repo *RegisterPickupTokenRepository) Issue(ctx context.Context, nonce string, userID uint, expiresAt time.Time) error {
	trimmed := strings.TrimSpace(nonce)
	if trimmed == "" {
		return errors.New("register_pickup_tokens: nonce is required")
	}
	if userID == 0 {
		return errors.New("register_pickup_tokens: user id is required")
	}
	if expiresAt.IsZero() {
		return errors.New("register_pickup_tokens: expires_at is required")
	}

	row := &models.RegisterPickupToken{
		Nonce:     trimmed,
		UserID:    userID,
		ExpiresAt: expiresAt.UTC(),
		CreatedAt: time.Now().UTC(),
	}
	return repo.database.WithContext(ctx).Create(row).Error
}

// Consume atomically marks the row identified by nonce as consumed and
// returns the original user_id. A row that does not exist, has already been
// consumed, or has expired returns (0, false, nil) — callers MUST treat all
// three cases the same way to keep replay attempts indistinguishable from
// missing/expired cookies and decoys.
func (repo *RegisterPickupTokenRepository) Consume(ctx context.Context, nonce string, now time.Time) (uint, bool, error) {
	trimmed := strings.TrimSpace(nonce)
	if trimmed == "" {
		return 0, false, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	var userID uint
	err := repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var row models.RegisterPickupToken
		if err := tx.Where("nonce = ?", trimmed).First(&row).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		result := tx.Model(&models.RegisterPickupToken{}).
			Where("nonce = ? AND consumed_at IS NULL AND expires_at > ?", trimmed, now.UTC()).
			Update("consumed_at", now.UTC())
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nil
		}
		userID = row.UserID
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	if userID == 0 {
		return 0, false, nil
	}
	return userID, true, nil
}

// DeleteExpired drops rows whose expires_at is at or before cutoff. Callers
// can run this periodically; the table is otherwise bounded only by the
// register rate limit and the 5-minute TTL.
func (repo *RegisterPickupTokenRepository) DeleteExpired(ctx context.Context, cutoff time.Time) error {
	if cutoff.IsZero() {
		cutoff = time.Now().UTC()
	}
	return repo.database.WithContext(ctx).Where("expires_at <= ?", cutoff.UTC()).Delete(&models.RegisterPickupToken{}).Error
}
