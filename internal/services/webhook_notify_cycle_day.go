package services

import (
	"context"
	"fmt"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// NotifyCycleDayReminderStore is the notify pass's seam onto the owner's
// cycle-day rules (*db.CycleDayReminderRepository).
type NotifyCycleDayReminderStore interface {
	ListByUser(ctx context.Context, userID uint) ([]models.CycleDayReminder, error)
	// ClaimSend sets last_sent_on to day by compare-and-set against previous,
	// pinned to the configuration epoch the pass read; false means another pass
	// owns the send or the owner's configuration moved on.
	ClaimSend(ctx context.Context, userID uint, ruleID uint, day time.Time, previous *time.Time, configVersion int) (bool, error)
	// ReleaseSend restores previous after a failed delivery.
	ReleaseSend(ctx context.Context, userID uint, ruleID uint, day time.Time, previous *time.Time) error
}

// WithCycleDayReminders wires the cycle-day rule store into the pass.
func (service *WebhookNotifyService) WithCycleDayReminders(store NotifyCycleDayReminderStore) *WebhookNotifyService {
	service.cycleDayRules = store
	return service
}

// The cycle-day copy keys, whole literals for the locale reachability sweep.
const (
	reminderCycleDayTitleKey          = "webhook.reminder.cycle_day.title"
	reminderCycleDayMessageKey        = "webhook.reminder.cycle_day.message"
	reminderCycleDayLabelMessageKey   = "webhook.reminder.cycle_day.message_label"
	reminderCycleDayHeldTitleKey      = "webhook.reminder.cycle_day_held.title"
	reminderCycleDayHeldMessageKey    = "webhook.reminder.cycle_day_held.message"
	cycleDayReminderPayloadDateFormat = "2006-01-02"
)

// buildCycleDayPayload renders a due cycle-day rule. The label reaches the
// message only when the rule's send_label opt-in is on.
func (service *WebhookNotifyService) buildCycleDayPayload(reminder DueCycleDayReminder, language string, disclaimer string) WebhookPayload {
	message := service.cycleDayMessage(reminder.CycleDay, language)
	if reminder.Rule.SendLabel && reminder.Rule.Label != "" {
		message = service.cycleDayLabelMessage(reminder.CycleDay, reminder.Rule.Label, language)
	}
	return WebhookPayload{
		Title:      service.localized.Message(language, reminderCycleDayTitleKey),
		Message:    message,
		Disclaimer: disclaimer,
		Type:       DueReminderTypeCycleDay,
		EventDate:  reminder.Day.Format(cycleDayReminderPayloadDateFormat),
		CycleDay:   reminder.CycleDay,
	}
}

// cycleDayMessage is the neutral sentence; an unanswered key sends no message
// rather than verb residue.
func (service *WebhookNotifyService) cycleDayMessage(cycleDay int, language string) string {
	template := service.localized.Message(language, reminderCycleDayMessageKey)
	if template == "" {
		return ""
	}
	return fmt.Sprintf(template, cycleDay)
}

// cycleDayLabelMessage is the sentence carrying an opted-in label.
func (service *WebhookNotifyService) cycleDayLabelMessage(cycleDay int, label string, language string) string {
	template := service.localized.Message(language, reminderCycleDayLabelMessageKey)
	if template == "" {
		return ""
	}
	return fmt.Sprintf(template, cycleDay, label)
}

// buildCycleDayHoldPayload renders the neutral "paused" notice: no cycle day,
// no label.
func (service *WebhookNotifyService) buildCycleDayHoldPayload(notice CycleDayHoldNotice, language string, disclaimer string) WebhookPayload {
	return WebhookPayload{
		Title:      service.localized.Message(language, reminderCycleDayHeldTitleKey),
		Message:    service.localized.Message(language, reminderCycleDayHeldMessageKey),
		Disclaimer: disclaimer,
		Type:       DueReminderTypeCycleDayHeld,
		EventDate:  notice.Day.Format(cycleDayReminderPayloadDateFormat),
	}
}
