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
	// ErrSettingsInterfaceLanguageNotStored is returned when the interface-language
	// write matched no row — the account was deleted between the request's session
	// check and the save. It is an internal failure, not owner-fault input.
	ErrSettingsInterfaceLanguageNotStored = errors.New("settings interface language not stored")
	// ErrSettingsReauthRateLimited is returned once the per-account re-auth
	// budget is spent. It is deliberately returned BEFORE the password is
	// compared, so an exhausted budget refuses even the correct password.
	ErrSettingsReauthRateLimited = errors.New("settings reauth rate limited")
)

const (
	// The re-auth budget is tighter than the 8/15min login budget: it guards a
	// password check an attacker can only reach with a session already in hand.
	// It is the account's one password re-auth budget, the 2FA disable included.
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
	UpdateInterfaceLanguage(ctx context.Context, userID uint, language string) (bool, error)
	UpdateReminderLeadDays(ctx context.Context, userID uint, leadDays int) error
	UpdatePasswordAndRevokeSessions(ctx context.Context, userID uint, expectedSessionVersion int, passwordHash string, mustChangePassword bool) error
	UpdatePasswordRecoveryCodeAndRevokeSessions(ctx context.Context, userID uint, expectedSessionVersion int, passwordHash string, recoveryHash string, mustChangePassword bool, beforeCommit func(sessionVersion int) error) error
	UpdateByID(ctx context.Context, userID uint, updates map[string]any) error
	LoadSettingsByID(ctx context.Context, userID uint) (models.User, error)
	ClearAllDataAndResetSettings(ctx context.Context, userID uint, expectedSessionVersion int) error
	DeleteAccountAndRelatedData(ctx context.Context, userID uint) error
}

type CycleSettingsUpdate struct {
	// Present names the columns this update may write; anything it does not
	// name is left as the row holds it. Build it through ValidateCycleSettings,
	// which carries the answer over from the request — a hand-built zero value
	// writes nothing.
	Present            CycleSettingsMembers
	CycleLength        int
	PeriodLength       int
	AutoPeriodFill     bool
	IrregularCycle     bool
	UnpredictableCycle bool
	AgeGroup           string
	UsageGoal          string
	LastPeriodStartSet bool
	LastPeriodStart    *time.Time
	// now and location are the request clock and the owner's time zone the start
	// date was validated against (ValidateCycleSettings sets both). A start
	// move's fill stops at the owner's local today taken from them, on the bound
	// periodFillLastDay holds for every period auto-fill. An update built by hand
	// carries a zero clock, which periodFillLastDay reads as "no today known".
	now      time.Time
	location *time.Location
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
	// dayLogs reads the owner's day logs for a Settings start move: the old
	// start's fill days are removed only while the old start still opens the
	// newest cycle. Without it a move fills the new start and removes nothing.
	dayLogs SettingsDayLogReader
}

// SettingsDayLogReader is the day-log read a Settings start move needs; the
// day service provides it.
type SettingsDayLogReader interface {
	FetchAllLogsForUser(ctx context.Context, userID uint) ([]models.DailyLog, error)
}

// AttachDayLogReader wires the day-log read a Settings start move consults
// before it removes the old start's fill days. Call it from bootstrap.
func (service *SettingsService) AttachDayLogReader(reader SettingsDayLogReader) {
	service.dayLogs = reader
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

// VerifyReauthPassword is VerifyReauth against the settings.reauth budget: the
// verify step every password-gated settings action uses. The budget is checked
// before the compare, so an exhausted budget refuses the correct password too. A
// blank submission is uncounted; a wrong password and the no-local-password
// refusal (empty hash or local_auth_enabled=false) both spend an equalized
// bcrypt and draw the budget. Like VerifyReauth it never resets: it hands back
// the budget it drew, and the caller resets that one once the write the password
// authorised has committed, so a correct password whose write was refused keeps
// the count.
func (service *SettingsService) VerifyReauthPassword(attempt ReauthAttempt, user *models.User, rawPassword string) (ReauthBudget, error) {
	budget := service.SettingsReauthBudget()
	return budget, service.VerifyReauth(budget, attempt, user, rawPassword)
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

// equalizeSettingsReauthTiming runs a bcrypt comparison against the same
// placeholder hash AuthenticateCredentials uses, so the one settings re-auth
// refusal that is decided by ACCOUNT STATE — "this account has no local
// password" in ValidateCurrentPassword and ValidatePasswordChange — costs what
// a wrong-password compare costs instead of returning before any bcrypt work.
//
// Deliberately not spent on the refusals decided by the caller's own
// submission (blank field, mismatched confirmation): their latency discloses
// nothing, and neither keeps the attempt the re-auth budget reserved for it
// (erasure and password change, see the SettingsService.reauthPolicy field
// comment): ReauthBudget.verify reserves before the compare and gives the slot
// back for a refusal that spent no bcrypt, so equalizing them would hand an
// authenticated client a full-cost bcrypt per request with no budget capping
// it.
//
// Declared as a var for the same test-substitution reason as
// equalizeAuthCredentialsTiming: tests replace it with an invocation counter
// instead of measuring wall-clock time. Production code never reassigns this.
// It spends through authTimingEqualizerCompare, the seam the login and
// registration equalizers share, so one recorder drives all three bodies.
var equalizeSettingsReauthTiming = func(password string) {
	_ = authTimingEqualizerCompare([]byte(credentialsTimingEqualizationHash), []byte(password))
}

// reauthPasswordHash is the hash a settings re-auth may compare against.
// AuthenticateCredentials refuses an account whose local_auth_enabled is off
// even when a hash is stored, so a re-auth must not accept that hash either:
// the account is answered as having no local password, through the same
// equalized branch as an empty hash (WEB-112).
func reauthPasswordHash(user *models.User) string {
	if user == nil || !user.LocalAuthEnabled {
		return ""
	}
	return user.PasswordHash
}

func (service *SettingsService) ValidateCurrentPassword(user *models.User, rawPassword string) error {
	password := strings.TrimSpace(rawPassword)
	passwordHash := reauthPasswordHash(user)
	// A blank submission is the caller's own input, not account state, so its
	// latency discloses nothing — and equalizing it would spend a full
	// passwordHashCost bcrypt on a branch VerifyReauthPassword never counts as
	// a failure, i.e. CPU the re-auth budget does not cap.
	//
	// It is answered BEFORE the account-state branch below, and not after, for
	// both halves of that sentence. Answered after, a blank submission would
	// cost nothing on an account that has a password hash and a full bcrypt on
	// one that does not — restoring, for free and without guessing anything,
	// exactly the distinguisher the branch below exists to remove; and the
	// uncapped CPU would be buyable with an empty body. ValidatePasswordChange
	// orders the same two checks the same way.
	if password == "" {
		return ErrSettingsPasswordMissing
	}
	if strings.TrimSpace(passwordHash) == "" {
		equalizeSettingsReauthTiming(password)
		return ErrSettingsLocalPasswordNotSet
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password)) != nil {
		// See the same top-up in ValidatePasswordChange: a stored hash below
		// passwordHashCost would otherwise answer faster than the equalized
		// no-local-password branch above.
		topUpAuthCredentialsTiming(passwordHash, password)
		return ErrSettingsPasswordInvalid
	}
	return nil
}

// SaveCycleSettings writes the columns the update names and no others. Writing
// the full row instead would make every save carry a snapshot of the columns it
// was not asked about, and a save that reverts a setting it never mentioned is
// the defect this endpoint exists not to have — inside one request or across
// two that overlap.
func (service *SettingsService) SaveCycleSettings(ctx context.Context, userID uint, settings CycleSettingsUpdate) error {
	updates := map[string]any{}
	if settings.Present.CycleLength {
		updates["cycle_length"] = settings.CycleLength
	}
	if settings.Present.PeriodLength {
		updates["period_length"] = settings.PeriodLength
	}
	if settings.Present.AutoPeriodFill {
		updates["auto_period_fill"] = settings.AutoPeriodFill
	}
	if settings.Present.IrregularCycle {
		updates["irregular_cycle"] = settings.IrregularCycle
	}
	if settings.Present.UnpredictableCycle {
		updates["unpredictable_cycle"] = settings.UnpredictableCycle
	}
	if settings.Present.AgeGroup {
		updates["age_group"] = NormalizeAgeGroup(settings.AgeGroup)
	}
	if settings.Present.UsageGoal {
		updates["usage_goal"] = NormalizeUsageGoal(settings.UsageGoal)
	}
	if settings.LastPeriodStartSet {
		if settings.LastPeriodStart == nil {
			updates["last_period_start"] = nil
		} else {
			updates["last_period_start"] = *settings.LastPeriodStart
		}
	}
	if len(updates) == 0 {
		// A body that named no member is a save with nothing to save. It is not
		// an error — the request asked for no change and got none — but it must
		// not reach the repository, where an empty update is a driver-level
		// question rather than a domain one.
		return nil
	}
	if settings.LastPeriodStartSet && settings.LastPeriodStart != nil {
		stored, err := service.users.LoadSettingsByID(ctx, userID)
		if err != nil {
			return err
		}
		clearOld, err := service.oldStartFillIsClearable(ctx, userID, stored, *settings.LastPeriodStart)
		if err != nil {
			return err
		}
		if move, moved := planPeriodStartMove(stored, settings, clearOld); moved {
			mover, ok := service.users.(periodStartMover)
			if !ok {
				// Never a columns-only save: the start would move without its
				// days, which is the phantom cycle this path exists to prevent.
				return errPeriodStartMoveUnsupported
			}
			// The mover gets a copy: the map UpdateByID receives below is then
			// handed to no other call, so its key set stays the literal one built
			// above, which TestNoUpdateByIDCallerPassesPasswordHashWithoutABump
			// proves free of password_hash.
			columns := make(map[string]any, len(updates))
			for column, value := range updates {
				columns[column] = value
			}
			return mover.UpdateCycleSettingsMovingPeriodStart(ctx, userID, columns, move)
		}
	}
	return service.users.UpdateByID(ctx, userID, updates)
}

var errPeriodStartMoveUnsupported = errors.New("settings repository cannot move the period start with its days")

// periodStartMover is the repository half of a Settings start move: the
// settings columns and the day-log move in one transaction. The production
// user repository implements it (pinned by a compile-time assertion in the
// service's integration test); a repository without it refuses the move.
type periodStartMover interface {
	UpdateCycleSettingsMovingPeriodStart(ctx context.Context, userID uint, updates map[string]any, move models.PeriodStartMove) error
}

// planPeriodStartMove decides what a new last_period_start does to the day
// logs. Onboarding with auto-fill wrote the old start's period days as plain
// period days, and the cycle-boundary rule counts such a run as a cycle start
// of its own: left in place after the start moved, it is a phantom cycle (a
// start moved earlier by under a cycle length leaves a cycle of a few days,
// and the dashboard stays anchored on the old date). So the days the old
// start's fill wrote are removed — only those (oldStartFillRun) — and the new
// start gets what onboarding would write for it under the owner's auto-fill
// setting and period length (as this save leaves them): the period's days up to
// its last day or the owner's local today, whichever comes first
// (periodFillLastDay), so a start moved to a recent date stores no day the
// owner has not reached. Only the days actually filled, and the new start, are
// spared from the old range's clear. Existing rows in the
// new range are never rewritten, except that a non-period row on the new start
// day becomes a period day: the date just saved is the owner's period start.
//
// The old range is considered only when clearOld holds (oldStartFillIsClearable:
// the old start still opens the newest cycle, and the new one corrects it
// rather than starting a later cycle) and the stored auto-fill setting is on:
// with it off, onboarding wrote no days there, and a bare period day in that
// range is the owner's own toggle. Clearing the start (nil) moves nothing.
func planPeriodStartMove(stored models.User, settings CycleSettingsUpdate, clearOld bool) (models.PeriodStartMove, bool) {
	newStart := dateOnly(*settings.LastPeriodStart)
	oldStart := time.Time{}
	if stored.LastPeriodStart != nil && !stored.LastPeriodStart.IsZero() {
		oldStart = dateOnly(*stored.LastPeriodStart)
	}
	if oldStart.Equal(newStart) {
		return models.PeriodStartMove{}, false
	}

	autoFill := stored.AutoPeriodFill
	if settings.Present.AutoPeriodFill {
		autoFill = settings.AutoPeriodFill
	}
	newLength := stored.PeriodLength
	if settings.Present.PeriodLength {
		newLength = settings.PeriodLength
	}

	move := models.PeriodStartMove{MarkDay: newStart}
	keep := map[string]bool{CalendarDayKey(newStart): true}
	if autoFill {
		// Resolved in the owner's location and handed over as the UTC-midnight
		// calendar day the stored dates carry, exactly as onboarding completion
		// bounds its fill.
		lastDay := CalendarDay(periodFillLastDay(newStart, periodFillLength(newLength), settings.now, settings.location), time.UTC)
		for offset := range periodFillLength(newLength) {
			day := newStart.AddDate(0, 0, offset)
			if day.After(lastDay) {
				break
			}
			move.FillDays = append(move.FillDays, day)
			keep[CalendarDayKey(day)] = true
		}
	}
	if clearOld && !oldStart.IsZero() && stored.AutoPeriodFill {
		span := periodFillLength(stored.PeriodLength)
		move.ClearFrom = oldStart
		move.ClearTo = oldStart.AddDate(0, 0, span)
		move.ClearRows = func(entries []models.DailyLog) []models.DailyLog {
			return oldStartFillRun(entries, oldStart, span, keep)
		}
	}
	return move, true
}

// oldStartFillRun picks, out of the rows dated in the old start's fill range,
// the ones that fill wrote and nothing else. It walks from the old start day by
// day, the walk ClearAutoFilledPeriodNeighbors makes, and stops at the first
// day that
//   - has no row (a gap: the fill wrote every day of its range),
//   - is not an IsAutoFilledPeriodCandidate (a day the owner edited; onboarding
//     writes flow none, so no flow is the fill's own), or
//   - was created by another write than the old start's row: one fill writes
//     its days with one creation stamp, so a bare period day the owner ticked
//     by hand — before or after turning auto-fill on, or past a period length
//     changed since onboarding — is never mistaken for the fill.
//
// A cohort is only proven by two rows sharing the stamp: unless the old start
// and the day after it are both walked, nothing is returned. One bare period
// row alone is indistinguishable from a hand tick on the start date, and fills
// written before one write stamped its whole range carry a distinct stamp per
// row — both are left in place. A period length of 1 is therefore never
// cleared.
//
// A day in keep (the new start and its fill range) is walked over but not
// returned: the move would write it again.
func oldStartFillRun(entries []models.DailyLog, oldStart time.Time, span int, keep map[string]bool) []models.DailyLog {
	byDay := make(map[string]models.DailyLog, len(entries))
	for _, entry := range entries {
		key := CalendarDayKey(dateOnly(entry.Date))
		if _, seen := byDay[key]; !seen {
			byDay[key] = entry
		}
	}
	var cohort time.Time
	var run []models.DailyLog
	walked := 0
	for offset := range span {
		key := CalendarDayKey(oldStart.AddDate(0, 0, offset))
		entry, found := byDay[key]
		if !found || !IsAutoFilledPeriodCandidate(entry, "") {
			break
		}
		if offset == 0 {
			cohort = entry.CreatedAt
		} else if !entry.CreatedAt.Equal(cohort) {
			break
		}
		walked++
		if !keep[key] {
			run = append(run, entry)
		}
	}
	if walked < 2 {
		return nil
	}
	return run
}

// oldStartFillIsClearable reports whether a Settings move from the stored start
// to newStart may remove the old start's fill days. Both must hold:
//   - the newest cycle boundary over the owner's logs (under the stored
//     context) is the one the old start's period cluster opens — once a later
//     cycle is logged, the old start is history, not a date being corrected;
//   - newStart is earlier than the old start, or later by fewer than
//     MinOnboardingCycleLength days — a start a full shortest cycle later is a
//     new period, and the old one stays recorded.
//
// Without a day-log reader the first cannot be established, so nothing is
// removed.
func (service *SettingsService) oldStartFillIsClearable(ctx context.Context, userID uint, stored models.User, newStart time.Time) (bool, error) {
	if stored.LastPeriodStart == nil || stored.LastPeriodStart.IsZero() {
		return false, nil
	}
	oldStart := dateOnly(*stored.LastPeriodStart)
	newStart = dateOnly(newStart)
	if !newStart.Before(oldStart) && CalendarDaysBetween(oldStart, newStart) >= MinOnboardingCycleLength {
		return false, nil
	}
	if service.dayLogs == nil {
		return false, nil
	}
	logs, err := service.dayLogs.FetchAllLogsForUser(ctx, userID)
	if err != nil {
		return false, err
	}
	today := DateAtLocation(time.Now(), resolveOwnerLocation(stored.Timezone, time.UTC))
	return newestBoundaryOpensClusterOf(logs, BoundaryContextFor(&stored, today), oldStart), nil
}

// periodFillLength is the number of days an auto-fill writes for a stored
// period length, the default where none valid is stored.
func periodFillLength(periodLength int) int {
	if IsValidOnboardingPeriodLength(periodLength) {
		return periodLength
	}
	return models.DefaultPeriodLength
}

// SaveUsageGoal persists ONLY users.usage_goal, scoped to userID, and returns
// the value actually stored. It is the partial form of SaveCycleSettings, for
// the surfaces that change the mode on its own (the dashboard quick switch):
// writing the whole cycle bundle from such a surface would push a page-old
// snapshot of every other cycle column back over whatever settings holds now.
// The goal is never derived from health data — only an explicit owner action
// reaches this method.
func (service *SettingsService) SaveUsageGoal(ctx context.Context, userID uint, rawUsageGoal string) (string, error) {
	usageGoal := NormalizeUsageGoal(rawUsageGoal)
	if err := service.users.UpdateByID(ctx, userID, map[string]any{"usage_goal": usageGoal}); err != nil {
		return "", err
	}
	return usageGoal, nil
}

// SaveInterfaceLanguage persists ONLY users.interface_language, scoped to
// userID: the account-side half of the interface save, whose other half is the
// `ovumcy_lang` cookie the transport layer writes.
//
// The language argument must already be normalized against the shipped locales
// — the transport layer owns that catalogue (internal/i18n) and normalizes
// before calling, the same division of labour PersistTimezone uses for IANA
// names. This service deliberately does not import the locale catalogue: the
// services layer stays free of i18n.
//
// Like the reminder and webhook saves it does NOT bump auth_session_version —
// the language of the interface is not part of the account's security posture,
// so changing it must not sign the owner's other devices out.
// A zero-row write is reported as ErrSettingsInterfaceLanguageNotStored rather
// than as success: GORM answers a no-match UPDATE with a nil error, so a save
// against an account that no longer exists would otherwise reach the owner as a
// success flash for a preference nothing kept.
func (service *SettingsService) SaveInterfaceLanguage(ctx context.Context, userID uint, language string) error {
	stored, err := service.users.UpdateInterfaceLanguage(ctx, userID, language)
	if err != nil {
		return err
	}
	if !stored {
		return ErrSettingsInterfaceLanguageNotStored
	}
	return nil
}

// SaveTrackingSettings persists the tracking preferences. The three section
// toggles arrive positive and are written in the stored, inverted spelling
// through TrackingVisibility.HiddenColumns — the one place that negation lives.
func (service *SettingsService) SaveTrackingSettings(ctx context.Context, userID uint, settings TrackingSettingsUpdate) error {
	hidden := settings.Visibility.HiddenColumns()
	return service.users.UpdateByID(ctx, userID, map[string]any{
		"track_bbt":              settings.TrackBBT,
		"temperature_unit":       NormalizeTemperatureUnit(settings.TemperatureUnit),
		"track_cervical_mucus":   settings.TrackCervicalMucus,
		"hide_sex_chip":          hidden.HideSexChip,
		"hide_cycle_factors":     hidden.HideCycleFactors,
		"hide_notes_field":       hidden.HideNotesField,
		"show_historical_phases": settings.ShowHistoricalPhases,
		"week_starts_on":         NormalizeWeekStart(settings.WeekStartsOn),
	})
}

func (service *SettingsService) LoadSettings(ctx context.Context, userID uint) (models.User, error) {
	return service.users.LoadSettingsByID(ctx, userID)
}

// ClearAllData erases the account's tracked data and resets its settings,
// revoking its sessions in the same write — only from expectedSessionVersion,
// the version of the session that passed the erasure re-auth. An account
// revoked by another write in between is left untouched and the result is
// ErrAuthSessionVersionChanged.
func (service *SettingsService) ClearAllData(ctx context.Context, userID uint, expectedSessionVersion int) error {
	return service.users.ClearAllDataAndResetSettings(ctx, userID, NormalizeAuthSessionVersion(expectedSessionVersion))
}

func (service *SettingsService) DeleteAccount(ctx context.Context, userID uint) error {
	return service.users.DeleteAccountAndRelatedData(ctx, userID)
}
