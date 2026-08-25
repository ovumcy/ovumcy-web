package services

import (
	"context"
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ErrViewerUserRequired is returned when a read is asked for with no resolved
// owner. Every caller sits behind the session middleware, so this is a
// precondition rather than a flow: it exists so that the path which is
// currently unreachable fails as an error instead of a panic, and so that it
// can never be answered with empty data — a read scoped to a missing id must
// refuse, not report that the owner has nothing.
var ErrViewerUserRequired = errors.New("viewer read requires a resolved user")

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

// The three methods below are every site in this file that reads user.ID, and
// each refuses before it does: the guard belongs where the dereference is, not
// on one method of the three. FetchDayLogForViewer needs none of its own — it
// holds no id and its first call is one of these.

func (service *ViewerService) FetchSymptomsForViewer(ctx context.Context, user *models.User, selectedIDs []uint) ([]models.SymptomType, error) {
	if user == nil {
		return nil, ErrViewerUserRequired
	}
	return service.symptoms.FetchPickerSymptoms(ctx, user.ID, selectedIDs)
}

func (service *ViewerService) FetchLogsForViewer(ctx context.Context, user *models.User, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error) {
	if user == nil {
		return nil, ErrViewerUserRequired
	}
	return service.days.FetchLogsForUser(ctx, user.ID, from, to, location)
}

func (service *ViewerService) FetchLogByDateForViewer(ctx context.Context, user *models.User, day time.Time, location *time.Location) (models.DailyLog, error) {
	if user == nil {
		return models.DailyLog{}, ErrViewerUserRequired
	}
	return service.days.FetchLogByDate(ctx, user.ID, day, location)
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
