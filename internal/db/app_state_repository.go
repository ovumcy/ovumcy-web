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

// AppStateRepository persists the process-level key/value markers in app_state
// (migration 028). It holds runtime bookkeeping only — never per-owner health
// data — so its operations are unscoped by user_id. Its sole consumer today is
// the built-in reminder scheduler (issue #125), which reads and writes
// last_reminder_run_date for restart safety and current-day catch-up.
type AppStateRepository struct {
	database *gorm.DB
}

func NewAppStateRepository(database *gorm.DB) *AppStateRepository {
	return &AppStateRepository{database: database}
}

// Get returns the stored value for key and whether a row exists. A missing key
// yields ("", false, nil) — the caller distinguishes "never written" (fresh
// install / no prior run) from any value.
func (repo *AppStateRepository) Get(ctx context.Context, key string) (string, bool, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", false, nil
	}

	var row models.AppState
	if err := repo.database.WithContext(ctx).Where("key = ?", trimmed).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return row.Value, true, nil
}

// Set upserts value for key, stamping updated_at. The ON CONFLICT (key) update
// makes a repeated write for the same key overwrite in place, so the scheduler's
// "ran today" marker is a single evolving row rather than an append. It is the
// only writer of these rows.
func (repo *AppStateRepository) Set(ctx context.Context, key string, value string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return errors.New("app_state: key is required")
	}

	row := &models.AppState{
		Key:       trimmed,
		Value:     value,
		UpdatedAt: time.Now().UTC(),
	}
	return repo.database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(row).Error
}
