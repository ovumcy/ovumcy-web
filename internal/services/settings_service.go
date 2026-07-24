package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrSettingsPasswordMissing     = errors.New("settings password missing")
	ErrSettingsPasswordInvalid     = errors.New("settings password invalid")
	ErrSettingsLocalPasswordNotSet = errors.New("settings local password not set")
	// ErrSettingsReauthRateLimited is returned once the per-account re-auth
	// budget is spent. It is deliberately returned BEFORE the password is
	// compared, so an exhausted budget refuses even the correct password.
	ErrSettingsReauthRateLimited = errors.New("settings reauth rate limited")
)

const (
	// The re-auth budget mirrors totp.disable rather than login: both guard a
	// password check that an attacker can only reach with a session already in
	// hand, so the budget is tighter than the 8/15min login budget.
	DefaultSettingsReauthAttemptsLimit  = 5
	DefaultSettingsReauthAttemptsWindow = 15 * time.Minute
)

// ReauthAttempt carries the identity a re-auth password check is budgeted
// against. ClientKey is the resolved client IP (the same spoof-proof value the
// edge limiters key on) and UserID scopes the second, HMAC-derived bucket, so
// rotating source addresses cannot buy a fresh budget for the same account.
type ReauthAttempt struct {
	ClientKey string
	UserID    uint
	Now       time.Time
}

func (attempt ReauthAttempt) identity() string {
	if attempt.UserID == 0 {
		return ""
	}
	return fmt.Sprintf("user:%d", attempt.UserID)
}

// clientBucket scopes the client-keyed bucket to the account as well, so the two
// buckets differ only in whether the source address is part of the key.
//
// The plain client key would be wrong here. Unlike login, re-auth is reachable
// only with a session for one specific account already in hand, so an attacker
// cannot spread guesses across accounts and an address-wide bucket buys no extra
// protection. It does cause harm: on a household instance several independent
// owners share one NAT address, and one owner mistyping their password would
// lock the others out of their own erasure and password-change flows.
//
// Keying (address, account) keeps a per-address budget against one account while
// the account-wide identity bucket still caps an attacker who rotates addresses.
func (attempt ReauthAttempt) clientBucket() string {
	identity := attempt.identity()
	if identity == "" {
		return attempt.ClientKey
	}
	return attempt.ClientKey + "|" + identity
}

func (attempt ReauthAttempt) at() time.Time {
	if attempt.Now.IsZero() {
		return time.Now()
	}
	return attempt.Now
}

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
	// reauthPolicy budgets the password re-authentication that gates erasure
	// and password change. It is created here rather than injected so the
	// budget is in force for every SettingsService, wired or not — a missing
	// bootstrap call degrades to a private limiter and IP-only keying instead
	// of silently removing the control.
	reauthPolicy    *AuthAttemptPolicy
	reauthSecretKey []byte
}

func NewSettingsService(users SettingsUserRepository) *SettingsService {
	return &SettingsService{
		users: users,
		reauthPolicy: NewAuthAttemptPolicy(
			"settings.reauth",
			nil,
			DefaultSettingsReauthAttemptsLimit,
			DefaultSettingsReauthAttemptsWindow,
		),
	}
}

// ConfigureReauthAttempts attaches the shared attempt limiter and the secret key
// used to derive the per-account bucket, and applies operator-configured limits.
// Call it from bootstrap so the re-auth budget shares state with the other auth
// policies; without it the service still enforces the default budget, but only
// against the client key.
func (service *SettingsService) ConfigureReauthAttempts(secretKey []byte, limiter *AttemptLimiter, attempts int, window time.Duration) {
	service.reauthSecretKey = secretKey
	// NewAuthAttemptPolicy substitutes a private limiter when limiter is nil, so
	// a single unconditional rebuild covers both the wired and unwired cases.
	service.reauthPolicy = NewAuthAttemptPolicy("settings.reauth", limiter, attempts, window)
}

// VerifyReauthPassword is the budgeted form of ValidateCurrentPassword: the one
// entry point every password-gated settings action must use. The budget is
// checked before the compare, so an exhausted budget refuses the correct
// password too; only a genuinely wrong password counts as a failure, since a
// blank submission or an account without local auth is a client error rather
// than a guess.
func (service *SettingsService) VerifyReauthPassword(attempt ReauthAttempt, passwordHash string, rawPassword string) error {
	now := attempt.at()
	identity := attempt.identity()
	if service.reauthPolicy.TooManyRecent(service.reauthSecretKey, attempt.clientBucket(), identity, now) {
		return ErrSettingsReauthRateLimited
	}
	if err := service.ValidateCurrentPassword(passwordHash, rawPassword); err != nil {
		if errors.Is(err, ErrSettingsPasswordInvalid) {
			service.reauthPolicy.AddFailure(service.reauthSecretKey, attempt.clientBucket(), identity, now)
		}
		return err
	}
	service.reauthPolicy.Reset(service.reauthSecretKey, attempt.clientBucket(), identity)
	return nil
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
