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
// data — so its operations are unscoped by user_id. Its consumers are the
// built-in reminder scheduler (issue #125), which reads and writes
// last_reminder_run_date for restart safety and current-day catch-up, and the
// boot-time key-rotation sentinel, which keeps the calendar-feed key epoch
// under calendar_feed_key_epoch, and the calendar-feed restore fence, which
// keeps both its own token and the unanchored marker it erases again.
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
// makes a repeated write for the same key overwrite in place, so each marker is
// a single evolving row rather than an append. Every key has exactly one
// writer (scheduler goroutine or boot sentinel), never two concurrently;
// ProbeWritable's key is upserted only inside a transaction it rolls back.
func (repo *AppStateRepository) Set(ctx context.Context, key string, value string) error {
	return setAppStateTx(repo.database.WithContext(ctx), key, value)
}

// errWriteProbeRollback rolls ProbeWritable's transaction back on success.
var errWriteProbeRollback = errors.New("app_state: write probe rollback")

// ProbeWritable proves the database accepts a write, then rolls it back. SQLite
// opens a file it may not write read-only and serves every read, so a store
// restored or re-owned out of the app user's reach boots clean and fails each
// write later -- after the restore fence's file half has already moved, which
// the next boot reads as a restore (#1321). Nothing is committed.
func (repo *AppStateRepository) ProbeWritable(ctx context.Context) error {
	err := repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := setAppStateTx(tx, models.AppStateKeyWriteProbe, "probe"); err != nil {
			return err
		}
		return errWriteProbeRollback
	})
	if errors.Is(err, errWriteProbeRollback) {
		return nil
	}
	return err
}

// setAppStateTx is Set on a caller's handle — in practice a transaction: the
// restore fence's database half commits inside the transaction of the write it
// protects (runFencedWrite), never in a commit of its own.
func setAppStateTx(tx *gorm.DB, key string, value string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return errors.New("app_state: key is required")
	}

	row := &models.AppState{
		Key:       trimmed,
		Value:     value,
		UpdatedAt: time.Now().UTC(),
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(row).Error
}

// Delete removes the row for key, if there is one. A key that was never written
// is not an error: the only caller that deletes — the calendar-feed restore
// fence, erasing its unanchored marker on the boot that answers for it and its
// own token half on a boot that runs without a usable fence — runs the same
// statement whether or not the row is there, so it never has to read before
// writing and cannot turn an absent row into a failed boot.
// A blank key deletes nothing rather than matching every row, the same
// defensive shape Get gives it; Set is the one operation that refuses a blank
// key outright, because there it would create a row nothing can address.
func (repo *AppStateRepository) Delete(ctx context.Context, key string) error {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return nil
	}
	return repo.database.WithContext(ctx).Where("key = ?", trimmed).Delete(&models.AppState{}).Error
}
