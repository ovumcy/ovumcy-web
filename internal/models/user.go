package models

import "time"

const (
	RoleOwner           = "owner"
	DefaultCycleLength  = 28
	DefaultPeriodLength = 5
	AgeGroupUnknown     = ""
	AgeGroupUnder20     = "under_20"
	AgeGroup20To35      = "age_20_35"
	AgeGroup35Plus      = "age_35_plus"
	UsageGoalHealth     = "health"
	UsageGoalAvoid      = "avoid_pregnancy"
	UsageGoalTrying     = "trying_to_conceive"
)

type User struct {
	ID                  uint       `gorm:"primaryKey"`
	DisplayName         string     `gorm:"size:80"`
	Email               string     `gorm:"uniqueIndex;not null"`
	PasswordHash        string     `gorm:"not null"`
	RecoveryCodeHash    string     `gorm:"column:recovery_code_hash"`
	LocalAuthEnabled    bool       `gorm:"column:local_auth_enabled;not null"`
	AuthSessionVersion  int        `gorm:"column:auth_session_version;not null;default:1"`
	MustChangePassword  bool       `gorm:"column:must_change_password;not null;default:false"`
	Role                string     `gorm:"not null;default:owner"`
	OnboardingCompleted bool       `gorm:"not null;default:false"`
	CycleLength         int        `gorm:"not null;default:28"`
	PeriodLength        int        `gorm:"not null;default:5"`
	LutealPhase         int        `gorm:"column:luteal_phase;not null;default:14"`
	AutoPeriodFill      bool       `gorm:"column:auto_period_fill;not null;default:true"`
	IrregularCycle      bool       `gorm:"column:irregular_cycle;not null;default:false"`
	TrackBBT            bool       `gorm:"column:track_bbt;not null;default:false"`
	TemperatureUnit     string     `gorm:"column:temperature_unit;not null;default:c"`
	TrackCervicalMucus  bool       `gorm:"column:track_cervical_mucus;not null;default:false"`
	HideSexChip         bool       `gorm:"column:hide_sex_chip;not null;default:false"`
	HideCycleFactors    bool       `gorm:"column:hide_cycle_factors;not null;default:false"`
	HideNotesField      bool       `gorm:"column:hide_notes_field;not null;default:false"`
	ShowHistoricalPhases bool      `gorm:"column:show_historical_phases;not null;default:false"`
	ShownPeriodTip      bool       `gorm:"column:shown_period_tip;not null;default:false"`
	AgeGroup            string     `gorm:"column:age_group;not null;default:''"`
	UsageGoal           string     `gorm:"column:usage_goal;not null;default:health"`
	UnpredictableCycle  bool       `gorm:"column:unpredictable_cycle;not null;default:false"`
	LongPeriodWarnedAt  *time.Time `gorm:"column:long_period_warning_cycle_start;type:date"`
	LastPeriodStart     *time.Time `gorm:"type:date"`
	CreatedAt           time.Time  `gorm:"not null"`
	TOTPSecret          string     `gorm:"column:totp_secret"`
	TOTPEnabled         bool       `gorm:"column:totp_enabled;not null;default:false"`
}
