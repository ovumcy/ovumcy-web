package db

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"gorm.io/gorm"
)

type DailyLogRepository struct {
	database *gorm.DB
}

func NewDailyLogRepository(database *gorm.DB) *DailyLogRepository {
	return &DailyLogRepository{database: database}
}

func (repo *DailyLogRepository) ListByUser(userID uint) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	if err := repo.database.Where("user_id = ?", userID).Order("date ASC, id ASC").Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (repo *DailyLogRepository) ListByUserRange(userID uint, fromStart *time.Time, toEnd *time.Time) ([]models.DailyLog, error) {
	query := repo.database.Model(&models.DailyLog{}).Where("user_id = ?", userID)
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

func (repo *DailyLogRepository) ListByUserDayRange(userID uint, dayStart time.Time, dayEnd time.Time) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	if err := repo.database.
		Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).
		Order("date DESC, id DESC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (repo *DailyLogRepository) ListPeriodDays(userID uint) ([]models.DailyLog, error) {
	logs := make([]models.DailyLog, 0)
	if err := repo.database.
		Select("date", "is_period", "cycle_start").
		Where("user_id = ? AND is_period = ?", userID, true).
		Order("date ASC").
		Find(&logs).Error; err != nil {
		return nil, err
	}
	return logs, nil
}

func (repo *DailyLogRepository) ClearCycleStartsExcept(userID uint, dayStart time.Time, dayEnd time.Time) error {
	return repo.database.
		Model(&models.DailyLog{}).
		Where("user_id = ? AND cycle_start = ? AND NOT (date >= ? AND date < ?)", userID, true, dayStart, dayEnd).
		Update("cycle_start", false).
		Error
}

func (repo *DailyLogRepository) FindByUserAndDayRange(userID uint, dayStart time.Time, dayEnd time.Time) (models.DailyLog, bool, error) {
	entry := models.DailyLog{}
	result := repo.database.
		Select(
			"id",
			"user_id",
			"date",
			"is_period",
			"cycle_start",
			"flow",
			"mood",
			"sex_activity",
			"bbt",
			"cervical_mucus",
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

func (repo *DailyLogRepository) Create(entry *models.DailyLog) error {
	return repo.database.Create(entry).Error
}

func (repo *DailyLogRepository) Save(entry *models.DailyLog) error {
	return repo.database.Save(entry).Error
}

func (repo *DailyLogRepository) DeleteByUserAndDayRange(userID uint, dayStart time.Time, dayEnd time.Time) error {
	return repo.database.Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).Delete(&models.DailyLog{}).Error
}

func (repo *DailyLogRepository) UpdateSymptomIDs(entry *models.DailyLog) error {
	return repo.database.Model(entry).Select("symptom_ids").Updates(entry).Error
}
