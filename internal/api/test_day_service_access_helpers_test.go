package api

import (
	"errors"
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
	"gorm.io/gorm"
)

func fetchLogByDateForTest(database *gorm.DB, userID uint, day time.Time, location *time.Location) (models.DailyLog, error) {
	dayStart, dayEnd := services.DayRange(day, location)
	entry := models.DailyLog{}
	err := database.
		Where("user_id = ? AND date >= ? AND date < ?", userID, dayStart, dayEnd).
		Order("date DESC, id DESC").
		First(&entry).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.DailyLog{}, nil
	}
	return entry, err
}
