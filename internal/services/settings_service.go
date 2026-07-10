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
	UpdateUserTimezone(ctx context.Context, userID uint, timezone string) error
	UpdateReminderLeadDays(ctx context.Context, userID uint, leadDays int) error
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

// PersistTimezone stores the owner's IANA timezone name, scoped to userID, but
// only when it differs from the value already persisted. currentTimezone is the
// value loaded on the authenticated user; newTimezone must be an IANA name the
// caller has already validated with the shared request-timezone parser (the
// transport layer resolves and validates it before calling this). When the two
// match, no DB UPDATE is issued so the common per-request path stays read-only.
// Returns true when a write occurred.
func (service *SettingsService) PersistTimezone(ctx context.Context, userID uint, currentTimezone string, newTimezone string) (bool, error) {
	if newTimezone == "" || newTimezone == currentTimezone {
		return false, nil
	}
	if err := service.users.UpdateUserTimezone(ctx, userID, newTimezone); err != nil {
		return false, err
	}
	return true, nil
}

// SettingsReminderUpdatedStatus is the flash status emitted after a successful
// reminder-lead-days save (always the same outcome).
const SettingsReminderUpdatedStatus = "reminders_updated"

// SaveReminderLeadDays persists the owner's shared reminder lead window
// (users.reminder_lead_days, issue #123) scoped to userID. The raw value is
// clamped into [MinReminderLeadDays, MaxReminderLeadDays] via the SAME
// NormalizeReminderLeadDays helper the webhook-settings save path uses, so both
// the standalone control and the webhook bundle share one 0–14 bound and an
// out-of-range value is clamped, never rejected. currentLeadDays is the value
// already persisted on the authenticated user; when the clamped value matches
// it, no DB UPDATE is issued so a resubmit of the same value is a read-only
// no-op (mirroring PersistTimezone). Returns true when a write occurred. It
// deliberately does not bump auth_session_version — a reminder preference is
// not a change to the account's security posture.
func (service *SettingsService) SaveReminderLeadDays(ctx context.Context, userID uint, currentLeadDays int, rawLeadDays int) (bool, error) {
	clamped := NormalizeReminderLeadDays(rawLeadDays)
	if clamped == NormalizeReminderLeadDays(currentLeadDays) {
		return false, nil
	}
	if err := service.users.UpdateReminderLeadDays(ctx, userID, clamped); err != nil {
		return false, err
	}
	return true, nil
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
		"hide_sex_chip":          settings.HideSexChip,
		"hide_cycle_factors":     settings.HideCycleFactors,
		"hide_notes_field":       settings.HideNotesField,
		"show_historical_phases": settings.ShowHistoricalPhases,
		"week_starts_on":         NormalizeWeekStart(settings.WeekStartsOn),
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
