package db

import (
	"context"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

var (
	ErrCycleDayReminderNotFound = models.ErrCycleDayReminderNotFound
	ErrCycleDayReminderLimit    = models.ErrCycleDayReminderLimit
)

// cycleDayReminderUniqueConstraint names the (user_id, cycle_day,
// remind_at_hour) index for UniqueConstraintError.
const cycleDayReminderUniqueConstraint = "cycle_day_reminders(user_id, cycle_day, remind_at_hour)"

// CycleDayReminderRepository persists the owner's cycle-day reminder rules
// (migration 045). Every query is scoped by user_id, and every write to a rule
// advances users.webhook_config_version in the same transaction: a rule decides
// whether a delivery happens, so a notify pass that read the rule set before an
// edit must lose its claim.
type CycleDayReminderRepository struct {
	database *gorm.DB
}

func NewCycleDayReminderRepository(database *gorm.DB) *CycleDayReminderRepository {
	return &CycleDayReminderRepository{database: database}
}

// ListByUser returns the owner's rules ordered by cycle day, then hour.
func (repo *CycleDayReminderRepository) ListByUser(ctx context.Context, userID uint) ([]models.CycleDayReminder, error) {
	if err := requireUserOwnerID(userID); err != nil {
		return nil, err
	}
	rules := make([]models.CycleDayReminder, 0)
	if err := repo.database.WithContext(ctx).
		Where("user_id = ?", userID).
		Order("cycle_day ASC, remind_at_hour ASC, id ASC").
		Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

// Create inserts a rule for the owner, refusing past the per-owner cap.
func (repo *CycleDayReminderRepository) Create(ctx context.Context, userID uint, input models.CycleDayReminderInput) error {
	if err := requireUserOwnerID(userID); err != nil {
		return err
	}
	return repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// The epoch bump runs first: it locks the owner row on Postgres, so two
		// concurrent creates cannot both pass the count below.
		if err := advanceWebhookConfigVersionTx(tx, userID); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&models.CycleDayReminder{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
			return err
		}
		if count >= models.CycleDayReminderMaxPerOwner {
			return ErrCycleDayReminderLimit
		}
		rule := models.CycleDayReminder{
			UserID:       userID,
			CycleDay:     input.CycleDay,
			RemindAtHour: input.RemindAtHour,
			Label:        input.Label,
			SendLabel:    input.SendLabel,
		}
		return classifyUniqueConstraintError(tx.Create(&rule).Error, cycleDayReminderUniqueConstraint)
	})
}

// Update rewrites a rule's editable fields in place. last_sent_on is kept: an
// edit made after today's send must not re-arm the rule within the same cycle.
func (repo *CycleDayReminderRepository) Update(ctx context.Context, userID uint, ruleID uint, input models.CycleDayReminderInput) error {
	if err := requireUserOwnerID(userID); err != nil {
		return err
	}
	if ruleID == 0 {
		return ErrCycleDayReminderNotFound
	}
	return repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := advanceWebhookConfigVersionTx(tx, userID); err != nil {
			return err
		}
		result := tx.Model(&models.CycleDayReminder{}).
			Where("id = ? AND user_id = ?", ruleID, userID).
			Updates(map[string]any{
				"cycle_day":      input.CycleDay,
				"remind_at_hour": input.RemindAtHour,
				"label":          input.Label,
				"send_label":     input.SendLabel,
			})
		if err := classifyUniqueConstraintError(result.Error, cycleDayReminderUniqueConstraint); err != nil {
			return err
		}
		if result.RowsAffected == 0 {
			return ErrCycleDayReminderNotFound
		}
		return nil
	})
}

// Delete removes one of the owner's rules.
func (repo *CycleDayReminderRepository) Delete(ctx context.Context, userID uint, ruleID uint) error {
	if err := requireUserOwnerID(userID); err != nil {
		return err
	}
	if ruleID == 0 {
		return ErrCycleDayReminderNotFound
	}
	return repo.database.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := advanceWebhookConfigVersionTx(tx, userID); err != nil {
			return err
		}
		result := tx.Where("id = ? AND user_id = ?", ruleID, userID).Delete(&models.CycleDayReminder{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrCycleDayReminderNotFound
		}
		return nil
	})
}

// ClaimSend takes an exclusive claim on one rule's send for day (the owner-local
// calendar day), by compare-and-set on last_sent_on against previous — the value
// the caller's snapshot carried. The claim is lost when the rule moved since that
// snapshot, when the owner's delivery configuration epoch is no longer
// configVersion (a rule edit, an endpoint change, disabling delivery, clear-data),
// or when delivery is disabled. A zero-row outcome is returned as false.
func (repo *CycleDayReminderRepository) ClaimSend(ctx context.Context, userID uint, ruleID uint, day time.Time, previous *time.Time, configVersion int) (bool, error) {
	if err := requireUserOwnerID(userID); err != nil {
		return false, err
	}
	if ruleID == 0 {
		return false, ErrCycleDayReminderNotFound
	}
	claim := repo.database.WithContext(ctx).Model(&models.CycleDayReminder{}).
		Where("id = ? AND user_id = ?", ruleID, userID).
		Where("EXISTS (SELECT 1 FROM users WHERE users.id = cycle_day_reminders.user_id AND users.webhook_config_version = ? AND users.webhook_enabled = ?)", configVersion, true)
	if previous == nil || previous.IsZero() {
		claim = claim.Where("last_sent_on IS NULL")
	} else {
		claim = claim.Where("last_sent_on = ?", canonicalWatermarkAnchor(*previous))
	}
	result := claim.Update("last_sent_on", canonicalWatermarkAnchor(day))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// ReleaseSend restores last_sent_on to previous after the delivery a claim
// covered failed, conditional on the column still holding day.
func (repo *CycleDayReminderRepository) ReleaseSend(ctx context.Context, userID uint, ruleID uint, day time.Time, previous *time.Time) error {
	if err := requireUserOwnerID(userID); err != nil {
		return err
	}
	var restored any
	if previous != nil && !previous.IsZero() {
		restored = canonicalWatermarkAnchor(*previous)
	}
	return repo.database.WithContext(ctx).Model(&models.CycleDayReminder{}).
		Where("id = ? AND user_id = ? AND last_sent_on = ?", ruleID, userID, canonicalWatermarkAnchor(day)).
		Updates(map[string]any{"last_sent_on": restored}).Error
}
