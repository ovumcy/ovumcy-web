package services

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

var (
	ErrCycleDayReminderNotFound     = models.ErrCycleDayReminderNotFound
	ErrCycleDayReminderLimit        = models.ErrCycleDayReminderLimit
	ErrCycleDayReminderInvalidDay   = errors.New("invalid cycle-day reminder day")
	ErrCycleDayReminderInvalidHour  = errors.New("invalid cycle-day reminder hour")
	ErrCycleDayReminderInvalidLabel = errors.New("invalid cycle-day reminder label")
	ErrCycleDayReminderDuplicate    = errors.New("duplicate cycle-day reminder")
	ErrCycleDayReminderOwner        = errors.New("cycle-day reminder owner is required")
)

// The settings status keys a successful cycle-day reminder write reports.
const (
	SettingsCycleDayReminderAddedStatus   = "cycle_day_reminder_added"
	SettingsCycleDayReminderSavedStatus   = "cycle_day_reminder_saved"
	SettingsCycleDayReminderRemovedStatus = "cycle_day_reminder_removed"
)

// CycleDayReminderStore is the persistence seam (*db.CycleDayReminderRepository).
type CycleDayReminderStore interface {
	ListByUser(ctx context.Context, userID uint) ([]models.CycleDayReminder, error)
	Create(ctx context.Context, userID uint, input models.CycleDayReminderInput) error
	Update(ctx context.Context, userID uint, ruleID uint, input models.CycleDayReminderInput) error
	Delete(ctx context.Context, userID uint, ruleID uint) error
}

// CycleDayReminderService owns validation of the owner's cycle-day reminder
// rules (WEB-383). Every write is owner-scoped by the store.
type CycleDayReminderService struct {
	store CycleDayReminderStore
}

func NewCycleDayReminderService(store CycleDayReminderStore) *CycleDayReminderService {
	return &CycleDayReminderService{store: store}
}

// List returns the owner's rules.
func (service *CycleDayReminderService) List(ctx context.Context, userID uint) ([]models.CycleDayReminder, error) {
	if userID == 0 {
		return nil, ErrCycleDayReminderOwner
	}
	return service.store.ListByUser(ctx, userID)
}

// Create validates and stores a new rule.
func (service *CycleDayReminderService) Create(ctx context.Context, userID uint, input models.CycleDayReminderInput) error {
	if userID == 0 {
		return ErrCycleDayReminderOwner
	}
	normalized, err := NormalizeCycleDayReminderInput(input)
	if err != nil {
		return err
	}
	return mapCycleDayReminderWriteError(service.store.Create(ctx, userID, normalized))
}

// Update validates and rewrites one of the owner's rules.
func (service *CycleDayReminderService) Update(ctx context.Context, userID uint, ruleID uint, input models.CycleDayReminderInput) error {
	if userID == 0 {
		return ErrCycleDayReminderOwner
	}
	if ruleID == 0 {
		return ErrCycleDayReminderNotFound
	}
	normalized, err := NormalizeCycleDayReminderInput(input)
	if err != nil {
		return err
	}
	return mapCycleDayReminderWriteError(service.store.Update(ctx, userID, ruleID, normalized))
}

// Delete removes one of the owner's rules.
func (service *CycleDayReminderService) Delete(ctx context.Context, userID uint, ruleID uint) error {
	if userID == 0 {
		return ErrCycleDayReminderOwner
	}
	if ruleID == 0 {
		return ErrCycleDayReminderNotFound
	}
	return service.store.Delete(ctx, userID, ruleID)
}

// NormalizeCycleDayReminderInput bounds the day and hour, trims the label and
// refuses control characters in it. A rule without a label cannot opt into
// sending one.
func NormalizeCycleDayReminderInput(input models.CycleDayReminderInput) (models.CycleDayReminderInput, error) {
	if input.CycleDay < models.CycleDayReminderMinDay || input.CycleDay > models.CycleDayReminderMaxDay {
		return models.CycleDayReminderInput{}, ErrCycleDayReminderInvalidDay
	}
	if input.RemindAtHour < 0 || input.RemindAtHour > 23 {
		return models.CycleDayReminderInput{}, ErrCycleDayReminderInvalidHour
	}
	label := strings.TrimSpace(input.Label)
	if !utf8.ValidString(label) || utf8.RuneCountInString(label) > models.CycleDayReminderLabelMaxRunes {
		return models.CycleDayReminderInput{}, ErrCycleDayReminderInvalidLabel
	}
	for _, r := range label {
		if unicode.IsControl(r) {
			return models.CycleDayReminderInput{}, ErrCycleDayReminderInvalidLabel
		}
	}
	input.Label = label
	input.SendLabel = input.SendLabel && label != ""
	return input, nil
}

type cycleDayReminderUniqueConstraintError interface {
	UniqueConstraint() string
}

func mapCycleDayReminderWriteError(err error) error {
	var unique cycleDayReminderUniqueConstraintError
	if errors.As(err, &unique) {
		return ErrCycleDayReminderDuplicate
	}
	return err
}
