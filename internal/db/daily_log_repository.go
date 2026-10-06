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

// ErrDailyLogOwnerRequired is returned by DailyLogRepository.Save and
// UpdateSymptomIDs when the entry carries no owner. Both scope their write by
// entry.UserID in the query itself (see Save's doc comment); a zero UserID is
// invalid input, not a wildcard — matching Where("user_id = ?", 0) would
// ordinarily just match zero rows and return nil, a silent no-op that looks
// exactly like a successful write of an entry nobody asked to persist.
var ErrDailyLogOwnerRequired = errors.New("daily log entry owner is required")

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
	return logs, repo.markSpottingSymptom(ctx, userID, logs)
}

// markSpottingSymptom resolves, once per read, which of the owner's loaded days
// carry the built-in Spotting symptom, and sets the read-time flag the cycle
// boundary rule consumes. A symptom is a per-owner catalog row (and an archived
// one still labels past days), so the lookup is scoped to the owner and skipped
// when no loaded day names any symptom at all.
func (repo *DailyLogRepository) markSpottingSymptom(ctx context.Context, userID uint, logs []models.DailyLog) error {
	anySymptom := false
	for index := range logs {
		if len(logs[index].SymptomIDs) > 0 {
			anySymptom = true
			break
		}
	}
	if !anySymptom {
		return nil
	}

	var ids []uint
	if err := repo.database.WithContext(ctx).
		Model(&models.SymptomType{}).
		Where("user_id = ? AND is_builtin = ? AND lower(name) = ?", userID, true, "spotting").
		Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}
	spotting := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		spotting[id] = struct{}{}
	}
	for index := range logs {
		for _, id := range logs[index].SymptomIDs {
			if _, ok := spotting[id]; ok {
				logs[index].HasSpottingSymptom = true
				break
			}
		}
	}
	return nil
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
	if err := repo.markSpottingSymptom(ctx, userID, logs); err != nil {
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

func (repo *DailyLogRepository) FindByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	return findDailyLogInDayRange(repo.database.WithContext(ctx), userID, dayStart, dayEnd)
}

// FindByUserAndDayRangeForUpdate is FindByUserAndDayRange for a write that
// merges onto the row it reads: on PostgreSQL the read is SELECT … FOR UPDATE,
// so a concurrent transaction merging onto the same day waits for this one to
// commit and then reads the row it left, instead of both merging onto the same
// old row and the later commit erasing the earlier one's fields. It locks only
// inside a transaction (WithinTransaction).
//
// SQLite has no row lock and the dialect drops the clause; it needs none,
// because its write transactions open with BEGIN IMMEDIATE (`_txlock=immediate`,
// sqlite.go), so a second writer cannot begin its read until the first commits.
//
// A day with no row locks nothing. Two first writes of the same day then both
// insert, and the unique (user_id, date) index refuses the later one — Create
// reports that as a UniqueConstraintError, which the caller answers by
// re-running its transaction to read and merge onto the row that won.
func (repo *DailyLogRepository) FindByUserAndDayRangeForUpdate(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	return findDailyLogInDayRange(repo.database.WithContext(ctx).Clauses(clause.Locking{Strength: clause.LockingStrengthUpdate}), userID, dayStart, dayEnd)
}

func findDailyLogInDayRange(query *gorm.DB, userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	entry := models.DailyLog{}
	result := query.
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

// Create inserts a new day row. A zero entry.UserID is refused rather than
// written: an owner-scoped read or the account-erasure sweep can never
// address a row with no owner, so it would sit unreachable forever instead of
// failing loudly at write time.
//
// A refusal by the unique (user_id, date) index — the day was created by a
// concurrent write after this one read it as absent — is a
// UniqueConstraintError, so a merging writer can tell it from a failed write
// and retry onto the row that exists now.
func (repo *DailyLogRepository) Create(ctx context.Context, entry *models.DailyLog) error {
	if entry.UserID == 0 {
		return ErrDailyLogOwnerRequired
	}
	return classifyUniqueConstraintError(repo.database.WithContext(ctx).Create(entry).Error, "daily_logs(user_id, date)")
}

// importDayInsertBatchSize bounds how many day rows go into a single INSERT.
// CreateInBatches chunks the slice so a large import (up to MaxImportEntries)
// never exceeds the driver's per-statement bound-parameter limit (SQLite's
// SQLITE_MAX_VARIABLE_NUMBER, Postgres' 65535): ~18 columns × 500 rows stays
// well under both.
const importDayInsertBatchSize = 500

// CreateBatch inserts multiple day rows, chunked into statements of
// importDayInsertBatchSize. The JSON import path uses it to write all new days
// at once instead of one INSERT per day. Per-row hooks (BeforeSave) still run,
// so stored dates are normalized to UTC-midnight exactly as with single Create.
// CreateBatch refuses the whole batch when any entry carries a zero UserID,
// for the same reason as Create: a zero-owner row would be unreachable by
// every owner-scoped read and by account erasure.
func (repo *DailyLogRepository) CreateBatch(ctx context.Context, entries []models.DailyLog) error {
	if len(entries) == 0 {
		return nil
	}
	for _, entry := range entries {
		if entry.UserID == 0 {
			return ErrDailyLogOwnerRequired
		}
	}
	return repo.database.WithContext(ctx).CreateInBatches(&entries, importDayInsertBatchSize).Error
}

// Save persists a full update of an already-owned row. Like the read/delete
// methods it is scoped by user_id: the update can only touch a row whose
// user_id matches the entry's own UserID, so a mutated entry.UserID can never
// silently reassign or overwrite another owner's row (defense-in-depth for the
// household multi-owner boundary). Every caller sources entry from a
// user-scoped read first, so a legitimate write always matches its row.
//
// Model(...).Select("*").Updates mirrors gorm.Save's "update all fields
// including zero values" semantics while deliberately omitting Save's
// upsert-on-zero-rows-affected fallback: under the guard a cross-owner attempt
// matches zero rows, and that fallback would otherwise re-create/overwrite the
// row by primary key and defeat the scope.
func (repo *DailyLogRepository) Save(ctx context.Context, entry *models.DailyLog) error {
	if entry.UserID == 0 {
		return ErrDailyLogOwnerRequired
	}
	return repo.database.WithContext(ctx).
		Model(entry).
		Where("user_id = ?", entry.UserID).
		Select("*").
		Updates(entry).Error
}

func (repo *DailyLogRepository) DeleteByUserAndDayRange(ctx context.Context, userID uint, dayStart time.Time, dayEnd time.Time) error {
	return repo.database.WithContext(ctx).Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).Delete(&models.DailyLog{}).Error
}

// ClearLastPeriodStartOn clears users.last_period_start for userID when it
// falls on the calendar day starting at dayStart (a canonical UTC midnight),
// and leaves it alone otherwise. It runs on this repository's handle, so inside
// WithinTransaction it commits or rolls back with the day write that un-marked
// the day. A zero userID is refused, never a reason to skip the scope.
func (repo *DailyLogRepository) ClearLastPeriodStartOn(ctx context.Context, userID uint, dayStart time.Time) error {
	if userID == 0 {
		return errors.New("user id is required")
	}
	return repo.database.WithContext(ctx).Model(&models.User{}).
		Where("id = ? AND last_period_start >= ? AND last_period_start < ?", userID, dayStart, dayStart.AddDate(0, 0, 1)).
		Update("last_period_start", nil).Error
}

// UpdateSymptomIDs updates only the symptom_ids column, scoped by user_id so the
// write can only touch a row whose user_id matches the entry's own UserID
// (defense-in-depth, mirroring Save and the read/delete methods).
func (repo *DailyLogRepository) UpdateSymptomIDs(ctx context.Context, entry *models.DailyLog) error {
	if entry.UserID == 0 {
		return ErrDailyLogOwnerRequired
	}
	return repo.database.WithContext(ctx).Model(entry).Where("user_id = ?", entry.UserID).Select("symptom_ids").Updates(entry).Error
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
