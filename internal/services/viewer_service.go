package services

import (
	"time"

	"github.com/terraincognita07/ovumcy/internal/models"
)

type ViewerDayReader interface {
	FetchLogByDate(userID uint, day time.Time, location *time.Location) (models.DailyLog, error)
	FetchLogsForUser(userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error)
}

type ViewerSymptomReader interface {
	FetchSymptoms(userID uint) ([]models.SymptomType, error)
}

type ViewerService struct {
	days     ViewerDayReader
	symptoms ViewerSymptomReader
}

func NewViewerService(days ViewerDayReader, symptoms ViewerSymptomReader) *ViewerService {
	return &ViewerService{
		days:     days,
		symptoms: symptoms,
	}
}

func (service *ViewerService) FetchSymptomsForViewer(user *models.User) ([]models.SymptomType, error) {
	if !ShouldExposeSymptomsForViewer(user) {
		return []models.SymptomType{}, nil
	}
	return service.symptoms.FetchSymptoms(user.ID)
}

func (service *ViewerService) FetchLogsForViewer(user *models.User, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error) {
	logs, err := service.days.FetchLogsForUser(user.ID, from, to, location)
	if err != nil {
		return nil, err
	}
	SanitizeLogsForViewer(user, logs)
	return logs, nil
}

func (service *ViewerService) FetchLogByDateForViewer(user *models.User, day time.Time, location *time.Location) (models.DailyLog, error) {
	logEntry, err := service.days.FetchLogByDate(user.ID, day, location)
	if err != nil {
		return models.DailyLog{}, err
	}
	return SanitizeLogForViewer(user, logEntry), nil
}

func (service *ViewerService) FetchDayLogForViewer(user *models.User, day time.Time, location *time.Location) (models.DailyLog, []models.SymptomType, error) {
	logEntry, err := service.FetchLogByDateForViewer(user, day, location)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	symptoms, err := service.FetchSymptomsForViewer(user)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	return logEntry, symptoms, nil
}
