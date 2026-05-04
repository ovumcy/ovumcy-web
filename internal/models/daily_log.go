package models

import (
	"time"

	"gorm.io/gorm"
)

const (
	FlowNone     = "none"
	FlowSpotting = "spotting"
	FlowLight    = "light"
	FlowMedium   = "medium"
	FlowHeavy    = "heavy"

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
	ID              uint      `gorm:"primaryKey"`
	UserID          uint      `gorm:"not null;uniqueIndex:uidx_user_date"`
	Date            time.Time `gorm:"type:date;not null;uniqueIndex:uidx_user_date"`
	IsPeriod        bool      `gorm:"not null;default:false"`
	CycleStart      bool      `gorm:"column:cycle_start;not null;default:false"`
	IsUncertain     bool      `gorm:"column:is_uncertain;not null;default:false"`
	Flow            string    `gorm:"not null;default:none"`
	Mood            int       `gorm:"not null;default:0"`
	SexActivity     string    `gorm:"column:sex_activity;not null;default:none"`
	BBT             float64   `gorm:"column:bbt;not null;default:0"`
	CervicalMucus   string    `gorm:"column:cervical_mucus;not null;default:none"`
	CycleFactorKeys []string  `gorm:"column:cycle_factor_keys;serializer:json"`
	SymptomIDs      []uint    `gorm:"serializer:json"`
	Notes           string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (logEntry *DailyLog) BeforeSave(*gorm.DB) error {
	if logEntry.CycleFactorKeys == nil {
		logEntry.CycleFactorKeys = []string{}
	}
	if !logEntry.Date.IsZero() {
		year, month, day := logEntry.Date.Date()
		logEntry.Date = time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	}
	return nil
}
