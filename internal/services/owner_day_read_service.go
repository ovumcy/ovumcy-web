package services

import (
	"context"
	"errors"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// ErrDayReadOwnerRequired is returned when a read is asked for with no resolved
// owner. Every caller sits behind the session middleware, so this is a
// precondition rather than a flow: it exists so that the path which is
// currently unreachable fails as an error instead of a panic, and so that it
// can never be answered with empty data — a read scoped to a missing id must
// refuse, not report that the owner has nothing.
var ErrDayReadOwnerRequired = errors.New("day read requires a resolved owner")

type OwnerDayReader interface {
	FetchLogByDate(ctx context.Context, userID uint, day time.Time, location *time.Location) (models.DailyLog, error)
	FetchLogsForUser(ctx context.Context, userID uint, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error)
}

type OwnerSymptomReader interface {
	FetchPickerSymptoms(ctx context.Context, userID uint, selectedIDs []uint) ([]models.SymptomType, error)
}

// OwnerDayReadService is the day-read seam between internal/api and the day and
// symptom repositories. It exists as a boundary, not as a decision: internal/api
// is transport-only and may never hold a repository handle, so the thinness of
// the methods below is the point of them rather than an argument against them.
//
// It was called ViewerService until this rename, and each method carried a
// ForViewer suffix. That name outlived its subject — release 1.4.0 removed the
// never-shipped non-owner "viewer" sanitization path — and what it left behind
// named a role the product declares absent: docs/architecture.md states there is
// no viewer or partner role, and internal/models declares exactly one, RoleOwner.
// Every method here takes the acting owner and reads nothing but user.ID, so
// "owner" is what the names now say. The barrier that keeps the old name from
// coming back is absent_role_naming_barrier_test.go.
type OwnerDayReadService struct {
	days     OwnerDayReader
	symptoms OwnerSymptomReader
}

func NewOwnerDayReadService(days OwnerDayReader, symptoms OwnerSymptomReader) *OwnerDayReadService {
	return &OwnerDayReadService{
		days:     days,
		symptoms: symptoms,
	}
}

// The three methods below are every site in this file that reads user.ID, and
// each refuses before it does: the guard belongs where the dereference is, not
// on one method of the three. FetchDayLogForOwner needs none of its own — it
// holds no id and its first call is one of these.

func (service *OwnerDayReadService) FetchSymptomsForOwner(ctx context.Context, user *models.User, selectedIDs []uint) ([]models.SymptomType, error) {
	if user == nil {
		return nil, ErrDayReadOwnerRequired
	}
	return service.symptoms.FetchPickerSymptoms(ctx, user.ID, selectedIDs)
}

func (service *OwnerDayReadService) FetchLogsForOwner(ctx context.Context, user *models.User, from time.Time, to time.Time, location *time.Location) ([]models.DailyLog, error) {
	if user == nil {
		return nil, ErrDayReadOwnerRequired
	}
	return service.days.FetchLogsForUser(ctx, user.ID, from, to, location)
}

func (service *OwnerDayReadService) FetchLogByDateForOwner(ctx context.Context, user *models.User, day time.Time, location *time.Location) (models.DailyLog, error) {
	if user == nil {
		return models.DailyLog{}, ErrDayReadOwnerRequired
	}
	return service.days.FetchLogByDate(ctx, user.ID, day, location)
}

func (service *OwnerDayReadService) FetchDayLogForOwner(ctx context.Context, user *models.User, day time.Time, location *time.Location) (models.DailyLog, []models.SymptomType, error) {
	logEntry, err := service.FetchLogByDateForOwner(ctx, user, day, location)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	symptoms, err := service.FetchSymptomsForOwner(ctx, user, logEntry.SymptomIDs)
	if err != nil {
		return models.DailyLog{}, nil, err
	}

	return logEntry, symptoms, nil
}
