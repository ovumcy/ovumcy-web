package api

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
	"github.com/terraincognita07/ovumcy/internal/services"
)

func (handler *Handler) fetchSymptomsForViewer(user *models.User) ([]models.SymptomType, error) {
	if !services.ShouldExposeSymptomsForViewer(user) {
		return []models.SymptomType{}, nil
	}
	handler.ensureDependencies()
	return handler.symptomService.FetchSymptoms(user.ID)
}

func (handler *Handler) fetchDayLogForViewer(user *models.User, day time.Time) (models.DailyLog, []models.SymptomType, error) {
	handler.ensureDependencies()
	logEntry, err := handler.dayService.FetchLogByDate(user.ID, day, handler.location)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	symptoms, err := handler.fetchSymptomsForViewer(user)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	return services.SanitizeLogForViewer(user, logEntry), symptoms, nil
}
