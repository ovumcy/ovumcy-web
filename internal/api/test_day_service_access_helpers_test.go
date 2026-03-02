package api

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

func fetchLogByDateForTest(handler *Handler, userID uint, day time.Time) (models.DailyLog, error) {
	return handler.dayService.FetchLogByDate(userID, day, handler.location)
}
