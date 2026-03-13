package models

import "time"

const (
	FlowNone   = "none"
	FlowLight  = "light"
	FlowMedium = "medium"
	FlowHeavy  = "heavy"

	SexActivityNone        = "none"
	SexActivityProtected   = "protected"
	SexActivityUnprotected = "unprotected"

	CervicalMucusNone     = "none"
	CervicalMucusDry      = "dry"
	CervicalMucusMoist    = "moist"
	CervicalMucusCreamy   = "creamy"
	CervicalMucusEggWhite = "eggwhite"
)

type DailyLog struct {
	ID            uint      `gorm:"primaryKey"`
	UserID        uint      `gorm:"not null;uniqueIndex:uidx_user_date"`
	Date          time.Time `gorm:"type:date;not null;uniqueIndex:uidx_user_date"`
	IsPeriod      bool      `gorm:"not null;default:false"`
	CycleStart    bool      `gorm:"column:cycle_start;not null;default:false"`
	IsUncertain   bool      `gorm:"column:is_uncertain;not null;default:false"`
	Flow          string    `gorm:"not null;default:none"`
	Mood          int       `gorm:"not null;default:0"`
	SexActivity   string    `gorm:"column:sex_activity;not null;default:none"`
	BBT           float64   `gorm:"column:bbt;not null;default:0"`
	CervicalMucus string    `gorm:"column:cervical_mucus;not null;default:none"`
	SymptomIDs    []uint    `gorm:"serializer:json"`
	Notes         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
