package db

import (
	"context"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

// busyRetryAttempts / busyRetryBackoff bound the application-level retry on
// SQLITE_BUSY. `_txlock=immediate` (see sqlite.go) removes the
// SQLITE_BUSY_SNAPSHOT (517) class that bypasses the busy handler, but under
// heavy concurrent writers the plain SQLITE_BUSY (5) can still surface when the
// busy handler declines to wait to avoid a write-lock livelock. A short bounded
// retry of the whole transaction absorbs that residue so concurrent day writes
// never surface a 500. Non-BUSY errors are never retried.
const (
	busyRetryAttempts = 6
	busyRetryBackoff  = 5 * time.Millisecond
)

// isSQLiteBusy reports whether err is a SQLITE_BUSY / "database is locked"
// contention error that is safe to retry with a fresh transaction.
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(strings.ToLower(msg), "database is locked")
}

type DailyLogRepository struct {
	database *gorm.DB
}

func NewDailyLogRepository(database *gorm.DB) *DailyLogRepository {
	return &DailyLogRepository{database: database}
}

func (repo *DailyLogRepository) ListByUser(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	if err := repo.database.WithContext(ctx).Where("user_id = ?", userID).Order("date ASC, id ASC").Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (repo *DailyLogRepository) ListByUserRange(ctx context.Context, userID uint, fromStart *time.Time, toEnd *time.Time) ([]models.DailyLog, error) {
	query := repo.database.WithContext(ctx).Model(&models.DailyLog{}).Where("user_id = ?", userID)
	if fromStart != nil {
		query = query.Where("date >= ?", *fromStart)
	}
	if toEnd != nil {
		query = query.Where("date < ?", *toEnd)
	}

	logs := make([]models.DailyLog, 0)
	if err := query.Order("date ASC, id ASC").Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (repo *DailyLogRepository) ListByUserDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	if err := repo.database.WithContext(ctx).
		Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).
		Order("date DESC, id DESC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (repo *DailyLogRepository) ListPeriodDays(ctx context.Context, userID uint) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	if err := repo.database.WithContext(ctx).
		Select("date", "is_period", "cycle_start", "is_uncertain").
		Where("user_id = ? AND is_period = ?", userID, true).
		Order("date ASC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (repo *DailyLogRepository) FindByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	entry := models.DailyLog{}
	result := repo.database.WithContext(ctx).
		Select(
			"id",
			"user_id",
			"date",
			"is_period",
			"cycle_start",
			"is_uncertain",
			"flow",
			"mood",
			"sex_activity",
			"bbt",
			"cervical_mucus",
			"pregnancy_test",
			"cycle_factor_keys",
			"symptom_ids",
			"notes",
			"created_at",
			"updated_at",
		).
		Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).
		Order("date DESC, id DESC").
		Limit(1).
		Find(&entry)
	if result.Error != nil {
		return models.DailyLog{}, false, result.Error
	}
	if result.RowsAffected == 0 {
		return models.DailyLog{}, false, nil
	}
	return entry, true, nil
}

func (repo *DailyLogRepository) Create(ctx context.Context, entry *models.DailyLog) error {
	return repo.database.WithContext(ctx).Create(entry).Error
}

func (repo *DailyLogRepository) Save(ctx context.Context, entry *models.DailyLog) error {
	return repo.database.WithContext(ctx).Save(entry).Error
}

func (repo *DailyLogRepository) DeleteByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) error {
	return repo.database.WithContext(ctx).Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).Delete(&models.DailyLog{}).Error
}

func (repo *DailyLogRepository) UpdateSymptomIDs(ctx context.Context, entry *models.DailyLog) error {
	return repo.database.WithContext(ctx).Model(entry).Select("symptom_ids").Updates(entry).Error
}

// WithinTransaction runs fn against a transaction-scoped repository bound to a
// single DB transaction. The provided repository must be used for all reads and
// writes inside fn so they commit or roll back atomically.
//
// The transaction is retried a bounded number of times on SQLITE_BUSY (see
// busyRetryAttempts); each retry runs fn again against a fresh transaction, so
// fn must be idempotent with respect to its own reads (the day upsert is: it
// re-reads before writing). Non-BUSY errors return immediately.
func (repo *DailyLogRepository) WithinTransaction(ctx context.Context, fn func(*DailyLogRepository) error) error {
	var err error
	for attempt := range busyRetryAttempts {
		err = repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return fn(&DailyLogRepository{database: tx})
		})
		if !isSQLiteBusy(err) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(busyRetryBackoff * time.Duration(attempt+1)):
		}
	}
	return err
}
