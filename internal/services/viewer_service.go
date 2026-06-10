package services

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

type ViewerDayReader interface {
	FetchLogByDate(ctx context.Context, userID uint, day time.Time, location *time.Location) (models.DailyLog, error)
	FetchLogsForUser(ctx context.Context, userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error)
}

type ViewerSymptomReader interface {
	FetchPickerSymptoms(ctx context.Context, userID uint, selectedIDs []uint) ([]models.SymptomType, error)
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

func (service *ViewerService) FetchSymptomsForViewer(ctx context.Context, user *models.User, selectedIDs []uint) ([]models.SymptomType, error) {
	if !ShouldExposeSymptomsForViewer(user) {
		return []models.SymptomType{}, nil
	}
	return service.symptoms.FetchPickerSymptoms(ctx, user.ID, selectedIDs)
}

func (service *ViewerService) FetchLogsForViewer(ctx context.Context, user *models.User, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error) {
	logs, err := service.days.FetchLogsForUser(ctx, user.ID, from, to, location)
	if err != nil {
		return nil, err
	}
	SanitizeLogsForViewer(user, logs)
	return logs, nil
}

func (service *ViewerService) FetchLogByDateForViewer(ctx context.Context, user *models.User, day time.Time, location *time.Location) (models.DailyLog, error) {
	logEntry, err := service.days.FetchLogByDate(ctx, user.ID, day, location)
	if err != nil {
		return models.DailyLog{}, err
	}
	return SanitizeLogForViewer(user, logEntry), nil
}

func (service *ViewerService) FetchDayLogForViewer(ctx context.Context, user *models.User, day time.Time, location *time.Location) (models.DailyLog, []models.SymptomType, error) {
	logEntry, err := service.FetchLogByDateForViewer(ctx, user, day, location)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	symptoms, err := service.FetchSymptomsForViewer(ctx, user, logEntry.SymptomIDs)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	return logEntry, symptoms, nil
}
