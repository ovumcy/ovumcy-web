package models

import "time"

const (
	RoleOwner           = "owner"
	RolePartner         = "partner"
	DefaultCycleLength  = 28
	DefaultPeriodLength = 5
)

type User struct {
	ID                  uint       `gorm:"primaryKey"`
	DisplayName         string     `gorm:"size:80"`
	Email               string     `gorm:"uniqueIndex;not null"`
	PasswordHash        string     `gorm:"not null"`
	RecoveryCodeHash    string     `gorm:"column:recovery_code_hash"`
	AuthSessionVersion  int        `gorm:"column:auth_session_version;not null;default:1"`
	MustChangePassword  bool       `gorm:"column:must_change_password;not null;default:false"`
	Role                string     `gorm:"not null;default:owner"`
	OnboardingCompleted bool       `gorm:"not null;default:false"`
	CycleLength         int        `gorm:"not null;default:28"`
	PeriodLength        int        `gorm:"not null;default:5"`
	AutoPeriodFill      bool       `gorm:"column:auto_period_fill;not null;default:true"`
	IrregularCycle      bool       `gorm:"column:irregular_cycle;not null;default:false"`
	TrackBBT            bool       `gorm:"column:track_bbt;not null;default:false"`
	TrackCervicalMucus  bool       `gorm:"column:track_cervical_mucus;not null;default:false"`
	HideSexChip         bool       `gorm:"column:hide_sex_chip;not null;default:false"`
	LastPeriodStart     *time.Time `gorm:"type:date"`
	CreatedAt           time.Time  `gorm:"not null"`
}
