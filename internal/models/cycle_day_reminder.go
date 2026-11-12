package models

import "time"

const (
	// CycleDayReminderMinDay and CycleDayReminderMaxDay bound a rule's cycle day;
	// 90 is the longest cycle length the app accepts anywhere.
	CycleDayReminderMinDay = 1
	CycleDayReminderMaxDay = 90
	// CycleDayReminderMaxPerOwner caps the rules one owner may hold.
	CycleDayReminderMaxPerOwner = 10
	// CycleDayReminderLabelMaxRunes bounds the owner-entered label.
	CycleDayReminderLabelMaxRunes = 64
	// DefaultCycleDayReminderHour is the owner-local hour a new rule sends from.
	DefaultCycleDayReminderHour = 9
)

// CycleDayReminder is one owner-defined webhook reminder on a fixed cycle day
// (migration 046). Label is owner-only text: it reaches a webhook payload only
// when SendLabel is true.
type CycleDayReminder struct {
	ID           uint       `gorm:"primaryKey"`
	UserID       uint       `gorm:"column:user_id;not null;index"`
	CycleDay     int        `gorm:"column:cycle_day;not null"`
	RemindAtHour int        `gorm:"column:remind_at_hour;not null"`
	Label        string     `gorm:"column:label;not null"`
	SendLabel    bool       `gorm:"column:send_label;not null"`
	LastSentOn   *time.Time `gorm:"column:last_sent_on;type:date"`
	CreatedAt    time.Time  `gorm:"column:created_at"`
}

// TableName pins the table name the migration created.
func (CycleDayReminder) TableName() string {
	return "cycle_day_reminders"
}

// CycleDayReminderInput is the owner-editable part of a rule, validated by the
// settings service before any write.
type CycleDayReminderInput struct {
	CycleDay     int
	RemindAtHour int
	Label        string
	SendLabel    bool
}
