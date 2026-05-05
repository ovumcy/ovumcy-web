package services

import (
	"errors"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrSettingsPasswordMissing     = errors.New("settings password missing")
	ErrSettingsPasswordInvalid     = errors.New("settings password invalid")
	ErrSettingsLocalPasswordNotSet = errors.New("settings local password not set")
)

type SettingsUserRepository interface {
	UpdateDisplayName(userID uint, displayName string) error
	UpdateRecoveryCodeHash(userID uint, recoveryHash string) error
	UpdatePasswordAndRevokeSessions(userID uint, passwordHash string, mustChangePassword bool) error
	UpdatePasswordRecoveryCodeAndRevokeSessions(userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error
	UpdateByID(userID uint, updates map[string]any) error
	LoadSettingsByID(userID uint) (models.User, error)
	ClearAllDataAndResetSettings(userID uint) error
	DeleteAccountAndRelatedData(userID uint) error
}

type CycleSettingsUpdate struct {
	CycleLength        int
	PeriodLength       int
	AutoPeriodFill     bool
	IrregularCycle     bool
	UnpredictableCycle bool
	AgeGroup           string
	UsageGoal          string
	LastPeriodStartSet bool
	LastPeriodStart    *time.Time
}

type SettingsService struct {
	users SettingsUserRepository
}

func NewSettingsService(users SettingsUserRepository) *SettingsService {
	return &SettingsService{users: users}
}

func (service *SettingsService) UpdateDisplayName(userID uint, displayName string) error {
	return service.users.UpdateDisplayName(userID, displayName)
}

func (service *SettingsService) UpdateRecoveryCodeHash(userID uint, recoveryHash string) error {
	return service.users.UpdateRecoveryCodeHash(userID, recoveryHash)
}

func (service *SettingsService) ValidateCurrentPassword(passwordHash string, rawPassword string) error {
	if strings.TrimSpace(passwordHash) == "" {
		return ErrSettingsLocalPasswordNotSet
	}
	password := strings.TrimSpace(rawPassword)
	if password == "" {
		return ErrSettingsPasswordMissing
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil {
		return ErrSettingsPasswordInvalid
	}
	return nil
}

func (service *SettingsService) SaveCycleSettings(userID uint, settings CycleSettingsUpdate) error {
	updates := map[string]any{
		"cycle_length":        settings.CycleLength,
		"period_length":       settings.PeriodLength,
		"auto_period_fill":    settings.AutoPeriodFill,
		"irregular_cycle":     settings.IrregularCycle,
		"unpredictable_cycle": settings.UnpredictableCycle,
		"age_group":           NormalizeAgeGroup(settings.AgeGroup),
		"usage_goal":          NormalizeUsageGoal(settings.UsageGoal),
	}
	if settings.LastPeriodStartSet {
		if settings.LastPeriodStart == nil {
			updates["last_period_start"] = nil
		} else {
			updates["last_period_start"] = *settings.LastPeriodStart
		}
	}
	return service.users.UpdateByID(userID, updates)
}

func (service *SettingsService) SaveTrackingSettings(userID uint, settings TrackingSettingsUpdate) error {
	return service.users.UpdateByID(userID, map[string]any{
		"track_bbt":              settings.TrackBBT,
		"temperature_unit":       NormalizeTemperatureUnit(settings.TemperatureUnit),
		"track_cervical_mucus":   settings.TrackCervicalMucus,
		"hide_sex_chip":          settings.HideSexChip,
		"hide_cycle_factors":     settings.HideCycleFactors,
		"hide_notes_field":       settings.HideNotesField,
		"show_historical_phases": settings.ShowHistoricalPhases,
	})
}

func (service *SettingsService) LoadSettings(userID uint) (models.User, error) {
	return service.users.LoadSettingsByID(userID)
}

func (service *SettingsService) ClearAllData(userID uint) error {
	return service.users.ClearAllDataAndResetSettings(userID)
}

func (service *SettingsService) DeleteAccount(userID uint) error {
	return service.users.DeleteAccountAndRelatedData(userID)
}
