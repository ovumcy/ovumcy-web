package services

import (
	"context"
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
	UpdateDisplayName(ctx context.Context, userID uint, displayName string) error
	UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, mustChangePassword bool) error
	UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, passwordHash string, recoveryHash string, mustChangePassword bool) error
	UpdateByID(ctx context.Context, userID uint, updates map[string]any) error
	LoadSettingsByID(ctx context.Context, userID uint) (models.User, error)
	ClearAllDataAndResetSettings(ctx context.Context, userID uint) error
	DeleteAccountAndRelatedData(ctx context.Context, userID uint) error
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

func (service *SettingsService) UpdateDisplayName(ctx context.Context, userID uint, displayName string) error {
	return service.users.UpdateDisplayName(ctx, userID, displayName)
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

func (service *SettingsService) SaveCycleSettings(ctx context.Context, userID uint, settings CycleSettingsUpdate) error {
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
	return service.users.UpdateByID(ctx, userID, updates)
}

func (service *SettingsService) SaveTrackingSettings(ctx context.Context, userID uint, settings TrackingSettingsUpdate) error {
	return service.users.UpdateByID(ctx, userID, map[string]any{
		"track_bbt":              settings.TrackBBT,
		"temperature_unit":       NormalizeTemperatureUnit(settings.TemperatureUnit),
		"track_cervical_mucus":   settings.TrackCervicalMucus,
		"track_lh_test":          settings.TrackLHTest,
		"hide_sex_chip":          settings.HideSexChip,
		"hide_cycle_factors":     settings.HideCycleFactors,
		"hide_notes_field":       settings.HideNotesField,
		"show_historical_phases": settings.ShowHistoricalPhases,
	})
}

func (service *SettingsService) LoadSettings(ctx context.Context, userID uint) (models.User, error) {
	return service.users.LoadSettingsByID(ctx, userID)
}

func (service *SettingsService) ClearAllData(ctx context.Context, userID uint) error {
	return service.users.ClearAllDataAndResetSettings(ctx, userID)
}

func (service *SettingsService) DeleteAccount(ctx context.Context, userID uint) error {
	return service.users.DeleteAccountAndRelatedData(ctx, userID)
}
