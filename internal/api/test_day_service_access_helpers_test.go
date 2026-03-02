package api

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func fetchLogsForUserForTest(handler *Handler, userID uint, from time.Time, to time.Time) ([]models.DailyLog, error) {
	handler.ensureDependencies()
	return handler.dayService.FetchLogsForUser(userID, from, to, handler.location)
}

func fetchLogByDateForTest(handler *Handler, userID uint, day time.Time) (models.DailyLog, error) {
	handler.ensureDependencies()
	return handler.dayService.FetchLogByDate(userID, day, handler.location)
}

func dayHasDataForDateForTest(handler *Handler, userID uint, day time.Time) (bool, error) {
	handler.ensureDependencies()
	return handler.dayService.DayHasDataForDate(userID, day, handler.location)
}
